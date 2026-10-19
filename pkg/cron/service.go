package cron

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/adhocore/gronx"

	"github.com/andre25costa-code/kuromatsu/pkg/fileutil"
	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

type CronSchedule struct {
	Kind    string `json:"kind"`
	AtMS    *int64 `json:"atMs,omitempty"`
	EveryMS *int64 `json:"everyMs,omitempty"`
	Expr    string `json:"expr,omitempty"`
	TZ      string `json:"tz,omitempty"`
}

type CronPayload struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Command string `json:"command,omitempty"`
	Channel string `json:"channel,omitempty"`
	To      string `json:"to,omitempty"`
	// Window optionally names a focus window (ADR-014/FR-014) this job's
	// "Message" kind should run in — pkg/tools/cron.go's ExecuteJob
	// prefixes the dispatched message with "[foco:<Window>] " so the
	// router (pkg/routing/focus.go) picks it up via the same inline-tag
	// precedence a user typing that tag would get. Empty means "let the
	// router's Origins["cron"] mapping decide" (or "chat" if focus is
	// disabled — a no-op either way).
	Window string `json:"window,omitempty"`
	// AgentID optionally names a registered agent (Trilho G B.2) this
	// job's "Message" kind must run on, bypassing agents.dispatch.rules
	// entirely — pkg/tools/cron.go's ExecuteJob calls
	// AgentLoop.ProcessDirectForAgent instead of ProcessCronWithChannel
	// when set. Empty (the default) means "route by Channel/To like any
	// other message", which is already agent-aware today: a job whose
	// Channel/To matches a dispatch rule already lands on that rule's
	// agent with zero code changes. This field exists only for a job
	// that wants a specific agent independent of channel/chat.
	AgentID string `json:"agent_id,omitempty"`
	// Quiet (AC-028-1) makes a Command job publish nothing when it
	// succeeds: no message, no LLM turn. Only an anomaly -- non-zero exit,
	// or output matching AnomalyRegex -- produces output.
	Quiet bool `json:"quiet,omitempty"`
	// AnomalyRegex (AC-028-2) flags an anomaly even on a zero exit code.
	AnomalyRegex string `json:"anomaly_regex,omitempty"`
	// OnAnomaly (AC-028-2/3) hands an anomaly to one LLM turn instead of
	// publishing the raw output. Nil means "publish the raw output".
	OnAnomaly *CronAnomaly `json:"on_anomaly,omitempty"`
}

// CronAnomaly configures the single LLM turn a Command job gets when its
// check fails (ADR-020 P3: the model interprets anomalies, it doesn't run
// the routine).
type CronAnomaly struct {
	// Message is the instruction the LLM gets ahead of the command output.
	Message string `json:"message"`
	// Window optionally names the focus window for that turn.
	Window string `json:"window,omitempty"`
	// MaxOutputChars truncates the command output sent to the LLM
	// (default DefaultAnomalyMaxOutputChars).
	MaxOutputChars int `json:"max_output_chars,omitempty"`
	// MaxPerDay caps LLM turns per job per day; past it the raw output is
	// published instead (default DefaultAnomalyMaxPerDay).
	MaxPerDay int `json:"max_per_day,omitempty"`
}

const (
	DefaultAnomalyMaxOutputChars = 2000
	DefaultAnomalyMaxPerDay      = 3
)

// Values of CronJobState.LastStatus and CronRun.Status.
const (
	RunStatusOK       = "ok"
	RunStatusError    = "error"
	RunStatusCanceled = "canceled"
)

const (
	// autoDisableMinWindow is the minimum time between a job's first
	// consecutive failure and its auto-disable, so a short provider outage
	// does not switch off a high-frequency job.
	autoDisableMinWindow = time.Hour
	// defaultMaxSleep bounds how long the run loop sleeps before it looks at
	// jobs.json again for changes made by another process.
	defaultMaxSleep = time.Minute
	// canceledAtRetryDelay is when a one-shot job whose run was canceled by
	// shutdown is tried again.
	canceledAtRetryDelay = time.Minute
)

// ErrJobNotFound is returned by mutations on an unknown job ID.
var ErrJobNotFound = errors.New("job not found")

// ErrJobDisabled is returned by RunNow for a disabled job.
var ErrJobDisabled = errors.New("job is disabled; enable it first")

