package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/fileutil"
	"github.com/andre25costa-code/kuromatsu/pkg/logger"
	"github.com/andre25costa-code/kuromatsu/pkg/providers"
	"github.com/andre25costa-code/kuromatsu/pkg/runstate"
	"github.com/andre25costa-code/kuromatsu/pkg/sleep"
)

// sleepBusyRetryInterval is how often a sleep run retries after a busy
// refusal, until the window's own deadline (AC-010-4/ADR-016 point 7's
// "sono re-tenta até o deadline da janela").
const sleepBusyRetryInterval = 5 * time.Minute

// sleepDigestTailMessages caps how much of a session's raw history backs a
// SessionDigest when the session has no summary yet -- keeps a single
// digest bounded regardless of how long the conversation ran.
const sleepDigestTailMessages = 20

// sleepBridge is the E9 parte 2 scheduler (FR-010, ADR-006/018): it owns a
// *sleep.Runtime wired to real session digests and the configured external
// model, and fires it once a night inside the configured window --
// mirroring evolutionBridge.startScheduledColdPath, but on its own timer
// (sleep has no per-turn trigger) and always gated through a dreamGate
// (Dream, same bit evolution uses, ADR-016 point 7: sleep and evolution
// never run at the same time as each other or as a user turn).
//
// nil whenever sleep.enabled=false, sleep.unconscious_model doesn't
// resolve to a valid non-native model (ADR-018/AC-010-7 -- logged, not a
// fatal boot error), or there is no workspace to consolidate. Every
// method is nil-receiver-safe, matching evolutionBridge's own convention.
type sleepBridge struct {
	cfg        config.SleepConfig
	runtime    *sleep.Runtime
	rs         *runstate.Engine
	workspaces []string

	bgCtx  context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// sleepScheduler owns zero or more independent sleepBridge instances, one
// per distinct effective sleep config among the registry's agents (ADR-019
// "sono configurável por agente"). With agents.list empty, or with no
// agent overriding sleep, every agent resolves to the same
// cfg.Sleep -- exactly one group, one bridge, byte-identical to the single
// global sleepBridge that existed before per-agent sleep. Nil-receiver-safe
// like sleepBridge itself; nil when there is nothing to schedule at all.
type sleepScheduler struct {
	bridges []*sleepBridge
}

// Close stops every bridge's timer goroutine and waits for in-flight runs
// to observe cancellation. Safe on a nil *sleepScheduler.
func (s *sleepScheduler) Close() error {
	if s == nil {
		return nil
	}
	for _, b := range s.bridges {
		b.Close()
	}
	return nil
}

// newSleepScheduler groups the registry's agents by their effective sleep
// config (Config.EffectiveSleep), then builds one independent sleepBridge
// per group whose config is actually Enabled and resolves to a valid
// external model. Called from NewAgentLoop and ReloadProviderAndConfig --
// never a live *AgentLoop whose fields could be concurrently swapped by a
// reload.
func newSleepScheduler(
	cfg *config.Config,
	registry *AgentRegistry,
	providerFactory func(*config.ModelConfig) (providers.LLMProvider, string, error),
	rs *runstate.Engine,
) *sleepScheduler {
	if cfg == nil || registry == nil {
		return nil
	}

	groups := make(map[config.SleepConfig][]string)
	registry.mu.RLock()
	for id, agent := range registry.agents {
		if agent == nil {
			continue
		}
		workspace := strings.TrimSpace(agent.Workspace)
		if workspace == "" {
			continue
		}
		sleepCfg := cfg.EffectiveSleep(id)
		found := false
		for _, w := range groups[sleepCfg] {
			if w == workspace {
				found = true
				break
			}
		}
		if !found {
			groups[sleepCfg] = append(groups[sleepCfg], workspace)
		}
	}
	registry.mu.RUnlock()

	var bridges []*sleepBridge
	for sleepCfg, workspaces := range groups {
		if b := newSleepBridgeForGroup(cfg, sleepCfg, workspaces, registry, providerFactory, rs); b != nil {
			bridges = append(bridges, b)
		}
	}
	if len(bridges) == 0 {
		return nil
	}
	return &sleepScheduler{bridges: bridges}
}

// newSleepBridgeForGroup builds one sleepBridge for the agents that share
// sleepCfg as their effective sleep config, scheduling only those
// workspaces. Returns nil under exactly the same conditions the single
// global bridge used to (disabled, no valid external model, no workspace,
// invalid window) -- see the doc comment on sleepBridge above.
func newSleepBridgeForGroup(
	cfg *config.Config,
	sleepCfg config.SleepConfig,
	workspaces []string,
	registry *AgentRegistry,
	providerFactory func(*config.ModelConfig) (providers.LLMProvider, string, error),
	rs *runstate.Engine,
) *sleepBridge {
	if !sleepCfg.Enabled {
		return nil
	}
	if !sleepCfg.HasValidExternalModel(cfg.ModelList) {
		logger.WarnCF("sleep", "modo dormir requer um modelo externo em unconscious_model", map[string]any{
			"workspaces": workspaces,
		})
		return nil
	}
	if len(workspaces) == 0 {
		return nil
	}
	window, err := sleep.ParseWindow(sleepCfg.EffectiveWindow())
	if err != nil {
		logger.WarnCF("sleep", "invalid sleep window, sleep bridge not scheduling", map[string]any{
			"window": sleepCfg.EffectiveWindow(),
			"error":  err.Error(),
		})
		return nil
	}

	sessions := newAgentSessionSource(registry)
	chat := newSleepChatFunc(cfg, sleepCfg.UnconsciousModel, providerFactory)

	runtimeCfg := sleep.Config{
		Enabled:          true,
		Window:           sleepCfg.EffectiveWindow(),
		UnconsciousModel: sleepCfg.UnconsciousModel,
		WeeklyDeep:       sleepCfg.WeeklyDeep,
		MaxTokensBudget:  sleepCfg.MaxTokensBudget,
		DryRun:           sleepCfg.DryRun,
	}

	bgCtx, cancel := context.WithCancel(context.Background())
	bridge := &sleepBridge{
		cfg:        sleepCfg,
		runtime:    sleep.NewRuntime(runtimeCfg, sessions, chat),
		rs:         rs,
		workspaces: workspaces,
		bgCtx:      bgCtx,
		cancel:     cancel,
	}
	bridge.start(window)
	return bridge
}

// sleepSnapshot returns al's current sleep scheduler (nil when disabled),
// guarded by mu like focus/reflexes/runstate.
func (al *AgentLoop) sleepSnapshot() *sleepScheduler {
	al.mu.RLock()
	defer al.mu.RUnlock()
	return al.sleep
}

// Close stops the scheduler's timer goroutine and waits for any in-flight
// run to observe cancellation. Safe on a nil *sleepBridge.
func (b *sleepBridge) Close() error {
	if b == nil {
		return nil
	}
	b.cancel()
	b.wg.Wait()
	return nil
}

func (b *sleepBridge) start(window sleep.Window) {
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for {
			next := window.NextStart(time.Now())
			timer := time.NewTimer(time.Until(next))
			select {
			case <-timer.C:
				b.runWindow(window)
			case <-b.bgCtx.Done():
				timer.Stop()
				return
			}
		}
	}()
}

