package agent

import (
	"context"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/bus"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

func TestShowModelUsesResolvedNativeProvider(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = t.TempDir()
	cfg.Agents.Defaults.Provider = "openai"
	cfg.Agents.Defaults.ModelName = "bonsai-local"
	cfg.ModelList = config.SecureModelList{{ModelName: "bonsai-local", Provider: "native", Model: "Bonsai-1.7B-Q1_0"}}
	al := NewAgentLoop(cfg, bus.NewMessageBus(), &turnProfileCaptureProvider{})
	instance := al.registry.GetDefaultAgent()
	rt := al.buildCommandsRuntime(context.Background(), instance, &processOptions{})
	name, provider := rt.GetModelInfo()
	if name != "Bonsai-1.7B-Q1_0" || provider != "native" {
		t.Fatalf("model=%s provider=%s", name, provider)
	}
}
