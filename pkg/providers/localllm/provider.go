package localllm

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/andre25costa-code/kuromatsu/pkg/providers/common"
	"github.com/andre25costa-code/kuromatsu/pkg/providers/protocoltypes"
)

// registry guarantees a single Provider (and therefore a single underlying
// llama context, ADR-003) per model path, even though the factory may
// construct a candidate once per agent instance.
var (
	registryMu sync.Mutex
	registry   = map[string]*Provider{}
)

// Provider adapts the local engine to providers.LLMProvider. The match is
// structural (via the protocoltypes aliases in pkg/providers/types.go), so
// this package never imports pkg/providers itself, avoiding an import
// cycle with pkg/providers/factory_provider.go.
type Provider struct {
	opts Options
	eng  engine
}

// NewProvider returns the process-wide shared Provider for opts.ModelPath,
// creating it on first call. FR-001, ADR-002/003.
func NewProvider(opts Options) (*Provider, error) {
	opts = opts.WithDefaults()
	if opts.ModelPath == "" {
		return nil, fmt.Errorf("localllm: ModelPath is required")
	}

	registryMu.Lock()
	defer registryMu.Unlock()
	if p, ok := registry[opts.ModelPath]; ok {
		return p, nil
	}
	p := &Provider{opts: opts, eng: newEngine()}
	registry[opts.ModelPath] = p
	return p, nil
}

// UnloadAll unloads every process-wide cached engine (ADR-017/FR-018,
// C3): called by the memguard PSI watchdog under sustained severe memory
// pressure, before the OOM killer would otherwise act. Safe to call even
// when nothing is loaded (each engine's unload() is itself idempotent --
// see engine_cgo.go's unloadLocked/unload).
func UnloadAll() {
	registryMu.Lock()
	snapshot := make([]*Provider, 0, len(registry))
	for _, p := range registry {
		snapshot = append(snapshot, p)
	}
	registryMu.Unlock()

	for _, p := range snapshot {
		p.eng.unload()
	}
}

// GetDefaultModel returns the model id derived from the GGUF filename
// (e.g. "Bonsai-1.7B-Q1_0" for ".../Bonsai-1.7B-Q1_0.gguf").
func (p *Provider) GetDefaultModel() string {
	base := filepath.Base(p.opts.ModelPath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// Chat renders the ChatML prompt, runs one completion, and parses the
// result back into content/reasoning/tool calls. AC-001-1, AC-002-*.
func (p *Provider) Chat(
	ctx context.Context,
	messages []protocoltypes.Message,
	tools []protocoltypes.ToolDefinition,
	model string,
	options map[string]any,
) (*protocoltypes.LLMResponse, error) {
	opts := p.opts
	if v, ok := common.AsInt(options["max_tokens"]); ok && v > 0 {
		opts.MaxPredict = v
	}
	if v, ok := common.AsFloat(options["temperature"]); ok {
		opts.Temperature = float32(v)
	}

	enableThinking := false
	if level, ok := options["thinking_level"].(string); ok && level != "" && level != "off" {
		enableThinking = true
	}

	prompt, coreEnd := RenderPromptParts(messages, tools, enableThinking)

	result, err := p.eng.completion(ctx, prompt, coreEnd, opts)
	if err != nil {
		return nil, err
	}

	content, reasoning, calls := ParseOutput(result.Text)

	finishReason := "stop"
	if len(calls) > 0 {
		finishReason = "tool_calls"
	}

	return &protocoltypes.LLMResponse{
		Content:          content,
		ReasoningContent: reasoning,
		ToolCalls:        calls,
		FinishReason:     finishReason,
		Usage: &protocoltypes.UsageInfo{
			PromptTokens:     result.PromptTokens,
			CompletionTokens: result.OutputTokens,
			TotalTokens:      result.PromptTokens + result.OutputTokens,
			CachedTokens:     result.CachedTokens,
			PrefillMs:        result.Prefill.Milliseconds(),
			GenerationMs:     result.Generation.Milliseconds(),
		},
	}, nil
}