// runWindow runs one consolidation pass across every known workspace,
// retrying on a busy refusal every sleepBusyRetryInterval until the
// window's own deadline (AC-010-4) -- a real failure (anything other than
// runstate.ErrBusy) is logged and the window is abandoned; the next
// scheduled window tries again from scratch (S17: "sem estado parcial
// persistido entre tentativas").
func (b *sleepBridge) runWindow(window sleep.Window) {
	b.runWindowUntil(window.Deadline(time.Now()))
}

func (b *sleepBridge) runWindowUntil(deadline time.Time) {
	ctx, cancel := context.WithDeadline(b.bgCtx, deadline)
	defer cancel()
	for {
		if ctx.Err() != nil {
			return
		}
		err := b.runOnceContext(ctx)
		if err == nil {
			return
		}
		if !errors.Is(err, runstate.ErrBusy) {
			logger.WarnCF("sleep", "cold path run failed", map[string]any{"error": err.Error()})
			return
		}

		wait := sleepBusyRetryInterval
		if remaining := time.Until(deadline); remaining < wait {
			wait = remaining
		}
		if wait <= 0 {
			logger.InfoC("sleep", "window deadline reached while busy; skipping until the next window")
			return
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return
		}
	}
}

// runOnce runs every known workspace through a fresh dreamGate. A busy
// refusal on any workspace stops the batch immediately so runWindow's
// retry re-attempts the whole set together, rather than interleaving
// partial per-workspace progress with retries.
func (b *sleepBridge) runOnce() error {
	return b.runOnceContext(b.bgCtx)
}