type CronJobState struct {
	NextRunAtMS *int64 `json:"nextRunAtMs,omitempty"`
	LastRunAtMS *int64 `json:"lastRunAtMs,omitempty"`
	LastStatus  string `json:"lastStatus,omitempty"`
	LastError   string `json:"lastError,omitempty"`
	// ConsecutiveFailures counts "error" runs since the last "ok" run;
	// "canceled" runs leave it unchanged.
	ConsecutiveFailures int `json:"consecutiveFailures,omitempty"`
	// FirstFailureAtMS is the start of the first run in the current
	// failure streak.
	FirstFailureAtMS *int64 `json:"firstFailureAtMs,omitempty"`
}

type CronJob struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	Enabled        bool         `json:"enabled"`
	Schedule       CronSchedule `json:"schedule"`
	Payload        CronPayload  `json:"payload"`
	State          CronJobState `json:"state"`
	CreatedAtMS    int64        `json:"createdAtMs"`
	UpdatedAtMS    int64        `json:"updatedAtMs"`
	DeleteAfterRun bool         `json:"deleteAfterRun"`
}

type CronStore struct {
	Version int       `json:"version"`
	Jobs    []CronJob `json:"jobs"`
}

type JobHandler func(job *CronJob) (string, error)

type CronService struct {
	storePath string
	store     *CronStore
	onJob     JobHandler
	mu        sync.RWMutex
	running   bool
	stopChan  chan struct{}
	wakeChan  chan struct{}
	gronx     *gronx.Gronx

	// lastBytes is the exact content of jobs.json this instance last read
	// or wrote; any other content means another process changed the file.
	lastBytes []byte
	// maxSleep caps the run loop's sleep so changes written by another
	// process (the CLI) are picked up. Tests lower it before Start.
	maxSleep time.Duration
	// now is the clock used for run timing and auto-disable.
	now func() time.Time
	// maxConsecutiveFailures disables a job after that many failed runs in
	// a row (and at least autoDisableMinWindow); 0 never disables.
	maxConsecutiveFailures int
	// onAutoDisable is called once, outside the lock, after a job has been
	// auto-disabled and saved.
	onAutoDisable func(CronJob)
}

func NewCronService(storePath string, onJob JobHandler) *CronService {
	cs := &CronService{
		storePath: storePath,
		onJob:     onJob,
		gronx:     gronx.New(),
		wakeChan:  make(chan struct{}, 1),
		maxSleep:  defaultMaxSleep,
		now:       time.Now,
	}
	// Initialize and load store on creation
	cs.loadStore()
	return cs
}

func (cs *CronService) Start() error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if cs.running {
		return nil
	}

	if err := cs.loadStore(); err != nil {
		return fmt.Errorf("failed to load store: %w", err)
	}

	cs.recomputeNextRuns()
	if err := cs.saveStoreUnsafe(); err != nil {
		return fmt.Errorf("failed to save store: %w", err)
	}

	cs.stopChan = make(chan struct{})
	if cs.wakeChan == nil {
		cs.wakeChan = make(chan struct{}, 1)
	}
	cs.running = true
	go cs.runLoop(cs.stopChan)

	return nil
}

func (cs *CronService) Stop() {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if !cs.running {
		return
	}

	cs.running = false
	if cs.stopChan != nil {
		close(cs.stopChan)
		cs.stopChan = nil
	}
}

func (cs *CronService) runLoop(stopChan chan struct{}) {
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()

	for {
		// every loop, pick up changes another process wrote to jobs.json,
		// then recalculate the next wake time
		cs.mu.Lock()
		cs.reloadIfChangedUnsafe()
		nextWake := cs.getNextWakeMS()
		maxSleep := cs.maxSleep
		if maxSleep <= 0 {
			maxSleep = defaultMaxSleep // never spin
		}
		cs.mu.Unlock()

		// Never sleep longer than maxSleep: a job added, enabled or marked
		// run-now by the CLI only reaches this process through the file.
		delay := maxSleep
		now := time.Now().UnixMilli()

		if nextWake != nil {
			diff := *nextWake - now
			if diff <= 0 {
				delay = 0
			} else if d := time.Duration(diff) * time.Millisecond; d < delay {
				delay = d
			}
		}

		timer.Reset(delay)

		select {
		case <-stopChan:
			return
		case <-cs.wakeChan: // wake on new job or update
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			continue
		case <-timer.C:
			cs.checkJobs()
		}
	}
}

