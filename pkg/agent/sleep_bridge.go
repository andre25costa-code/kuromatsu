package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
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

// newSleepBridge constructs the scheduler from explicit, already-resolved
// dependencies (mirroring newEvolutionBridge's signature style: registry,
// cfg, provider-ish things, rs -- never a live *AgentLoop whose fields
// could be concurrently swapped by a reload), or returns nil when there is
// nothing to schedule (see the doc comment above for the exact
// conditions). Called from NewAgentLoop and ReloadProviderAndConfig.
func newSleepBridge(
	cfg *config.Config,
	registry *AgentRegistry,
	providerFactory func(*config.ModelConfig) (providers.LLMProvider, string, error),
	rs *runstate.Engine,
) *sleepBridge {
	if cfg == nil || !cfg.Sleep.Enabled {
		return nil
	}
	if !cfg.Sleep.HasValidExternalModel(cfg.ModelList) {
		logger.WarnC("sleep", "modo dormir requer um modelo externo em sleep.unconscious_model")
		return nil
	}
	workspaces := registryWorkspaces(registry)
	if len(workspaces) == 0 {
		return nil
	}
	window, err := sleep.ParseWindow(cfg.Sleep.EffectiveWindow())
	if err != nil {
		logger.WarnCF("sleep", "invalid sleep.window, sleep bridge not scheduling", map[string]any{
			"window": cfg.Sleep.EffectiveWindow(),
			"error":  err.Error(),
		})
		return nil
	}

	sessions := newAgentSessionSource(registry)
	chat := newSleepChatFunc(cfg, providerFactory)

	sleepCfg := sleep.Config{
		Enabled:          true,
		Window:           cfg.Sleep.EffectiveWindow(),
		UnconsciousModel: cfg.Sleep.UnconsciousModel,
		WeeklyDeep:       cfg.Sleep.WeeklyDeep,
		MaxTokensBudget:  cfg.Sleep.MaxTokensBudget,
		DryRun:           cfg.Sleep.DryRun,
	}

	bgCtx, cancel := context.WithCancel(context.Background())
	bridge := &sleepBridge{
		cfg:        cfg.Sleep,
		runtime:    sleep.NewRuntime(sleepCfg, sessions, chat),
		rs:         rs,
		workspaces: workspaces,
		bgCtx:      bgCtx,
		cancel:     cancel,
	}
	bridge.start(window)
	return bridge
}

// sleepSnapshot returns al's current sleep bridge (nil when disabled),
// guarded by mu like focus/reflexes/runstate.
func (al *AgentLoop) sleepSnapshot() *sleepBridge {
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
	deadline := window.Deadline(time.Now())
	for {
		if b.bgCtx.Err() != nil {
			return
		}
		err := b.runOnce()
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
		case <-b.bgCtx.Done():
			return
		}
	}
}

// runOnce runs every known workspace through a fresh dreamGate. A busy
// refusal on any workspace stops the batch immediately so runWindow's
// retry re-attempts the whole set together, rather than interleaving
// partial per-workspace progress with retries.
func (b *sleepBridge) runOnce() error {
	gate := &dreamGate{inner: b.runtime, rs: b.rs}
	for _, workspace := range b.workspaces {
		if err := gate.RunColdPathOnce(b.bgCtx, workspace); err != nil {
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
	providerFactory func(*config.ModelConfig) (providers.LLMProvider, string, error),
) sleep.ChatFunc {
	if providerFactory == nil {
		providerFactory = providers.CreateProviderFromConfig
	}
	fallback := providers.NewFallbackChain(providers.NewCooldownTracker(), providers.NewRateLimiterRegistry())

	return func(ctx context.Context, systemPrompt, userPrompt string) (sleep.ChatResult, error) {
		modelCfg, err := resolvedRuntimeModelConfig(cfg, cfg.Sleep.UnconsciousModel, cfg.Agents.Defaults.Workspace)
		if err != nil {
			return sleep.ChatResult{}, fmt.Errorf("sleep: resolving unconscious_model %q: %w", cfg.Sleep.UnconsciousModel, err)
		}

		provider, modelID, err := providerFactory(modelCfg)
		if err != nil {
			return sleep.ChatResult{}, fmt.Errorf("sleep: creating provider for unconscious_model %q: %w", cfg.Sleep.UnconsciousModel, err)
		}
		defer closeProviderIfStateful(provider)

		candidate := providers.FallbackCandidate{
			Provider:    modelCfg.Provider,
			Model:       modelID,
			DisplayName: cfg.Sleep.UnconsciousModel,
		}
		messages := []providers.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		}

		result, err := fallback.ExecuteCandidate(
			ctx,
			[]providers.FallbackCandidate{candidate}, // AC-010-8: exactly this one candidate, no chain
			func(ctx context.Context, c providers.FallbackCandidate) (*providers.LLMResponse, error) {
				return provider.Chat(ctx, messages, nil, modelID, map[string]any{})
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

// agentSessionSource adapts the live AgentRegistry to sleep.SessionSource
// (collects real session digests instead of the fakes pkg/sleep's own
// tests use). Per-session "already consolidated" state is an in-memory
// message count, not the since timestamp SessionSource.RecentDigests
// receives: SessionStore exposes no per-session last-modified time to
// filter on, so a message-count high-water-mark is the closest
// alternative that stays real (not a fake always-return-everything). Like
// Runtime's own lastRun map, this resets on process restart -- consistent
// with the rest of pkg/sleep's fidelity, not a new gap.
//
// Known limitation (documented, not fixed here): the high-water-mark
// advances during collection, before runTriage actually consolidates
// anything -- if the chat call then fails partway through a run, the
// sessions already counted as "seen" are not retried on the next window.
// Accepted for this rodada: sleep is a best-effort, retry-from-scratch
// design end-to-end (S17), and building per-session success tracking
// would mean changing pkg/sleep's own interfaces, which this rodada's
// scope deliberately does not touch.
type agentSessionSource struct {
	registry *AgentRegistry

	mu   sync.Mutex
	seen map[string]map[string]int // workspace -> sessionKey -> last-seen message count
}

func newAgentSessionSource(registry *AgentRegistry) *agentSessionSource {
	return &agentSessionSource{registry: registry, seen: make(map[string]map[string]int)}
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
		seenForWorkspace = make(map[string]int)
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

			s.mu.Lock()
			lastCount := seenForWorkspace[key]
			s.mu.Unlock()
			if !weeklyDeep && len(history) <= lastCount {
				continue // nothing new since the last consolidation
			}

			summary := strings.TrimSpace(agentInst.Sessions.GetSummary(key))
			if summary == "" {
				summary = renderHistoryTail(history, sleepDigestTailMessages)
			}
			if summary == "" {
				continue
			}
			digests = append(digests, sleep.SessionDigest{SessionID: key, Summary: summary})

			s.mu.Lock()
			seenForWorkspace[key] = len(history)
			s.mu.Unlock()
		}
	}

	sort.Slice(digests, func(i, j int) bool { return digests[i].SessionID < digests[j].SessionID })
	return digests, nil
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
