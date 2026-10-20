package status

import (
	"fmt"
	"os"
	"strings"

	"github.com/andre25costa-code/kuromatsu/cmd/kuromatsu/internal"
	"github.com/andre25costa-code/kuromatsu/cmd/kuromatsu/internal/cliui"
	"github.com/andre25costa-code/kuromatsu/pkg/auth"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/hwprofile"
	"github.com/andre25costa-code/kuromatsu/pkg/providers"
	"github.com/andre25costa-code/kuromatsu/pkg/providers/localllm"
	"github.com/andre25costa-code/kuromatsu/pkg/sysinfo"
)

// detect is a seam over sysinfo.Detect so tests can simulate hardware.
var detect = sysinfo.Detect

// hardwareLines renders the status "Hardware" section: what was detected,
// the profile it suggests, which profile cfg corresponds to, and whether
// this CPU can run the native build (only meaningful on a native build).
func hardwareLines(cfg *config.Config, hw sysinfo.Hardware, nativeBuilt bool) []string {
	ram := "RAM unknown"
	if hw.TotalRAM > 0 {
		avail := hwprofile.FormatGiB(hw.AvailableRAM) + " available"
		if hw.AvailableRAM == 0 {
			avail = "available unknown"
		}
		ram = fmt.Sprintf("%s RAM (%s)", hwprofile.FormatGiB(hw.TotalRAM), avail)
	}
	limit := "none"
	if hw.MemLimit > 0 {
		limit = hwprofile.FormatGiB(hw.MemLimit)
	}

	detected := string(hwprofile.Select(hw))
	if hw.Budget() == 0 {
		detected += " (" + hwprofile.UnknownRAMNote + ")"
	}

	native := "n/a (pure-Go build)"
	if nativeBuilt {
		if hw.NativeCPUOK {
			native = "✓ " + nativeFeatures(hw.Arch)
		} else {
			native = "✗ missing " + strings.Join(hw.NativeCPUMissing, " ") + " — native model disabled"
		}
	}

	return []string{
		fmt.Sprintf("Hardware   %s, limit %s, %d CPUs, %s", ram, limit, hw.CPUs, hw.Arch),
		fmt.Sprintf("Profile    detected: %s · config matches: %s", detected, hwprofile.Match(cfg, hw)),
		"Native CPU " + native,
	}
}

// nativeFeatures names the extensions the native build relies on for arch.
func nativeFeatures(arch string) string {
	switch arch {
	case "amd64":
		return "AVX2 FMA"
	case "arm64":
		return "dotprod fp16"
	}
	return arch
}