func (cs *CronService) checkJobs() {
	cs.mu.Lock()

	if !cs.running {
		cs.mu.Unlock()
		return
	}

	cs.reloadIfChangedUnsafe()

	now := time.Now().UnixMilli()
	var dueJobIDs []string

	// Collect jobs that are due (we need to copy them to execute outside lock)
	for i := range cs.store.Jobs {
		job := &cs.store.Jobs[i]
		if job.Enabled && job.State.NextRunAtMS != nil && *job.State.NextRunAtMS <= now {
			dueJobIDs = append(dueJobIDs, job.ID)
		}
	}

	// Reset next run for due jobs before unlocking to avoid duplicate execution.
	dueMap := make(map[string]bool, len(dueJobIDs))
	for _, jobID := range dueJobIDs {
		dueMap[jobID] = true
	}
	for i := range cs.store.Jobs {
		if dueMap[cs.store.Jobs[i].ID] {
			cs.store.Jobs[i].State.NextRunAtMS = nil
		}
	}

	// The loop wakes at least every maxSleep; only rewrite jobs.json when a
	// job actually became due.
	if len(dueJobIDs) > 0 {
		// saveStoreUnsafe logs its own failure.
		_ = cs.saveStoreUnsafe()
	}

	cs.mu.Unlock()

	// Execute jobs outside lock.
	for _, jobID := range dueJobIDs {
		cs.executeJobByID(jobID)
	}
}

func (cs *CronService) executeJobByID(jobID string) {
	start := cs.now()
	startTime := start.UnixMilli()

	cs.mu.RLock()
	var callbackJob *CronJob
	for i := range cs.store.Jobs {
		job := &cs.store.Jobs[i]
		if job.ID == jobID {
			jobCopy := cloneCronJob(*job)
			callbackJob = &jobCopy
			break
		}
	}
	onJob := cs.onJob
	cs.mu.RUnlock()

	if callbackJob == nil {
		logger.WarnCF("cron", "job not found, skipping", map[string]any{"job_id": jobID})
		return
	}

	// Log job execution start
	logger.InfoCF("cron", "executing job", map[string]any{
		"job_id": jobID, "name": callbackJob.Name,
		"schedule": callbackJob.Schedule.Kind, "channel": callbackJob.Payload.Channel,
	})

	var err error
	if onJob != nil {
		_, err = onJob(callbackJob)
	}

	end := cs.now()
	execDuration := end.Sub(start).Milliseconds()

	// A canceled run (gateway shutdown or reload) is neither a failure nor a
	// success: the failure streak is left as it is.
	status := RunStatusOK
	if err != nil {
		status = RunStatusError
		if errors.Is(err, context.Canceled) {
			status = RunStatusCanceled
		}
	}

	// Now acquire lock to update state
	cs.mu.Lock()

	// The CLI may have changed the job (e.g. disabled it) while it ran.
	cs.reloadIfChangedUnsafe()

	var job *CronJob
	for i := range cs.store.Jobs {
		if cs.store.Jobs[i].ID == jobID {
			job = &cs.store.Jobs[i]
			break
		}
	}
	if job == nil {
		logger.WarnCF("cron", "job disappeared before state update", map[string]any{"job_id": jobID})
		cs.mu.Unlock()
		return
	}

	job.State.LastRunAtMS = &startTime
	job.UpdatedAtMS = time.Now().UnixMilli()
	job.State.LastStatus = status

	switch status {
	case RunStatusError:
		job.State.LastError = err.Error()
		job.State.ConsecutiveFailures++
		if job.State.FirstFailureAtMS == nil {
			first := startTime
			job.State.FirstFailureAtMS = &first
		}
		logger.WarnCF("cron", "job failed", map[string]any{
			"job_id": job.ID, "name": job.Name, "duration_ms": execDuration, "error": err.Error(),
		})
	case RunStatusCanceled:
		job.State.LastError = err.Error()
		logger.InfoCF("cron", "Job run canceled", map[string]any{
			"job_id": job.ID, "name": job.Name, "duration_ms": execDuration,
		})
	default:
		job.State.LastError = ""
		job.State.ConsecutiveFailures = 0
		job.State.FirstFailureAtMS = nil
	}

	// Compute next run time
	var nextRunStr string
	var autoDisabled *CronJob
	if job.Schedule.Kind == "at" && status == RunStatusCanceled {
		// A one-shot job stopped by shutdown has not done its work yet: keep
		// it and let it run again shortly (or after the restart, see
		// recomputeNextRuns) instead of deleting it.
		if job.Enabled {
			retry := time.Now().Add(canceledAtRetryDelay).UnixMilli()
			job.State.NextRunAtMS = &retry
			nextRunStr = time.UnixMilli(retry).Format("2006-01-02 15:04:05")
		}
	} else if job.Schedule.Kind == "at" {
		if job.DeleteAfterRun {
			_, _ = cs.removeJobUnsafe(job.ID)
			nextRunStr = "(deleted)"
		} else {
			job.Enabled = false
			job.State.NextRunAtMS = nil
			nextRunStr = "(disabled)"
		}
	} else if job.Enabled {
		nextRun := cs.computeNextRun(&job.Schedule, time.Now().UnixMilli())
		job.State.NextRunAtMS = nextRun
		if nextRun != nil {
			nextRunStr = time.UnixMilli(*nextRun).Format("2006-01-02 15:04:05")
		} else {
			nextRunStr = "(none)"
		}
		if cs.shouldAutoDisableUnsafe(job, end) {
			job.Enabled = false
			job.State.NextRunAtMS = nil
			job.State.LastError = autoDisabledError(job.State.ConsecutiveFailures, job.State.LastError)
			disabledCopy := cloneCronJob(*job)
			autoDisabled = &disabledCopy
			nextRunStr = "(auto-disabled)"
			logger.WarnCF("cron", "Job auto-disabled after consecutive failures", map[string]any{
				"job_id": job.ID, "name": job.Name, "failures": job.State.ConsecutiveFailures,
			})
		}
	} else {
		job.State.NextRunAtMS = nil
		nextRunStr = "(disabled)"
	}

	if err == nil {
		logger.InfoCF("cron", "job completed", map[string]any{
			"job_id": job.ID, "name": job.Name, "duration_ms": execDuration, "next_run": nextRunStr,
		})
	}

	// saveStoreUnsafe logs its own failure.
	_ = cs.saveStoreUnsafe()

	runErr := ""
	if err != nil {
		runErr = err.Error()
	}
	cs.recordRunUnsafe(jobID, CronRun{
		StartedAtMS: startTime,
		DurationMS:  execDuration,
		Status:      status,
		Error:       runErr,
	})

	onAutoDisable := cs.onAutoDisable
	cs.mu.Unlock()

	if autoDisabled != nil && onAutoDisable != nil {
		onAutoDisable(*autoDisabled)
	}
}

