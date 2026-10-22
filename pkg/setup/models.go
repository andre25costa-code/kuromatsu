// Package setup holds the configuration steps shared by the interactive
// onboard, the `model add` command and the web UI: writing a model choice,
// probing a provider's model list and applying permission presets.
package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

// Probe kinds accepted by ProbeModels. Each one selects the auth header and
// the response shape of GET <base>/models.
const (
	KindOpenAI     = "openai"     // any OpenAI-compatible endpoint: Bearer
	KindOllama     = "ollama"     // local Ollama: no auth
	KindAnthropic  = "anthropic"  // x-api-key + anthropic-version
	KindGemini     = "gemini"     // x-goog-api-key, {"models":[{"name":"models/..."}]}
	KindOpenRouter = "openrouter" // Bearer
	Kind9Router    = "9router"    // local 9router: Bearer only when a key is set
)

// AnthropicVersion is the API version header sent to Anthropic.
const AnthropicVersion = "2023-06-01"

// probeTimeout bounds one model-list request.
const probeTimeout = 15 * time.Second

// errorBodyLimit is how much of a failed response body an error shows.
const errorBodyLimit = 512

// ModelEntry is one model returned by a provider's model list.
type ModelEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ModelChoice is a model entry to write to model_list and make the default.
// An empty APIKey removes the entry's keys (e.g. Ollama needs none).
type ModelChoice struct {
	Alias    string
	Provider string
	Model    string
	APIBase  string
	APIKey   string
}

// ApplyModelChoice upserts the model_list entry named c.Alias with the
// choice's provider, model, base and key, enables it and makes it the
// default model. It only changes cfg; the caller saves.
func ApplyModelChoice(cfg *config.Config, c ModelChoice) {
	var keys config.SecureStrings
	if strings.TrimSpace(c.APIKey) != "" {
		keys = config.SimpleSecureStrings(c.APIKey)
	}

	var entry *config.ModelConfig
	for _, m := range cfg.ModelList {
		if m != nil && m.ModelName == c.Alias {
			entry = m
			break
		}
	}
	if entry == nil {
		entry = &config.ModelConfig{ModelName: c.Alias}
		cfg.ModelList = append(cfg.ModelList, entry)
	}
	entry.Provider = c.Provider
	entry.Model = c.Model
	entry.APIBase = c.APIBase
	entry.APIKeys = keys
	entry.Enabled = true

	cfg.Agents.Defaults.ModelName = c.Alias
}

type modelsDataEnvelope struct {
	Data []ModelEntry `json:"data"`
}

type geminiModels struct {
	Models []struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
		Description string `json:"description"`
	} `json:"models"`
}

// ProbeModels GETs <baseURL>/models with the auth header of kind and returns
// the listed models. It accepts the {"data":[...]} envelope, a bare array and
// Gemini's {"models":[{"name":"models/<id>"}]}. A key is required for every
// kind except Ollama and 9router. A non-200 answer is reported with its
// status and up to 512 bytes of the body.
func ProbeModels(ctx context.Context, kind, baseURL, apiKey string) ([]ModelEntry, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("api base is required")
	}
	switch kind {
	case KindOpenAI, KindOllama, KindAnthropic, KindGemini, KindOpenRouter, Kind9Router:
	default:
		return nil, fmt.Errorf("unknown provider kind %q", kind)
	}
	if kind != KindOllama && kind != Kind9Router && strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("api key is required")
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	url := strings.TrimRight(baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	switch kind {
	case KindOpenAI, KindOpenRouter:
		req.Header.Set("Authorization", "Bearer "+apiKey)
	case Kind9Router:
		// 9router ships with its key check off; send one only when set.
		if strings.TrimSpace(apiKey) != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
	case KindAnthropic:
		req.Header.Set("X-Api-Key", apiKey)
		req.Header.Set("Anthropic-Version", AnthropicVersion)
	case KindGemini:
		req.Header.Set("X-Goog-Api-Key", apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit))
		if readErr != nil {
			return nil, fmt.Errorf("read error response: %w", readErr)
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return parseModels(kind, body)
}

func parseModels(kind string, body []byte) ([]ModelEntry, error) {
	if kind == KindGemini {
		var g geminiModels
		if err := json.Unmarshal(body, &g); err == nil && g.Models != nil {
			out := make([]ModelEntry, 0, len(g.Models))
			for _, m := range g.Models {
				out = append(out, ModelEntry{
					ID:          strings.TrimPrefix(m.Name, "models/"),
					Name:        m.DisplayName,
					Description: m.Description,
				})
			}
			return out, nil
		}
	}

	// {"data": [...]} envelope. json.Unmarshal sets Data to an empty slice
	// for `{"data":[]}` but leaves it nil when "data" is absent or null, which
	// separates "empty list" from "some other object".
	var envelope modelsDataEnvelope
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Data != nil {
		return envelope.Data, nil
	}

	// Bare-array shape, including `[]`.
	var arr []ModelEntry
	if err := json.Unmarshal(body, &arr); err == nil {
		return arr, nil
	}

	preview := body
	if len(preview) > 256 {
		preview = preview[:256]
	}
	return nil, fmt.Errorf("decode response: unrecognized shape: %s", strings.TrimSpace(string(preview)))
}
