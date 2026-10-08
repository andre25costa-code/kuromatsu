package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/logger"
	"github.com/andre25costa-code/kuromatsu/pkg/providers/common"
	"github.com/andre25costa-code/kuromatsu/pkg/refinery"
)

// defaultRetrievedBudgetTokens is used when memory_budget_tokens is unset.
const defaultRetrievedBudgetTokens = 600

// retrievedBlockHeader marks the per-turn memory block inside the user
// message, so the model does not read it as the user's own words.
const retrievedBlockHeader = "[Memory relevant to this message — retrieved from long-term memory, not written by the user]"

// retrievalStore returns the atom store for memory: retrieved (FR-022 phase
// 2), opening memory/atoms.db on first use only -- other memory modes never
// create it (AC-022-4). Each call imports manual edits to MEMORY.md first
// (AC-022-7; a no-op when the file is unchanged, and it never rewrites the
// file). It returns nil, after a warning logged at most once a day, when the
// store cannot be used or holds no atoms, so the caller falls back to core
// (AC-022-5).
func (cb *ContextBuilder) retrievalStore() *refinery.Store {
	cb.atomsMu.Lock()
	defer cb.atomsMu.Unlock()

	memoryDir := filepath.Join(cb.workspace, "memory")
	if cb.atoms == nil {
		store, err := refinery.OpenStore(filepath.Join(memoryDir, "atoms.db"))
		if err != nil {
			cb.warnRetrievalFallbackLocked("open atom store", err)
			return nil
		}
		cb.atoms = store
	}
	ctx := context.Background()
	if err := cb.atoms.SyncMemoryFile(ctx, filepath.Join(memoryDir, "MEMORY.md")); err != nil {
		cb.warnRetrievalFallbackLocked("import MEMORY.md", err)
		return nil
	}
	has, err := cb.atoms.HasActive(ctx)
	if err != nil {
		cb.warnRetrievalFallbackLocked("check atom store", err)
		return nil
	}
	if !has {
		cb.warnRetrievalFallbackLocked("atom store is empty", nil)
		return nil
	}
	return cb.atoms
}

func (cb *ContextBuilder) warnRetrievalFallbackLocked(reason string, err error) {
	today := time.Now().Format("2006-01-02")
	if cb.atomsWarnedDay == today {
		return
	}
	cb.atomsWarnedDay = today
	fields := map[string]any{"reason": reason}
	if err != nil {
		fields["error"] = err.Error()
	}
	logger.WarnCF("agent", "memory: retrieved falling back to core (FR-022)", fields)
}

func (cb *ContextBuilder) retrievedSelectOptions() refinery.SelectOptions {
	budget := cb.memoryBudget
	if budget <= 0 {
		budget = defaultRetrievedBudgetTokens
	}
	return refinery.SelectOptions{
		BudgetTokens:   budget,
		Lambda:         refinery.DefaultMMRLambda,
		MinHamming:     refinery.DefaultSelectHamming,
		EstimateTokens: estimateTextTokens,
	}
}

// retrievedFloorText renders the query-independent floor for the cached
// system prompt (AC-022-3). ok is false when the caller must fall back to
// core.
func (cb *ContextBuilder) retrievedFloorText() (text string, ok bool) {
	store := cb.retrievalStore()
	if store == nil {
		return "", false
	}
	floor, err := store.Floor(context.Background())
	if err != nil {
		logger.WarnCF("agent", "memory: retrieved could not read the floor", map[string]any{"error": err.Error()})
		return "", false
	}
	sel := refinery.Select(floor, nil, cb.retrievedSelectOptions())
	if sel.FloorTruncated {
		logger.WarnCF("agent", "memory: retrieved floor exceeds memory_budget_tokens; newest kept (AC-022-3)",
			map[string]any{"floor_atoms": len(floor), "kept": len(sel.Floor)})
	}
	return refinery.RenderLines(sel.Floor), true
}

// retrievedTurnBlock returns the atoms retrieved for message, minus the
// floor already in the system prompt, as the block prepended to this call's
// user message (AC-022-10). Empty when nothing matches or on fallback.
func (cb *ContextBuilder) retrievedTurnBlock(message string) string {
	store := cb.retrievalStore()
	if store == nil || strings.TrimSpace(message) == "" {
		return ""
	}
	// The header is part of the injected memory, so it is charged to the
	// budget too (AC-022-1).
	opts := cb.retrievedSelectOptions()
	opts.BudgetTokens -= estimateTextTokens(retrievedBlockHeader + "\n")
	if opts.BudgetTokens <= 0 {
		return ""
	}
	sel, err := refinery.Retrieve(context.Background(), store, message, refinery.DefaultMaxCandidates, opts)
	if err != nil {
		logger.WarnCF("agent", "memory: retrieved search failed", map[string]any{"error": err.Error()})
		return ""
	}
	if len(sel.Retrieved) == 0 {
		return ""
	}
	return retrievedBlockHeader + "\n" + refinery.RenderLines(sel.Retrieved)
}

// retrievedCacheSignature identifies the current floor, so a cached prompt
// variant is rebuilt when a rule/preference/pinned atom changes -- including
// atoms written by another connection, which no workspace mtime reflects.
func (cb *ContextBuilder) retrievedCacheSignature() string {
	text, ok := cb.retrievedFloorText()
	if !ok {
		return "fallback"
	}
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:8])
}

// Close releases the atom store opened for memory: retrieved, if any.
func (cb *ContextBuilder) Close() error {
	cb.atomsMu.Lock()
	defer cb.atomsMu.Unlock()
	if cb.atoms == nil {
		return nil
	}
	err := cb.atoms.Close()
	cb.atoms = nil
	return err
}

// retrievedOnNativeModel reports a memory: retrieved window whose first
// model is the in-process one. The per-turn block changes every call, and
// uncached prefill on the native model runs at about 1 token/s, so FR-022
// recommends retrieved with an external provider.
func retrievedOnNativeModel(memoryMode string, agent *AgentInstance) bool {
	if agent == nil || len(agent.Candidates) == 0 ||
		!strings.EqualFold(strings.TrimSpace(memoryMode), config.FocusMemoryRetrieved) {
		return false
	}
	first := agent.Candidates[0]
	return common.IsNativeModel(first.Provider, first.Model)
}

var warnRetrievedNativeOnce sync.Once

func warnIfRetrievedOnNativeModel(memoryMode string, agent *AgentInstance) {
	if !retrievedOnNativeModel(memoryMode, agent) {
		return
	}
	warnRetrievedNativeOnce.Do(func() {
		logger.WarnCF(
			"agent",
			"memory: retrieved on the native model: the per-turn memory block is re-prefilled every call (~1 tok/s); prefer an external provider for this window (FR-022)",
			map[string]any{"model": agent.Candidates[0].Model},
		)
	})
}