// shouldAutoDisableUnsafe reports whether a job that just failed has failed
// often enough, for long enough, to be switched off.
func (cs *CronService) shouldAutoDisableUnsafe(job *CronJob, now time.Time) bool {
	if cs.maxConsecutiveFailures <= 0 || job.State.LastStatus != RunStatusError {
		return false
	}
	if job.State.ConsecutiveFailures < cs.maxConsecutiveFailures || job.State.FirstFailureAtMS == nil {
		return false
	}
	return now.UnixMilli()-*job.State.FirstFailureAtMS >= autoDisableMinWindow.Milliseconds()
}

// autoDisabledPrefix matches the prefix auto-disable puts on LastError.
var autoDisabledPrefix = regexp.MustCompile(`^auto-disabled after \d+ consecutive failures: `)

func autoDisabledError(failures int, lastError string) string {
	return fmt.Sprintf("auto-disabled after %d consecutive failures: %s", failures, StripAutoDisabledPrefix(lastError))
}

// StripAutoDisabledPrefix returns lastError without the
// "auto-disabled after N consecutive failures: " prefix, if present.
func StripAutoDisabledPrefix(lastError string) string {
	return autoDisabledPrefix.ReplaceAllString(lastError, "")
}

func (cs *CronService) computeNextRun(schedule *CronSchedule, nowMS int64) *int64 {
	return computeNextRunAt(schedule, nowMS)
}

