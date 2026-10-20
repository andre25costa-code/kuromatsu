package tools

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

const maxOutputBufferSize = 1 * 1024 * 1024 // 1MB

const outputTruncateMarker = "\n... [output truncated, exceeded 1MB]\n"

const (
	// maxRunningBackgroundSessions caps how many background sessions may be
	// running at once in this process.
	maxRunningBackgroundSessions = 8
	// backgroundSessionMaxAge is how long a background session may keep
	// running before the periodic cleanup kills it.
	backgroundSessionMaxAge = time.Hour
	// doneSessionRetention is how long a finished session stays readable
	// before the periodic cleanup removes it.
	doneSessionRetention = 30 * time.Minute
)

// expiredSessionNote is appended to the output of a session killed for
// exceeding backgroundSessionMaxAge.
const expiredSessionNote = "\n[killed after 1h: background session exceeded the maximum age]\n"

// PtyKeyMode represents arrow key encoding mode for PTY sessions.
// Programs send smkx/rmkx sequences to switch between CSI and SS3 modes.
type PtyKeyMode uint8

const (
	PtyKeyModeCSI PtyKeyMode = iota // triggered by rmkx (\x1b[?1l)
	PtyKeyModeSS3                   // triggered by smkx (\x1b[?1h)
)

const PtyKeyModeNotFound PtyKeyMode = 255

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionDone     = errors.New("session already completed")
	ErrPTYNotSupported = errors.New("PTY is not supported on this platform")
	ErrNoStdin         = errors.New("no stdin available")
)

type ProcessSession struct {
	mu              sync.Mutex
	owner           string // Immutable trusted agent/session scope, set before Add.
	ID              string
	PID             int
	Command         string
	PTY             bool
	Background      bool
	StartTime       int64
	EndTime         int64 // Unix seconds when the session finished or was killed; 0 while running.
	ExitCode        int
	Status          string
	stdinWriter     io.Writer
	outputBuffer    *bytes.Buffer
	outputTruncated bool
	ptyMaster       *os.File

	// ptyKeyMode tracks arrow key encoding mode (CSI vs SS3)
	ptyKeyMode PtyKeyMode
}

func (s *ProcessSession) IsDone() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Status == "done" || s.Status == "exited"
}

func (s *ProcessSession) GetPtyKeyMode() PtyKeyMode {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ptyKeyMode
}

func (s *ProcessSession) SetPtyKeyMode(mode PtyKeyMode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ptyKeyMode = mode
}

func (s *ProcessSession) GetStatus() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Status
}

func (s *ProcessSession) GetExitCode() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ExitCode
}

func (s *ProcessSession) killProcess() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Status != "running" {
		return ErrSessionDone
	}

	pid := s.PID
	if pid <= 0 {
		return ErrSessionNotFound
	}

	if err := killProcessGroup(pid); err != nil {
		return err
	}

	s.Status = "done"
	s.ExitCode = -1
	s.markEndedLocked()
	return nil
}

// markEndedLocked records the end time once. Callers hold s.mu.
func (s *ProcessSession) markEndedLocked() {
	if s.EndTime == 0 {
		s.EndTime = time.Now().Unix()
	}
}

// markExited records the exit of the session's process. A session already
// marked done (e.g. killed) keeps its status and exit code.
func (s *ProcessSession) markExited(state *os.ProcessState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Status == "running" {
		if state != nil {
			s.ExitCode = state.ExitCode()
		}
		s.Status = "done"
	}
	s.markEndedLocked()
}

func (s *ProcessSession) Kill() error {
	return s.killProcess()
}

func (s *ProcessSession) Write(data string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Status != "running" {
		return ErrSessionDone
	}

	var writer io.Writer
	if s.PTY && s.ptyMaster != nil {
		writer = s.ptyMaster
	} else if s.stdinWriter != nil {
		writer = s.stdinWriter
	} else {
		return ErrNoStdin
	}

	_, err := writer.Write([]byte(data))
	return err
}

