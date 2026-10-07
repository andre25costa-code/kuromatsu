package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	runtimeevents "github.com/andre25costa-code/kuromatsu/pkg/events"
	"github.com/andre25costa-code/kuromatsu/pkg/logger"
	"github.com/andre25costa-code/kuromatsu/pkg/runstate"
	"github.com/andre25costa-code/kuromatsu/pkg/sysinfo"
	"github.com/andre25costa-code/kuromatsu/pkg/telemetry"
)

// telemetryPruneInterval is how often the daily Prune routine runs
// (AC-019-5). A day is the unit the ADR/AC talk about; this ticks more
// often than "once a day" would strictly need only in the sense that the
// first prune fires immediately at startup (see startPruneLoop) rather
// than waiting a full day after telemetry is first enabled.
const telemetryPruneInterval = 24 * time.Hour

// defaultStatsWindowHours is /stats' default lookback when no hours
// argument is given (AC-019-4's own example uses 24).
const defaultStatsWindowHours = 24

// telemetryBridge is the Trilho C telemetry scheduler (FR-019/C4,
// ADR-017): it owns the *telemetry.Store/Recorder pair, subscribes to
// KindAgentTurnEnd to write one row per completed turn (AC-019-1), and
// runs a daily Prune (AC-019-5). nil whenever telemetry.enabled=false
// (AC-019-6) — no turns.db file is ever created. Every method is
// nil-receiver-safe, matching evolutionBridge/sleepBridge's own convention.
type telemetryBridge struct {
	cfg      config.TelemetryConfig
	store    *telemetry.Store
	recorder *telemetry.Recorder
	rs       *runstate.Engine

	sub runtimeevents.Subscription

	bgCtx  context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// newTelemetryBridge opens turns.db and starts the Recorder + prune loop,
// or returns (nil, nil) when telemetry.enabled=false (AC-019-6) -- callers
// mirror newSleepBridge/newEvolutionBridge's "nil means disabled, don't
// treat it as an error" convention.
func newTelemetryBridge(cfg *config.Config, rs *runstate.Engine) (*telemetryBridge, error) {
	if cfg == nil || !cfg.Telemetry.Enabled {
		return nil, nil
	}
	dbPath := cfg.Telemetry.EffectiveDBPath(config.GetHome())
	if dbPath == "" {
		// Unreachable given the Enabled check above (EffectiveDBPath is
		// only ever "" when disabled), kept as a defensive no-op rather
		// than a panic/assert.
		return nil, nil
	}

	store, err := telemetry.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("telemetry: %w", err)
	}
	recorder := telemetry.NewRecorder(store)

	bgCtx, cancel := context.WithCancel(context.Background())
	b := &telemetryBridge{
		cfg:      cfg.Telemetry,
		store:    store,
		recorder: recorder,
		rs:       rs,
		bgCtx:    bgCtx,
		cancel:   cancel,
	}
	b.startPruneLoop()
	return b, nil
}

// telemetrySnapshot returns al's current telemetry bridge (nil when
// disabled), guarded by mu like focus/reflexes/runstate/sleep.
func (al *AgentLoop) telemetrySnapshot() *telemetryBridge {
	al.mu.RLock()
	defer al.mu.RUnlock()
	return al.telemetry
}

// Close stops the prune loop and the runtime-event subscription, then
// closes the Recorder (which drains its goroutine and closes the Store).
// Safe on a nil *telemetryBridge.
func (b *telemetryBridge) Close() error {
	if b == nil {
		return nil
	}
	if b.sub != nil {
		if err := b.sub.Close(); err != nil {
			logger.WarnCF("agent", "Failed to close telemetry runtime event subscription", map[string]any{
				"error": err.Error(),
			})
		}
		<-b.sub.Done()
	}
	b.cancel()
	b.wg.Wait()
	return b.recorder.Close()
}

// subscribeRuntimeEvents wires OnRuntimeEvent onto ch's KindAgentTurnEnd
// stream -- mirrors evolutionBridge.subscribeRuntimeEvents exactly,
// including why Block: the bridge's own work per event (build a
// TurnRecord, hand it to the Recorder's own non-blocking channel) is fast
// and never itself blocks on I/O, so there's no reason to ever drop a
// turn-end event here the way DropNewest/DropOldest would.
func (b *telemetryBridge) subscribeRuntimeEvents(ch runtimeevents.EventChannel) error {
	if b == nil || ch == nil {
		return nil
	}
	sub, err := ch.Source("agent").OfKind(runtimeevents.KindAgentTurnEnd).Subscribe(
		b.bgCtx,
		runtimeevents.SubscribeOptions{
			Name:         "telemetry-bridge",
			Buffer:       hookObserverBufferSize,
			Backpressure: runtimeevents.Block,
			Concurrency:  runtimeevents.Locked,
		},
		b.OnRuntimeEvent,
	)
	if err != nil {
		return err
	}
	b.sub = sub
	return nil
}

// OnRuntimeEvent records one turns.db row per KindAgentTurnEnd
// (AC-019-1). Every field the S34/AC-019-1 column list needs beyond what
// TurnEndPayload already carries (session_key via meta, mode_bits, rss_kb)
// is filled in here.
func (b *telemetryBridge) OnRuntimeEvent(_ context.Context, evt runtimeevents.Event) error {
	if b == nil || evt.Kind != runtimeevents.KindAgentTurnEnd {
		return nil
	}
	payload, ok := evt.Payload.(TurnEndPayload)
	if !ok {
		return nil
	}
	meta := hookMetaFromRuntimeEvent(evt)

	b.recorder.Record(telemetry.TurnRecord{
		Origin:           payload.Origin,
		SessionKey:       meta.SessionKey,
		Window:           payload.FocusWindow,
		Escalations:      payload.FocusEscalations,
		UnknownToolCalls: payload.UnknownToolCalls,
		PromptTokens:     payload.PromptTokens,
		CachedTokens:     payload.CachedTokens,
		OutputTokens:     payload.OutputTokens,
		PrefillMs:        payload.PrefillMs,
		GenMs:            payload.GenMs,
		TotalMs:          payload.Duration.Milliseconds(),
		ToolsCalled:      len(payload.ToolExecutions),
		Iterations:       payload.Iterations,
		Status:           string(payload.Status),
		ModeBits:         b.modeBits(),
		RSSKb:            processRSSKb(),
		StealPct:         payload.StealPct,
	})
	return nil
}