// computeNextRunAt returns the schedule's next run after nowMS, or nil when
// it has none (an 'at' in the past, an invalid expression). A cron
// expression is evaluated in schedule.TZ when set.
func computeNextRunAt(schedule *CronSchedule, nowMS int64) *int64 {
	switch schedule.Kind {
	case "at":
		if schedule.AtMS != nil && *schedule.AtMS > nowMS {
			return schedule.AtMS
		}
		return nil
	case "every":
		if schedule.EveryMS == nil || *schedule.EveryMS <= 0 {
			return nil
		}
		next := nowMS + *schedule.EveryMS
		return &next
	case "cron":
		if schedule.Expr == "" {
			return nil
		}

		// Use gronx to calculate next run time. gronx reads the wall-clock
		// fields of the time it gets, so moving it into the job's zone makes
		// "0 9 * * *" mean 09:00 there.
		now := time.UnixMilli(nowMS)
		if schedule.TZ != "" {
			loc, err := time.LoadLocation(schedule.TZ)
			if err != nil {
				logger.WarnCF("cron", "Invalid time zone in schedule; using local time", map[string]any{
					"tz": schedule.TZ, "error": err.Error(),
				})
			} else {
				now = now.In(loc)
			}
		}
		nextTime, err := gronx.NextTickAfter(schedule.Expr, now, false)
		if err != nil {
			logger.WarnCF("cron", "failed to compute next run", map[string]any{
				"expr": schedule.Expr, "error": err.Error(),
			})
			return nil
		}

		nextMS := nextTime.UnixMilli()
		return &nextMS
	default:
		logger.WarnCF("cron", "unknown schedule kind", map[string]any{"kind": schedule.Kind})
		return nil
	}
}

// wake up the loop to re-evaluate next wake time immediately (e.g. after add/update/remove jobs)
func (cs *CronService) notify() {
	select {
	case cs.wakeChan <- struct{}{}:
	default:
		// The 1-slot buffer already holds a pending wake-up, which the loop
		// consumes on its next select; a second one adds nothing.
	}
}

func (cs *CronService) recomputeNextRuns() {
	now := time.Now().UnixMilli()
	for i := range cs.store.Jobs {
		job := &cs.store.Jobs[i]
		if !job.Enabled {
			continue
		}
		// A one-shot job whose run was canceled by shutdown keeps its retry
		// time, although its own time has passed.
		if job.Schedule.Kind == "at" && job.State.LastStatus == RunStatusCanceled && job.State.NextRunAtMS != nil {
			continue
		}
		job.State.NextRunAtMS = cs.computeNextRun(&job.Schedule, now)
	}
}

func (cs *CronService) getNextWakeMS() *int64 {
	var nextWake *int64
	for _, job := range cs.store.Jobs {
		if job.Enabled && job.State.NextRunAtMS != nil {
			if nextWake == nil || *job.State.NextRunAtMS < *nextWake {
				nextWake = job.State.NextRunAtMS
			}
		}
	}
	return nextWake
}

func (cs *CronService) Load() error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.loadStore()
}

func (cs *CronService) SetOnJob(handler JobHandler) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.onJob = handler
}

// SetMaxConsecutiveFailures sets how many failed runs in a row disable a
// job (also requiring at least an hour since the first of them); 0 never
// disables.
func (cs *CronService) SetMaxConsecutiveFailures(n int) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.maxConsecutiveFailures = n
}

// SetOnAutoDisable registers the callback run once, after the save and
// outside the lock, for each job the service auto-disables.
func (cs *CronService) SetOnAutoDisable(fn func(CronJob)) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.onAutoDisable = fn
}