func (s *ProcessSession) Read() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.outputBuffer.Len() == 0 {
		return ""
	}

	data := s.outputBuffer.String()
	s.outputBuffer.Reset()
	return data
}

func (s *ProcessSession) ToSessionInfo() SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()

	return SessionInfo{
		ID:        s.ID,
		Command:   s.Command,
		Status:    s.Status,
		PID:       s.PID,
		StartedAt: s.StartTime,
	}
}

type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*ProcessSession
	stopCh   chan struct{}
	stopOnce sync.Once
}

func NewSessionManager() *SessionManager {
	sm := &SessionManager{
		sessions: make(map[string]*ProcessSession),
		stopCh:   make(chan struct{}),
	}

	// Start cleaner goroutine - runs every 5 minutes, cleans up sessions done for >30 minutes
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-sm.stopCh:
				return
			case <-ticker.C:
				sm.cleanupOldSessions()
			}
		}
	}()

	return sm
}

// Stop shuts down the background cleanup goroutine. Safe to call multiple
// times from concurrent goroutines. After Stop returns, the SessionManager
// is still usable — only the cleanup goroutine is terminated.
func (sm *SessionManager) Stop() {
	sm.stopOnce.Do(func() {
		close(sm.stopCh)
	})
}

// cleanupOldSessions removes sessions that finished more than
// doneSessionRetention ago and kills running sessions older than
// backgroundSessionMaxAge. A killed session stays listed until a later run, so
// its owner can still read why it ended.
func (sm *SessionManager) cleanupOldSessions() {
	now := time.Now()
	doneCutoff := now.Add(-doneSessionRetention).Unix()
	ageCutoff := now.Add(-backgroundSessionMaxAge).Unix()

	var expired []*ProcessSession
	sm.mu.Lock()
	for id, session := range sm.sessions {
		session.mu.Lock()
		status, start, end := session.Status, session.StartTime, session.EndTime
		session.mu.Unlock()
		switch {
		case status == "done" || status == "exited" || status == "error":
			if end < doneCutoff {
				delete(sm.sessions, id)
			}
		case status == "running" && start < ageCutoff:
			expired = append(expired, session)
		}
	}
	sm.mu.Unlock()

	// Kill outside the manager lock: killing a process tree can be slow
	// (taskkill on Windows).
	for _, session := range expired {
		if err := session.Kill(); err != nil {
			// ErrSessionDone means it exited on its own since the scan.
			if !errors.Is(err, ErrSessionDone) {
				logger.WarnCF("shell", "failed to kill expired background session",
					map[string]any{"session_id": session.ID, "error": err.Error()})
			}
			continue
		}
		session.mu.Lock()
		if session.outputBuffer != nil {
			session.outputBuffer.WriteString(expiredSessionNote)
		}
		session.mu.Unlock()
	}
}

func (sm *SessionManager) Add(session *ProcessSession) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.sessions[session.ID] = session
}

// AddIfBelow adds the session only while fewer than max sessions are running.
// The check and the insert happen under the same lock.
func (sm *SessionManager) AddIfBelow(session *ProcessSession, limit int) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.runningCountLocked() >= limit {
		return fmt.Errorf("too many background sessions running (max %d); kill or wait for one", limit)
	}
	sm.sessions[session.ID] = session
	return nil
}

// RunningCount returns how many sessions are running.
func (sm *SessionManager) RunningCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.runningCountLocked()
}

func (sm *SessionManager) runningCountLocked() int {
	n := 0
	for _, session := range sm.sessions {
		if session.GetStatus() == "running" {
			n++
		}
	}
	return n
}

func (sm *SessionManager) Get(sessionID string) (*ProcessSession, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, ok := sm.sessions[sessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}

	return session, nil
}

func (sm *SessionManager) Remove(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.sessions, sessionID)
}

func (sm *SessionManager) List() []SessionInfo {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make([]SessionInfo, 0, len(sm.sessions))
	for _, session := range sm.sessions {
		result = append(result, session.ToSessionInfo())
	}

	return result
}

func generateSessionID() string {
	return uuid.New().String()[:8]
}
