package providers

import (
	"context"

	"github.com/andre25costa-code/kuromatsu/pkg/providers/common"
)

type toolSchemaTransformProvider struct {
	delegate    LLMProvider
	transform   string
	compactOpts common.CompactSchemaOptions
}

type toolSchemaStreamingProvider struct {
	*toolSchemaTransformProvider
}

func wrapProviderWithToolSchemaTransform(delegate LLMProvider, transform string) (LLMProvider, error) {
	return wrapProviderWithToolSchemaTransformOptions(delegate, transform, common.CompactSchemaOptions{})
}

// wrapProviderWithToolSchemaTransformOptions is
// wrapProviderWithToolSchemaTransform with explicit CompactSchemaOptions
// (used by the "compact" transform; ignored by every other transform). Kept
// as a separate function so every existing call/test of the 2-arg form
// (default options) is untouched.
func wrapProviderWithToolSchemaTransformOptions(
	delegate LLMProvider,
	transform string,
	compactOpts common.CompactSchemaOptions,
) (LLMProvider, error) {
	transform, err := common.NormalizeToolSchemaTransform(transform)
	if err != nil {
		return nil, err
	}
	if transform == common.ToolSchemaTransformOff || delegate == nil {
		return delegate, nil
	}
	base := &toolSchemaTransformProvider{
		delegate:    delegate,
		transform:   transform,
		compactOpts: compactOpts,
	}
	if _, ok := delegate.(StreamingProvider); ok {
		return &toolSchemaStreamingProvider{toolSchemaTransformProvider: base}, nil
	}
	return base, nil
}

func (p *toolSchemaTransformProvider) Chat(
	ctx context.Context,
	messages []Message,
	tools []ToolDefinition,
	model string,
	options map[string]any,
) (*LLMResponse, error) {
	transformed, err := common.TransformToolDefinitionsWithOptions(tools, p.transform, p.compactOpts)
	if err != nil {
		return nil, err
	}
	return p.delegate.Chat(ctx, messages, transformed, model, options)
}

func (p *toolSchemaTransformProvider) GetDefaultModel() string {
	return p.delegate.GetDefaultModel()
}

func (p *toolSchemaStreamingProvider) ChatStream(
	ctx context.Context,
	messages []Message,
	tools []ToolDefinition,
	model string,
	options map[string]any,
	onChunk func(accumulated string),
) (*LLMResponse, error) {
	streaming := p.delegate.(StreamingProvider)
	transformed, err := common.TransformToolDefinitionsWithOptions(tools, p.transform, p.compactOpts)
	if err != nil {
		return nil, err
	}
	return streaming.ChatStream(ctx, messages, transformed, model, options, onChunk)
}

func (p *toolSchemaStreamingProvider) ChatStreamEvents(
	ctx context.Context,
	messages []Message,
	tools []ToolDefinition,
	model string,
	options map[string]any,
	onChunk func(StreamChunk),
) (*LLMResponse, error) {
	streaming, ok := p.delegate.(StreamingEventProvider)
	if !ok {
		return p.ChatStream(ctx, messages, tools, model, options, func(accumulated string) {
			if onChunk != nil {
				onChunk(StreamChunk{Content: accumulated})
			}
		})
	}
	transformed, err := common.TransformToolDefinitionsWithOptions(tools, p.transform, p.compactOpts)
	if err != nil {
		return nil, err
	}
	return streaming.ChatStreamEvents(ctx, messages, transformed, model, options, onChunk)
}

func (p *toolSchemaTransformProvider) SupportsThinking() bool {
	tc, ok := p.delegate.(ThinkingCapable)
	return ok && tc.SupportsThinking()
}

func (p *toolSchemaTransformProvider) SupportsNativeSearch() bool {
	ns, ok := p.delegate.(NativeSearchCapable)
	return ok && ns.SupportsNativeSearch()
}

func (p *toolSchemaTransformProvider) Close() {
	if stateful, ok := p.delegate.(StatefulProvider); ok {
		stateful.Close()
	}
}