// SetMaxSleep caps how long the run loop sleeps before re-reading jobs.json.
// Call it before Start.
func (cs *CronService) SetMaxSleep(d time.Duration) {
	if d <= 0 {
		d = defaultMaxSleep
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.maxSleep = d
}

// MinAgentTurnInterval is the shortest interval a recurring ("every") job
// that runs an agent turn may have. Faster routines must be deterministic
// commands.
const MinAgentTurnInterval = 5 * time.Minute

// CheckAgentTurnInterval refuses an "every" schedule shorter than
// MinAgentTurnInterval for a job without a command (an agent turn). Every
// path that lets a chat or the web UI set a schedule applies it.
func CheckAgentTurnInterval(s CronSchedule, command string) error {
	if strings.TrimSpace(command) != "" || s.Kind != "every" || s.EveryMS == nil {
		return nil
	}
	if *s.EveryMS >= MinAgentTurnInterval.Milliseconds() {
		return nil
	}
	return fmt.Errorf(
		"recurring agent turns must run at least %d seconds apart; use a deterministic command for faster schedules",
		int64(MinAgentTurnInterval/time.Second),
	)
}

func validateSchedule(s CronSchedule, nowMS int64) error {
	if s.TZ != "" {
		if _, err := time.LoadLocation(s.TZ); err != nil {
			return fmt.Errorf("invalid time zone %q: %w", s.TZ, err)
		}
	}
	switch s.Kind {
	case "cron":
		if !gronx.IsValid(s.Expr) || computeNextRunAt(&s, nowMS) == nil {
			return fmt.Errorf("invalid cron expression %q", s.Expr)
		}
	case "at":
		if s.AtMS == nil || *s.AtMS <= nowMS {
			return errors.New("'at' time is in the past")
		}
	case "every":
		if s.EveryMS == nil || *s.EveryMS <= 0 {
			return errors.New("'every' interval must be positive")
		}
	default:
		return fmt.Errorf("unknown schedule kind %q", s.Kind)
	}
	return nil
}

// reloadIfChangedUnsafe replaces the in-memory store with jobs.json when the
// file no longer holds the bytes this instance last read or wrote, i.e.
// another process (the CLI while the gateway runs, or the other way round)
// changed it. It never recomputes NextRunAtMS, so a run-now written by
// another process is kept. A read or parse error keeps the in-memory store.
//
// There is no lock between processes: a write from another process landing
// between this reload and the caller's save is lost. That window is a few
// microseconds under cs.mu, which is acceptable for a human-driven CLI.
func (cs *CronService) reloadIfChangedUnsafe() {
	data, err := os.ReadFile(cs.storePath)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.WarnCF("cron", "Failed to re-read cron store", map[string]any{
				"path": cs.storePath, "error": err.Error(),
			})
		}
		return
	}
	if bytes.Equal(data, cs.lastBytes) {
		return
	}
	store := &CronStore{Version: 1, Jobs: []CronJob{}}
	if err := json.Unmarshal(data, store); err != nil {
		logger.WarnCF("cron", "Ignoring unreadable cron store change", map[string]any{
			"path": cs.storePath, "error": err.Error(),
		})
		return
	}
	cs.store = store
	cs.lastBytes = data
}

func (cs *CronService) loadStore() error {
	cs.store = &CronStore{
		Version: 1,
		Jobs:    []CronJob{},
	}

	data, err := os.ReadFile(cs.storePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if err := json.Unmarshal(data, cs.store); err != nil {
		return err
	}
	cs.lastBytes = data
	return nil
}

func (cs *CronService) saveStoreUnsafe() error {
	data, err := json.MarshalIndent(cs.store, "", "  ")
	if err != nil {
		logger.ErrorCF("cron", "Failed to encode cron store", map[string]any{"error": err.Error()})
		return err
	}

	// Use unified atomic write utility with explicit sync for flash storage reliability.
	if err := fileutil.WriteFileAtomic(cs.storePath, data, 0o600); err != nil {
		logger.ErrorCF("cron", "Failed to write cron store", map[string]any{
			"path": cs.storePath, "error": err.Error(),
		})
		return err
	}
	// Remember exactly what was written, so only another process's write
	// looks like a change to reloadIfChangedUnsafe.
	cs.lastBytes = data
	return nil
}

// ErrJobExistsWithDifferentCommand is returned by AddJobWithPayload when an
// identical job (same name, schedule, message and destination) already exists
// with another command: "add" never rewrites an existing job.
var ErrJobExistsWithDifferentCommand = errors.New("cron: job already exists with a different command")

func (cs *CronService) AddJob(
	name string,
	schedule CronSchedule,
	message string,
	channel, to string,
) (*CronJob, error) {
	job, _, err := cs.addJob(name, schedule, CronPayload{Message: message, Channel: channel, To: to}, false)
	return job, err
}

// AddJobWithPayload creates a job with its full payload (command, quiet,
// anomaly handling, window...) in one locked, single save. An identical job
// (AC-028-6: same name, schedule, message and destination) is returned with
// created=false instead of being duplicated; if it has a different command
// the call fails with ErrJobExistsWithDifferentCommand and nothing changes.
func (cs *CronService) AddJobWithPayload(
	name string,
	schedule CronSchedule,
	payload CronPayload,
) (job *CronJob, created bool, err error) {
	return cs.addJob(name, schedule, payload, true)
}

func (cs *CronService) addJob(
	name string,
	schedule CronSchedule,
	payload CronPayload,
	checkCommand bool,
) (*CronJob, bool, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.reloadIfChangedUnsafe()

	// A schedule that can never fire is refused instead of stored as an
	// enabled job that silently never runs.
	if err := validateSchedule(schedule, time.Now().UnixMilli()); err != nil {
		return nil, false, err
	}

	// AC-028-6: an identical job (same name, schedule, message and
	// destination) is returned instead of duplicated -- a model repeating
	// the same "cron add" on every heartbeat created 7 copies on 2026-09-21.
	for i := range cs.store.Jobs {
		existing := &cs.store.Jobs[i]
		if existing.Name == name && sameSchedule(existing.Schedule, schedule) &&
			existing.Payload.Message == payload.Message && existing.Payload.Channel == payload.Channel &&
			existing.Payload.To == payload.To {
			jobCopy := cloneCronJob(*existing)
			if checkCommand && existing.Payload.Command != payload.Command {
				return &jobCopy, false, ErrJobExistsWithDifferentCommand
			}
			return &jobCopy, false, nil
		}
	}

	now := time.Now().UnixMilli()

	// One-time tasks (at) should be deleted after execution
	deleteAfterRun := (schedule.Kind == "at")

	if payload.Kind == "" {
		payload.Kind = "agent_turn"
	}
	job := CronJob{
		ID:       generateID(),
		Name:     name,
		Enabled:  true,
		Schedule: schedule,
		Payload:  payload,
		State: CronJobState{
			NextRunAtMS: cs.computeNextRun(&schedule, now),
		},
		CreatedAtMS:    now,
		UpdatedAtMS:    now,
		DeleteAfterRun: deleteAfterRun,
	}

	cs.store.Jobs = append(cs.store.Jobs, job)
	if err := cs.saveStoreUnsafe(); err != nil {
		return nil, false, err
	}

	cs.notify()

	jobCopy := cloneCronJob(job)
	return &jobCopy, true, nil
}

// GetJob returns a copy of the job, re-reading jobs.json first if another
// process changed it.
func (cs *CronService) GetJob(jobID string) (*CronJob, bool) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.reloadIfChangedUnsafe()

	for i := range cs.store.Jobs {
		if cs.store.Jobs[i].ID == jobID {
			jobCopy := cloneCronJob(cs.store.Jobs[i])
			return &jobCopy, true
		}
	}
	return nil, false
}