func (b *sleepBridge) runOnceContext(ctx context.Context) error {
	gate := &dreamGate{inner: b.runtime, rs: b.rs}
	for _, workspace := range b.workspaces {
		if err := gate.RunColdPathOnce(ctx, workspace); err != nil {
			return err
		}
	}
	return nil
}

// newSleepChatFunc builds the sleep.ChatFunc that calls exclusively the
// configured unconscious_model (AC-010-8): its own single-candidate
// FallbackChain.ExecuteCandidate, tools=nil, never the agent's normal
// fallback chain and never bonsai-local. Deliberately does not touch
// al.activeRequestsInc/Dec or al.rsEnter(Inference) -- Dream is the
// occupancy bit for this call (held by the dreamGate wrapping
// RunColdPathOnce), not Inference; entering Inference here would make the
// sleep run preempt itself the instant it started.
func newSleepChatFunc(
	cfg *config.Config,
	unconsciousModel string,
	providerFactory func(*config.ModelConfig) (providers.LLMProvider, string, error),
) sleep.ChatFunc {
	if providerFactory == nil {
		providerFactory = providers.CreateProviderFromConfig
	}
	fallback := providers.NewFallbackChain(providers.NewCooldownTracker(), providers.NewRateLimiterRegistry())

	return func(ctx context.Context, systemPrompt, userPrompt string) (sleep.ChatResult, error) {
		maxTokens, err := sleep.OutputTokenLimit(ctx, systemPrompt, userPrompt)
		if err != nil {
			return sleep.ChatResult{}, err
		}
		modelCfg, err := resolvedRuntimeModelConfig(cfg, unconsciousModel, cfg.Agents.Defaults.Workspace)
		if err != nil {
			return sleep.ChatResult{}, fmt.Errorf(
				"sleep: resolving unconscious_model %q: %w",
				unconsciousModel,
				err,
			)
		}

		provider, modelID, err := providerFactory(modelCfg)
		if err != nil {
			return sleep.ChatResult{}, fmt.Errorf(
				"sleep: creating provider for unconscious_model %q: %w",
				unconsciousModel,
				err,
			)
		}
		defer closeProviderIfStateful(provider)

		candidate := providers.FallbackCandidate{
			Provider:    modelCfg.Provider,
			Model:       modelID,
			DisplayName: unconsciousModel,
		}
		messages := []providers.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		}

		result, err := fallback.ExecuteCandidate(
			ctx,
			[]providers.FallbackCandidate{candidate}, // AC-010-8: exactly this one candidate, no chain
			func(ctx context.Context, c providers.FallbackCandidate) (*providers.LLMResponse, error) {
				return provider.Chat(ctx, messages, nil, modelID, map[string]any{"max_tokens": maxTokens})
			},
		)
		if err != nil {
			return sleep.ChatResult{}, err
		}
		if result == nil || result.Response == nil {
			return sleep.ChatResult{}, fmt.Errorf("sleep: unconscious_model returned an empty response")
		}

		totalTokens := 0
		if result.Response.Usage != nil {
			totalTokens = result.Response.Usage.TotalTokens
		}
		return sleep.ChatResult{Text: result.Response.Content, TotalTokens: totalTokens}, nil
	}
}

// agentSessionSource tracks persisted content revisions, acknowledged only after memory is saved.
type agentSessionSource struct {
	registry *AgentRegistry

	mu   sync.Mutex
	seen map[string]map[string]string // workspace -> sessionKey -> committed content hash
}