func statusCmd() {
	cfg, err := internal.LoadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	configPath := internal.GetConfigPath()
	build, _ := config.FormatBuildInfo()

	_, configStatErr := os.Stat(configPath)
	configOK := configStatErr == nil

	workspace := cfg.WorkspacePath()
	_, wsErr := os.Stat(workspace)
	wsOK := wsErr == nil

	report := cliui.StatusReport{
		Logo:          internal.Logo,
		Version:       config.FormatVersion(),
		Build:         build,
		ConfigPath:    configPath,
		ConfigOK:      configOK,
		WorkspacePath: workspace,
		WorkspaceOK:   wsOK,
		Model:         cfg.Agents.Defaults.GetModelName(),
		Hardware:      hardwareLines(cfg, detect(), localllm.Built()),
	}

	if configOK {
		// Kuromatsu moved to a model-centric configuration (model_list). Status should
		// not depend on a legacy cfg.Providers field (which may not exist under some
		// build tags). We infer provider availability from model_list entries.
		hasProtocolKey := func(protocol string) bool {
			want := providers.NormalizeProvider(protocol)
			for _, m := range cfg.ModelList {
				if m == nil {
					continue
				}
				got, _ := providers.ExtractProtocol(m)
				if got == want && m.APIKey() != "" {
					return true
				}
			}
			return false
		}
		findLocalModelBase := func(modelName string) (string, bool) {
			for _, m := range cfg.ModelList {
				if m == nil {
					continue
				}
				if m.ModelName == modelName && m.APIBase != "" {
					return m.APIBase, true
				}
			}
			return "", false
		}
		findProtocolBase := func(protocol string) (string, bool) {
			want := providers.NormalizeProvider(protocol)
			for _, m := range cfg.ModelList {
				if m == nil {
					continue
				}
				got, _ := providers.ExtractProtocol(m)
				if got == want && m.APIBase != "" {
					return m.APIBase, true
				}
			}
			return "", false
		}

		hasOpenRouter := hasProtocolKey("openrouter")
		hasAnthropic := hasProtocolKey("anthropic")
		hasOpenAI := hasProtocolKey("openai")
		hasGemini := hasProtocolKey("gemini")
		hasZhipu := hasProtocolKey("zhipu")
		hasQwen := hasProtocolKey("qwen")
		hasGroq := hasProtocolKey("groq")
		hasMoonshot := hasProtocolKey("moonshot")
		hasDeepSeek := hasProtocolKey("deepseek")
		hasVolcEngine := hasProtocolKey("volcengine")
		hasNvidia := hasProtocolKey("nvidia")

		// Local endpoints: allow both the special reserved name and protocol-based entries.
		vllmBase, hasVLLM := findLocalModelBase("local-model")
		if !hasVLLM {
			vllmBase, hasVLLM = findProtocolBase("vllm")
		}
		ollamaBase, hasOllama := findProtocolBase("ollama")

		val := func(enabled bool, extra ...string) string {
			if enabled {
				if len(extra) > 0 && extra[0] != "" {
					return "✓ " + extra[0]
				}
				return "✓"
			}
			return "not set"
		}

		report.Providers = []cliui.ProviderRow{
			{Name: "OpenRouter API", Val: val(hasOpenRouter)},
			{Name: "Anthropic API", Val: val(hasAnthropic)},
			{Name: "OpenAI API", Val: val(hasOpenAI)},
			{Name: "Gemini API", Val: val(hasGemini)},
			{Name: "Zhipu API", Val: val(hasZhipu)},
			{Name: "Qwen API", Val: val(hasQwen)},
			{Name: "Groq API", Val: val(hasGroq)},
			{Name: "Moonshot API", Val: val(hasMoonshot)},
			{Name: "DeepSeek API", Val: val(hasDeepSeek)},
			{Name: "VolcEngine API", Val: val(hasVolcEngine)},
			{Name: "Nvidia API", Val: val(hasNvidia)},
			{Name: "vLLM / local", Val: val(hasVLLM, vllmBase)},
			{Name: "Ollama", Val: val(hasOllama, ollamaBase)},
			{Name: "Native (Bonsai)", Val: nativeStatusVal()},
		}

		store, _ := auth.LoadStore()
		if store != nil && len(store.Credentials) > 0 {
			for provider, cred := range store.Credentials {
				st := "authenticated"
				if cred.IsExpired() {
					st = "expired"
				} else if cred.NeedsRefresh() {
					st = "needs refresh"
				}
				report.OAuthLines = append(report.OAuthLines,
					fmt.Sprintf("%s (%s): %s", provider, cred.AuthMethod, st))
			}
		}
	}

	cliui.PrintStatus(report)
}

// nativeStatusVal reports the state of the in-process native provider
// (FR-004): whether the binary was compiled with the nativellm cgo engine,
// and whether the Bonsai GGUF is present on disk.
func nativeStatusVal() string {
	if !localllm.Built() {
		return "not built (make build-native)"
	}
	path, err := localllm.ResolveModelPath("Bonsai-1.7B-Q1_0", config.GetHome())
	if err != nil {
		return "built, model missing (make model-download)"
	}
	return "✓ " + path
}
