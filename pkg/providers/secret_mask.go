package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/andre25costa-code/kuromatsu/pkg/refinery"
)

// FR-029 (ADR-020 P4): credentials never leave the VM in clear text for an
// external provider. Each request gets its own masker: credentials found in
// the outgoing messages become "[SECRET_n]" placeholders, and placeholders
// the model echoes back are restored locally (response text and tool-call
// arguments) so tools keep receiving the real value. Nothing is masked in
// what the agent persists, and nothing that isn't a credential is touched.

// The credential shapes are refinery.SecretPatterns (ported from
// context-refinery's clean.py), shared with the atoms' "sensitive" flag.

const secretPlaceholderPrefix = "[SECRET_"

// shouldMaskProtocol reports whether a provider protocol sends data off the
// VM. Only the in-process native model doesn't (AC-029-3).
func shouldMaskProtocol(protocol string) bool {
	return protocol != "native"
}

type secretMasker struct {
	byValue       map[string]string // secret -> placeholder
	byPlaceholder map[string]string // placeholder -> secret
}

func newSecretMasker() *secretMasker {
	return &secretMasker{byValue: map[string]string{}, byPlaceholder: map[string]string{}}
}

func (m *secretMasker) placeholder(secret string) string {
	if ph, ok := m.byValue[secret]; ok {
		return ph
	}
	ph := fmt.Sprintf("%s%d]", secretPlaceholderPrefix, len(m.byValue)+1)
	m.byValue[secret] = ph
	m.byPlaceholder[ph] = secret
	return ph
}

// mask replaces every credential in s, reporting whether anything changed.
func (m *secretMasker) mask(s string) (string, bool) {
	changed := false
	for _, p := range refinery.SecretPatterns {
		matches := p.Re.FindAllStringSubmatchIndex(s, -1)
		if len(matches) == 0 {
			continue
		}
		var b strings.Builder
		last := 0
		for _, idx := range matches {
			start, end := idx[2*p.Group], idx[2*p.Group+1]
			if start < 0 || strings.HasPrefix(s[start:end], secretPlaceholderPrefix) {
				continue
			}
			b.WriteString(s[last:start])
			b.WriteString(m.placeholder(s[start:end]))
			last = end
			changed = true
		}
		b.WriteString(s[last:])
		s = b.String()
	}
	return s, changed
}

func (m *secretMasker) unmask(s string) string {
	if len(m.byPlaceholder) == 0 || !strings.Contains(s, secretPlaceholderPrefix) {
		return s
	}
	for ph, secret := range m.byPlaceholder {
		s = strings.ReplaceAll(s, ph, secret)
	}
	return s
}

// unmaskJSON restores placeholders inside a JSON document (tool-call
// arguments), escaping each secret as a JSON string fragment.
func (m *secretMasker) unmaskJSON(s string) string {
	if len(m.byPlaceholder) == 0 || !strings.Contains(s, secretPlaceholderPrefix) {
		return s
	}
	for ph, secret := range m.byPlaceholder {
		quoted, _ := json.Marshal(secret)
		s = strings.ReplaceAll(s, ph, string(quoted[1:len(quoted)-1]))
	}
	return s
}

// maskValue returns a masked deep copy of a decoded JSON value, leaving v
// untouched; changed reports whether any credential was found.
func (m *secretMasker) maskValue(v any) (any, bool) {
	switch val := v.(type) {
	case string:
		return m.mask(val)
	case map[string]any:
		out, changed := make(map[string]any, len(val)), false
		for k, item := range val {
			masked, c := m.maskValue(item)
			out[k], changed = masked, changed || c
		}
		return out, changed
	case []any:
		out, changed := make([]any, len(val)), false
		for i, item := range val {
			masked, c := m.maskValue(item)
			out[i], changed = masked, changed || c
		}
		return out, changed
	default:
		return v, false
	}
}

func (m *secretMasker) unmaskValue(v any) any {
	switch val := v.(type) {
	case string:
		return m.unmask(val)
	case map[string]any:
		for k, item := range val {
			val[k] = m.unmaskValue(item)
		}
		return val
	case []any:
		for i, item := range val {
			val[i] = m.unmaskValue(item)
		}
		return val
	default:
		return v
	}
}

// maskMessages returns copies of messages with credentials masked; the
// caller's slice is never modified. When nothing matches, the original
// slice is returned as is.
func (m *secretMasker) maskMessages(messages []Message) []Message {
	var out []Message
	for i, msg := range messages {
		masked, changed := m.maskMessage(msg)
		if changed && out == nil {
			out = append(make([]Message, 0, len(messages)), messages[:i]...)
		}
		if out != nil {
			out = append(out, masked)
		}
	}
	if out == nil {
		return messages
	}
	return out
}

