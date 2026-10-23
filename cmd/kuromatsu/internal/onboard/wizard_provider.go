package onboard

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/andre25costa-code/kuromatsu/cmd/kuromatsu/internal/model"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/providers"
	"github.com/andre25costa-code/kuromatsu/pkg/setup"
)

// --- step 2: model provider ---------------------------------------------

// probeModels is a seam over setup.ProbeModels for tests.
var probeModels = setup.ProbeModels

// wizardAlias is the model_list alias the wizard writes on a config without
// a usable default model.
const wizardAlias = "default"

// providerKind describes one provider option of step 2.
type providerKind struct {
	kind     string // setup.Kind*
	provider string // model_list provider written
	base     string // default API base ("" = ask with no default)
}

var providerKinds = []providerKind{
	{kind: setup.KindOpenAI},
	{kind: setup.KindOllama, provider: "ollama", base: "http://localhost:11434/v1"},
	{kind: setup.KindAnthropic, provider: "anthropic", base: "https://api.anthropic.com/v1"},
	{kind: setup.KindGemini, provider: "gemini", base: "https://generativelanguage.googleapis.com/v1beta"},
	{kind: setup.KindOpenRouter, provider: "openrouter", base: "https://openrouter.ai/api/v1"},
	{kind: setup.Kind9Router, provider: "9router", base: providers.DefaultAPIBaseForProtocol("9router")},
}

// currentModel returns the default model entry when it is a remote API
// entry the wizard may rewrite (not the native in-process model).
func currentModel(cfg *config.Config) *config.ModelConfig {
	name := cfg.Agents.Defaults.GetModelName()
	if name == "" {
		return nil
	}
	for _, m := range cfg.ModelList {
		if m != nil && m.ModelName == name && m.APIBase != "" && m.Provider != "native" {
			return m
		}
	}
	return nil
}

func kindOf(m *config.ModelConfig) string {
	switch m.Provider {
	case "ollama", "anthropic", "gemini", "openrouter", "9router":
		return m.Provider
	}
	return setup.KindOpenAI
}

func (w *wizard) stepProvider() error {
	cur := currentModel(w.cfg)
	keep, def := "", 1
	switch {
	case cur != nil:
		keep, def = fmt.Sprintf("%s: %s", cur.ModelName, cur.Model), 0
	case w.existing:
		// A rerun without a remote default model (e.g. only the native
		// one): Enter skips, so the model keys stay as they are.
		def = len(w.msg.providerOptions)
	}
	n, err := w.choose(w.msg.providerTitle, keep, w.msg.providerOptions, def)
	if err != nil {
		return err
	}
	if n == 0 || n == len(w.msg.providerOptions) {
		w.sumModel = w.msg.sumModelKeep
		return nil
	}
	pk := providerKinds[n-1]

	alias := wizardAlias
	var oldKeys config.SecureStrings
	baseDef := pk.base
	provider := pk.provider
	if cur != nil {
		alias = cur.ModelName
		if kindOf(cur) == pk.kind {
			baseDef = cur.APIBase
			oldKeys = cur.APIKeys
			provider = cur.Provider // same kind: keep e.g. an explicit "openai"
		}
	}

	var base, key, modelID string
	keepKey := false
	edit := true
	for {
		if edit {
			if base, key, keepKey, err = w.askEndpoint(pk, baseDef, oldKeys); err != nil {
				return err
			}
			baseDef = base
			edit = false
		}
		w.printf(w.msg.probing+"\n", base)
		entries, probeErr := probeModels(context.Background(), pk.kind, base, key)
		if probeErr == nil && len(entries) == 0 {
			probeErr = errors.New(w.msg.probeEmpty)
		}
		if probeErr == nil {
			w.printf(w.msg.probeOK+"\n", len(entries))
			modelID, err = model.PickModel(w.io.in, w.io.out, entries, "q")
			if errors.Is(err, model.ErrPickQuit) || errors.Is(err, model.ErrNoSelection) {
				return errWizardQuit
			}
			if err != nil {
				return err
			}
			break
		}
		w.printf(w.msg.probeFailed+"\n", maskSecret(probeErr.Error(), key))
		action, err := w.probeAction()
		if err != nil {
			return err
		}
		switch action {
		case "r":
			continue
		case "e":
			edit = true
			continue
		case "p":
			w.sumModel = w.msg.sumModelKeep
			return nil
		}
		// "s": save anyway with a model id typed by hand.
		for modelID == "" {
			if modelID, err = w.ask(w.msg.manualModel, ""); err != nil {
				return err
			}
		}
		break
	}

	choice := setup.ModelChoice{Alias: alias, Provider: provider, Model: modelID, APIBase: base}
	if !keepKey {
		choice.APIKey = key
	}
	setup.ApplyModelChoice(w.cfg, choice)
	keyShown := "-"
	if keepKey {
		for _, m := range w.cfg.ModelList {
			if m != nil && m.ModelName == alias {
				m.APIKeys = oldKeys
			}
		}
		keyShown = maskKey(key)
	} else if key != "" {
		keyShown = maskKey(key)
	}
	providerShown := provider
	if providerShown == "" {
		providerShown = w.msg.providerOpenAIC
	}
	w.sumModel = fmt.Sprintf(w.msg.sumModel, alias, providerShown, modelID, base, keyShown)
	return nil
}

// askEndpoint asks the API base and, except for Ollama, the key. An empty
// key keeps oldKeys when there are some (keep is then true and key holds
// the current key, for the probe only). For 9router the key is optional:
// with no current key, Enter means no key.
func (w *wizard) askEndpoint(
	pk providerKind, baseDef string, oldKeys config.SecureStrings,
) (base, key string, keep bool, err error) {
	for base == "" {
		if base, err = w.ask(w.msg.baseURL, baseDef); err != nil {
			return "", "", false, err
		}
		if base == "" {
			w.println(w.msg.baseRequired)
		}
	}
	if pk.kind == setup.KindOllama {
		return base, "", false, nil
	}
	current := ""
	if len(oldKeys) > 0 {
		current = oldKeys[0].String()
	}
	for {
		optional := pk.kind == setup.Kind9Router && current == ""
		prompt := w.msg.apiKey
		switch {
		case current != "":
			prompt = w.msg.apiKeyKeep
		case optional:
			prompt = w.msg.apiKeyOptional
		}
		if key, err = w.secret(prompt); err != nil {
			return "", "", false, err
		}
		if key != "" {
			return base, key, false, nil
		}
		if current != "" {
			return base, current, true, nil
		}
		if optional {
			return base, "", false, nil
		}
		w.println(w.msg.apiKeyRequired)
	}
}

func (w *wizard) probeAction() (string, error) {
	for {
		w.println(w.msg.probeMenu)
		text, err := w.ask(">", "r")
		if err != nil {
			return "", err
		}
		switch strings.ToLower(text) {
		case "r", "e", "s", "p":
			return strings.ToLower(text), nil
		}
		w.println(w.msg.invalidChoice)
	}
}