// recordReflex records the AC-013-6/AC-019-3 row for a reflex match:
// reflex.go's tryReflex never reaches runTurn (no LLM call is ever made),
// so there is no KindAgentTurnEnd for OnRuntimeEvent to observe -- this is
// the "reflexos gravam direto" path the plan calls for. prompt_tokens/
// output_tokens/cached_tokens/prefill_ms/gen_ms/escalations/
// unknown_tool_calls/iterations are all left at their zero value:
// none of those apply to a reflex (no LLM call, no focus routing, no tool
// escalation loop).
func (b *telemetryBridge) recordReflex(sessionKey string) {
	if b == nil {
		return
	}
	b.recorder.Record(telemetry.TurnRecord{
		Origin:     OriginReflex,
		SessionKey: sessionKey,
		Status:     string(TurnEndStatusCompleted),
		ModeBits:   b.modeBits(),
		RSSKb:      processRSSKb(),
	})
}

func (b *telemetryBridge) modeBits() uint32 {
	if b == nil || b.rs == nil {
		return 0
	}
	return uint32(b.rs.Snapshot())
}

// processRSSKb reads the current process's RSS via sysinfo.ProcessRSS,
// returning 0 on a platform without /proc (e.g. Windows) -- graceful
// degradation, not an error, matching sysinfo's own contract.
func processRSSKb() int64 {
	rssBytes, ok := sysinfo.ProcessRSS(os.Getpid())
	if !ok {
		return 0
	}
	return int64(rssBytes / 1024)
}

// startPruneLoop runs Prune once immediately (so enabling telemetry on an
// already-long-running process doesn't wait a full day for its first
// prune) and then every telemetryPruneInterval until Close.
func (b *telemetryBridge) startPruneLoop() {
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		b.pruneOnce()
		ticker := time.NewTicker(telemetryPruneInterval)
		defer ticker.Stop()
		for {
			select {
			case <-b.bgCtx.Done():
				return
			case <-ticker.C:
				b.pruneOnce()
			}
		}
	}()
}

func (b *telemetryBridge) pruneOnce() {
	removed, err := b.store.Prune(b.bgCtx, b.cfg.EffectiveRetentionDays())
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		logger.WarnCF("telemetry", "prune failed", map[string]any{"error": err.Error()})
		return
	}
	if removed > 0 {
		logger.InfoCF("telemetry", "pruned old turn records", map[string]any{
			"removed":        removed,
			"retention_days": b.cfg.EffectiveRetentionDays(),
		})
	}
}

// queryStats answers /stats [window] [hours] (AC-019-4). Nil-receiver-safe
// so cmd_stats.go's Handler can call it unconditionally and get a clear
// message back instead of a generic "unavailable" when telemetry is off.
func (b *telemetryBridge) queryStats(ctx context.Context, window string, hours int) (string, error) {
	if b == nil {
		return "", fmt.Errorf("telemetry is disabled (telemetry.enabled=false)")
	}
	if hours <= 0 {
		hours = defaultStatsWindowHours
	}
	since := time.Now().Add(-time.Duration(hours) * time.Hour)

	byWindow, err := b.store.StatsByWindow(ctx, since, window)
	if err != nil {
		return "", fmt.Errorf("telemetry: %w", err)
	}
	byHour, err := b.store.StatsByHourOfDay(ctx, since, window)
	if err != nil {
		return "", fmt.Errorf("telemetry: %w", err)
	}
	return formatStatsReport(window, hours, byWindow, byHour), nil
}

func formatStatsReport(window string, hours int, byWindow, byHour []telemetry.Aggregate) string {
	scope := "all windows"
	if window != "" {
		scope = fmt.Sprintf("window %q", window)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Telemetry -- last %dh (%s)\n", hours, scope)

	fmt.Fprintf(&sb, "\nBy window:\n")
	if len(byWindow) == 0 {
		sb.WriteString("  (no turns recorded in this range)\n")
	}
	for _, a := range byWindow {
		writeStatsLine(&sb, aggregateLabel(a.Label, "(none)"), a)
	}

	fmt.Fprintf(&sb, "\nBy hour of day (UTC):\n")
	if len(byHour) == 0 {
		sb.WriteString("  (no turns recorded in this range)\n")
	}
	sorted := append([]telemetry.Aggregate(nil), byHour...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Label < sorted[j].Label })
	for _, a := range sorted {
		writeStatsLine(&sb, a.Label, a)
	}

	return strings.TrimRight(sb.String(), "\n")
}

func aggregateLabel(label, emptyLabel string) string {
	if label == "" {
		return emptyLabel
	}
	return label
}

func writeStatsLine(b *strings.Builder, label string, a telemetry.Aggregate) {
	fmt.Fprintf(b, "  %-10s n=%-4d prompt~%-6.0f cache~%-5.1f%% out~%-5.0f avg=%-6.0fms p50=%-6.0fms p90=%-6.0fms\n",
		label, a.Count, a.AvgPromptTokens, a.AvgCachedPercent, a.AvgOutputTokens, a.AvgTotalMs, a.P50Ms, a.P90Ms)
}