// UpdateJob replaces a job. The schedule is validated only when it changes,
// so a job loaded from disk with a schedule that can no longer fire (an
// expired 'at', a bad expression) can still have its message or command
// edited. A schedule or payload change starts a new failure streak.
func (cs *CronService) UpdateJob(job *CronJob) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.reloadIfChangedUnsafe()

	for i := range cs.store.Jobs {
		if cs.store.Jobs[i].ID == job.ID {
			previous := cs.store.Jobs[i]
			updated := cloneCronJob(*job)
			now := time.Now().UnixMilli()
			scheduleChanged := !sameSchedule(previous.Schedule, updated.Schedule)
			if scheduleChanged {
				if err := validateSchedule(updated.Schedule, now); err != nil {
					return err
				}
			}
			// reflect.DeepEqual follows the OnAnomaly pointer, so an
			// unchanged copy does not count as a payload change.
			if scheduleChanged || !reflect.DeepEqual(previous.Payload, updated.Payload) {
				updated.State.ConsecutiveFailures = 0
				updated.State.FirstFailureAtMS = nil
			}
			updated.UpdatedAtMS = now
			if updated.Enabled {
				if previous.Enabled != updated.Enabled || !sameSchedule(previous.Schedule, updated.Schedule) {
					updated.State.NextRunAtMS = cs.computeNextRun(&updated.Schedule, now)
				}
			} else {
				updated.State.NextRunAtMS = nil
			}
			cs.store.Jobs[i] = updated

			cs.notify()

			return cs.saveStoreUnsafe()
		}
	}
	return ErrJobNotFound
}

func cloneCronJob(job CronJob) CronJob {
	clone := job
	if job.Schedule.AtMS != nil {
		atMS := *job.Schedule.AtMS
		clone.Schedule.AtMS = &atMS
	}
	if job.Schedule.EveryMS != nil {
		everyMS := *job.Schedule.EveryMS
		clone.Schedule.EveryMS = &everyMS
	}
	if job.State.NextRunAtMS != nil {
		nextRunAtMS := *job.State.NextRunAtMS
		clone.State.NextRunAtMS = &nextRunAtMS
	}
	if job.State.LastRunAtMS != nil {
		lastRunAtMS := *job.State.LastRunAtMS
		clone.State.LastRunAtMS = &lastRunAtMS
	}
	if job.State.FirstFailureAtMS != nil {
		firstFailureAtMS := *job.State.FirstFailureAtMS
		clone.State.FirstFailureAtMS = &firstFailureAtMS
	}
	if job.Payload.OnAnomaly != nil {
		onAnomaly := *job.Payload.OnAnomaly
		clone.Payload.OnAnomaly = &onAnomaly
	}
	return clone
}

