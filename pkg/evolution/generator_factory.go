package evolution

import "github.com/andre25costa-code/kuromatsu/pkg/providers"

func NewDraftGeneratorForWorkspace(workspace string, provider providers.LLMProvider, modelID string) DraftGenerator {
	fallback := NewDefaultDraftGenerator(workspace)
	if provider == nil {
		return fallback
	}
	return NewLLMDraftGenerator(provider, modelID, fallback)
}