func newAgentSessionSource(registry *AgentRegistry) *agentSessionSource {
	return &agentSessionSource{registry: registry, seen: make(map[string]map[string]string)}
}

func (s *agentSessionSource) RecentDigests(
	_ context.Context,
	workspace string,
	_ time.Time,
	weeklyDeep bool,
) ([]sleep.SessionDigest, error) {
	agents := s.agentsForWorkspace(workspace)
	if len(agents) == 0 {
		return nil, nil
	}

	s.mu.Lock()
	seenForWorkspace := s.seen[workspace]
	if seenForWorkspace == nil {
		seenForWorkspace = make(map[string]string)
		data, err := os.ReadFile(filepath.Join(workspace, "state", "sleep-cursors.json"))
		if err == nil {
			if err = json.Unmarshal(data, &seenForWorkspace); err != nil {
				s.mu.Unlock()
				return nil, fmt.Errorf("sleep cursor: %w", err)
			}
		} else if !os.IsNotExist(err) {
			s.mu.Unlock()
			return nil, err
		}
		if seenForWorkspace == nil {
			seenForWorkspace = make(map[string]string)
		}
		s.seen[workspace] = seenForWorkspace
	}
	s.mu.Unlock()

	var digests []sleep.SessionDigest
	visitedKeys := make(map[string]struct{})
	for _, agentInst := range agents {
		if agentInst == nil || agentInst.Sessions == nil {
			continue
		}
		for _, key := range agentInst.Sessions.ListSessions() {
			if _, dup := visitedKeys[key]; dup {
				continue
			}
			visitedKeys[key] = struct{}{}

			history := agentInst.Sessions.GetHistory(key)
			if len(history) == 0 {
				continue
			}

			summary := strings.TrimSpace(agentInst.Sessions.GetSummary(key))
			tail := renderHistoryTail(history, sleepDigestTailMessages)
			if summary != "" {
				summary += "\n\nRecent messages:\n"
			}
			summary += tail
			snapshot, err := json.Marshal(history)
			if err != nil {
				return nil, err
			}
			revision := fmt.Sprintf("%x", sha256.Sum256(append(snapshot, []byte(summary)...)))
			s.mu.Lock()
			previous := seenForWorkspace[key]
			s.mu.Unlock()
			if !weeklyDeep && revision == previous {
				continue
			}
			if summary == "" {
				continue
			}
			digests = append(digests, sleep.SessionDigest{SessionID: key, Summary: summary, Revision: revision})

		}
	}

	sort.Slice(digests, func(i, j int) bool { return digests[i].SessionID < digests[j].SessionID })
	return digests, nil
}

func (s *agentSessionSource) Acknowledge(ctx context.Context, workspace string, digests []sleep.SessionDigest) error {
	if len(digests) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make(map[string]string)
	for key, revision := range s.seen[workspace] {
		next[key] = revision
	}
	for _, digest := range digests {
		next[digest.SessionID] = digest.Revision
	}
	data, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if err := fileutil.WriteFileAtomic(
		filepath.Join(workspace, "state", "sleep-cursors.json"),
		data,
		0o600,
	); err != nil {
		return err
	}
	s.seen[workspace] = next
	return nil
}

func (s *agentSessionSource) agentsForWorkspace(workspace string) []*AgentInstance {
	if s == nil || s.registry == nil {
		return nil
	}
	s.registry.mu.RLock()
	defer s.registry.mu.RUnlock()
	var out []*AgentInstance
	for _, a := range s.registry.agents {
		if a != nil && a.Workspace == workspace {
			out = append(out, a)
		}
	}
	return out
}

// renderHistoryTail renders the last maxMessages of history as
// "role: content" lines, skipping empty content. Used only when a session
// has no summary yet (GetSummary returns "").
func renderHistoryTail(history []providers.Message, maxMessages int) string {
	start := 0
	if len(history) > maxMessages {
		start = len(history) - maxMessages
	}
	var b strings.Builder
	for _, msg := range history[start:] {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		fmt.Fprintf(&b, "%s: %s\n", msg.Role, content)
	}
	return strings.TrimSpace(b.String())
}