func sameSchedule(a, b CronSchedule) bool {
	return a.Kind == b.Kind &&
		sameInt64(a.AtMS, b.AtMS) &&
		sameInt64(a.EveryMS, b.EveryMS) &&
		a.Expr == b.Expr &&
		a.TZ == b.TZ
}

func sameInt64(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// RemoveJob deletes a job. It reports whether the job existed, and an error
// when the deletion could not be saved.
func (cs *CronService) RemoveJob(jobID string) (bool, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.reloadIfChangedUnsafe()

	return cs.removeJobUnsafe(jobID)
}

func (cs *CronService) removeJobUnsafe(jobID string) (bool, error) {
	before := len(cs.store.Jobs)
	var jobs []CronJob
	for _, job := range cs.store.Jobs {
		if job.ID != jobID {
			jobs = append(jobs, job)
		}
	}
	cs.store.Jobs = jobs
	removed := len(cs.store.Jobs) < before

	var saveErr error
	if removed {
		if saveErr = cs.saveStoreUnsafe(); saveErr != nil {
			saveErr = fmt.Errorf("save cron store: %w", saveErr)
		}
	}

	cs.notify()

	return removed, saveErr
}

// EnableJob enables or disables a job and returns a copy of it. Enabling
// starts a new failure streak and drops the auto-disable note from
// LastError; enabling an 'at' job whose time has passed is an error.
func (cs *CronService) EnableJob(jobID string, enabled bool) (*CronJob, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.reloadIfChangedUnsafe()

	for i := range cs.store.Jobs {
		job := &cs.store.Jobs[i]
		if job.ID == jobID {
			now := time.Now().UnixMilli()

			if enabled {
				nextRun := cs.computeNextRun(&job.Schedule, now)
				if job.Schedule.Kind == "at" && nextRun == nil {
					return nil, errors.New("'at' time is in the past")
				}
				job.State.NextRunAtMS = nextRun
				job.State.ConsecutiveFailures = 0
				job.State.FirstFailureAtMS = nil
				job.State.LastError = StripAutoDisabledPrefix(job.State.LastError)
			} else {
				job.State.NextRunAtMS = nil
			}
			job.Enabled = enabled
			job.UpdatedAtMS = now

			if err := cs.saveStoreUnsafe(); err != nil {
				return nil, fmt.Errorf("save cron store: %w", err)
			}

			cs.notify()

			jobCopy := cloneCronJob(*job)
			return &jobCopy, nil
		}
	}

	return nil, ErrJobNotFound
}

// RunNow marks an enabled job as due now. The process running the
// scheduler (the gateway) executes it on its next wake-up; another process
// only writes the mark to jobs.json.
func (cs *CronService) RunNow(jobID string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.reloadIfChangedUnsafe()

	for i := range cs.store.Jobs {
		job := &cs.store.Jobs[i]
		if job.ID != jobID {
			continue
		}
		if !job.Enabled {
			return ErrJobDisabled
		}
		now := time.Now().UnixMilli()
		job.State.NextRunAtMS = &now
		job.UpdatedAtMS = now
		if err := cs.saveStoreUnsafe(); err != nil {
			return fmt.Errorf("save cron store: %w", err)
		}
		cs.notify()
		return nil
	}
	return ErrJobNotFound
}

// ListJobs returns copies of the jobs, re-reading jobs.json first if another
// process changed it.
func (cs *CronService) ListJobs(includeDisabled bool) []CronJob {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.reloadIfChangedUnsafe()

	jobs := make([]CronJob, 0, len(cs.store.Jobs))
	for _, job := range cs.store.Jobs {
		if includeDisabled || job.Enabled {
			jobs = append(jobs, cloneCronJob(job))
		}
	}

	return jobs
}

func (cs *CronService) Status() map[string]any {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	var enabledCount int
	for _, job := range cs.store.Jobs {
		if job.Enabled {
			enabledCount++
		}
	}

	return map[string]any{
		"enabled":      cs.running,
		"jobs":         len(cs.store.Jobs),
		"nextWakeAtMS": cs.getNextWakeMS(),
	}
}

func generateID() string {
	// Use crypto/rand for better uniqueness under concurrent access
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback to time-based if crypto/rand fails
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