func (m *secretMasker) maskMessage(msg Message) (Message, bool) {
	anyChange := false
	maskInto := func(s *string) {
		if masked, changed := m.mask(*s); changed {
			*s, anyChange = masked, true
		}
	}
	maskInto(&msg.Content)
	maskInto(&msg.ReasoningContent)
	if len(msg.SystemParts) > 0 {
		parts := append([]ContentBlock(nil), msg.SystemParts...)
		for i := range parts {
			maskInto(&parts[i].Text)
		}
		msg.SystemParts = parts
	}
	if len(msg.ToolCalls) > 0 {
		calls := append([]ToolCall(nil), msg.ToolCalls...)
		for i := range calls {
			if calls[i].Function != nil {
				fn := *calls[i].Function
				maskInto(&fn.Arguments)
				calls[i].Function = &fn
			}
			// anthropic, anthropic_messages and bedrock serialize past
			// tool calls from this map, not from Function.Arguments.
			if calls[i].Arguments != nil {
				if masked, changed := m.maskValue(calls[i].Arguments); changed {
					calls[i].Arguments, anyChange = masked.(map[string]any), true
				}
			}
		}
		msg.ToolCalls = calls
	}
	return msg, anyChange
}

func (m *secretMasker) unmaskResponse(resp *LLMResponse) *LLMResponse {
	if resp == nil || len(m.byPlaceholder) == 0 {
		return resp
	}
	resp.Content = m.unmask(resp.Content)
	resp.ReasoningContent = m.unmask(resp.ReasoningContent)
	for i := range resp.ToolCalls {
		tc := &resp.ToolCalls[i]
		if tc.Function != nil {
			tc.Function.Arguments = m.unmaskJSON(tc.Function.Arguments)
		}
		if tc.Arguments != nil {
			m.unmaskValue(tc.Arguments)
		}
	}
	return resp
}

type secretMaskProvider struct {
	delegate LLMProvider
}

type secretMaskStreamingProvider struct {
	*secretMaskProvider
}

// wrapProviderWithSecretMask wraps an external provider with FR-029's
// reversible credential masking, keeping its optional interfaces.
func wrapProviderWithSecretMask(delegate LLMProvider) LLMProvider {
	if delegate == nil {
		return nil
	}
	base := &secretMaskProvider{delegate: delegate}
	if _, ok := delegate.(StreamingProvider); ok {
		return &secretMaskStreamingProvider{secretMaskProvider: base}
	}
	return base
}

func (p *secretMaskProvider) Chat(
	ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]any,
) (*LLMResponse, error) {
	m := newSecretMasker()
	resp, err := p.delegate.Chat(ctx, m.maskMessages(messages), tools, model, options)
	return m.unmaskResponse(resp), err
}

func (p *secretMaskProvider) GetDefaultModel() string {
	return p.delegate.GetDefaultModel()
}

func (p *secretMaskStreamingProvider) ChatStream(
	ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]any,
	onChunk func(accumulated string),
) (*LLMResponse, error) {
	m := newSecretMasker()
	streaming := p.delegate.(StreamingProvider)
	resp, err := streaming.ChatStream(ctx, m.maskMessages(messages), tools, model, options, func(accumulated string) {
		if onChunk != nil {
			onChunk(m.unmask(accumulated))
		}
	})
	return m.unmaskResponse(resp), err
}

func (p *secretMaskStreamingProvider) ChatStreamEvents(
	ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]any,
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
	m := newSecretMasker()
	resp, err := streaming.ChatStreamEvents(ctx, m.maskMessages(messages), tools, model, options, func(c StreamChunk) {
		if onChunk != nil {
			onChunk(StreamChunk{Content: m.unmask(c.Content), ReasoningContent: m.unmask(c.ReasoningContent)})
		}
	})
	return m.unmaskResponse(resp), err
}

func (p *secretMaskProvider) SupportsThinking() bool {
	tc, ok := p.delegate.(ThinkingCapable)
	return ok && tc.SupportsThinking()
}

func (p *secretMaskProvider) SupportsNativeSearch() bool {
	ns, ok := p.delegate.(NativeSearchCapable)
	return ok && ns.SupportsNativeSearch()
}

func (p *secretMaskProvider) Close() {
	if stateful, ok := p.delegate.(StatefulProvider); ok {
		stateful.Close()
	}
}
