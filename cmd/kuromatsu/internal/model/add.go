package model

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/andre25costa-code/kuromatsu/cmd/kuromatsu/internal"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/setup"
)

const defaultAliasName = "custom-prefer"

func newAddCommand() *cobra.Command {
	var (
		apiBase   string
		apiKey    string
		modelID   string
		alias     string
		modelType string
	)

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a model from an OpenAI-compatible endpoint",
		Long: `Add a model entry by querying an OpenAI-compatible endpoint exposing
GET <api-base>/models, then setting it as the default model.

If --model is omitted, the available models are listed and you can pick one
interactively. If --model is provided, the entry is written without contacting
the server.

Sample interactive session (key shown masked):

    $ kuromatsu model add \
        -b https://ark.cn-beijing.volces.com/api/v3 \
        -k 7dff****-****-****-****-********e829

    115 model(s) available:
        1) doubao-lite-128k-240428    (doubao-lite-128k)
        2) doubao-pro-128k-240515     (doubao-pro-128k)
        ...
       48) deepseek-r1-250120          (deepseek-r1)
       78) kimi-k2-250711              (kimi-k2)
        ...
      115) doubao-seed3d-2-0-260328    (doubao-seed3d-2-0)
    Pick a model (number or id): 48
    ✓ Saved model 'custom-prefer' (deepseek-r1-250120) and set as default.`,
		Example: `  kuromatsu model add --api-base https://api.openai.com/v1 --api-key sk-...
  kuromatsu model add -b http://localhost:8000/v1 -k dummy -m my-model -n local`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAdd(addOptions{
				apiBase:   strings.TrimSpace(apiBase),
				apiKey:    strings.TrimSpace(apiKey),
				modelID:   strings.TrimSpace(modelID),
				alias:     strings.TrimSpace(alias),
				modelType: strings.TrimSpace(modelType),
				stdin:     cmd.InOrStdin(),
				stdout:    cmd.OutOrStdout(),
			})
		},
	}

	cmd.Flags().StringVarP(&apiBase, "api-base", "b", "",
		"API base URL (required), e.g. https://api.openai.com/v1")
	cmd.Flags().StringVarP(&apiKey, "api-key", "k", "", "API key (required)")
	cmd.Flags().StringVarP(&modelID, "model", "m", "",
		"Model id; when set, skips the interactive picker and the network call")
	cmd.Flags().StringVarP(&alias, "name", "n", defaultAliasName,
		"Local alias written to model_list and used as the default model name")
	cmd.Flags().StringVar(&modelType, "type", "openai-compatible",
		"Endpoint type (only 'openai-compatible' is supported today)")
	_ = cmd.MarkFlagRequired("api-base")
	_ = cmd.MarkFlagRequired("api-key")

	return cmd
}

type addOptions struct {
	apiBase   string
	apiKey    string
	modelID   string
	alias     string
	modelType string
	stdin     io.Reader
	stdout    io.Writer
}

func runAdd(opt addOptions) error {
	if opt.modelType != "" && opt.modelType != "openai-compatible" {
		return fmt.Errorf("unsupported --type %q (only 'openai-compatible' is supported)", opt.modelType)
	}
	if opt.alias == "" {
		opt.alias = defaultAliasName
	}

	selected := opt.modelID
	if selected == "" {
		entries, err := fetchOpenAIModels(opt.apiBase, opt.apiKey)
		if err != nil {
			return fmt.Errorf("fetch models: %w", err)
		}
		if len(entries) == 0 {
			return fmt.Errorf("no models returned by %s", opt.apiBase)
		}
		selected, err = pickModel(opt.stdin, opt.stdout, entries)
		if err != nil {
			return err
		}
	}

	return upsertModelDefault(opt.apiBase, opt.apiKey, opt.alias, selected, opt.stdout)
}

func pickModel(stdin io.Reader, stdout io.Writer, entries []setup.ModelEntry) (string, error) {
	in, ok := stdin.(*bufio.Reader)
	if !ok {
		in = bufio.NewReader(stdin)
	}
	return PickModel(in, stdout, entries, "")
}

// ErrPickQuit is returned by PickModel when the user types its quit word.
var ErrPickQuit = errors.New("model selection aborted")

// ErrNoSelection is returned by PickModel when the input ends before a
// choice.
var ErrNoSelection = errors.New("no selection provided")

// PickModel lists entries and reads a choice (number or id) from in, one
// line at a time, so a caller that shares in with other prompts loses no
// buffered input. When quitWord is not empty and no model has that id,
// typing it returns ErrPickQuit.
func PickModel(in *bufio.Reader, stdout io.Writer, entries []setup.ModelEntry, quitWord string) (string, error) {
	fmt.Fprintf(stdout, "\n%d model(s) available:\n", len(entries))
	for i, m := range entries {
		line := m.ID
		if m.Name != "" && m.Name != m.ID {
			line = fmt.Sprintf("%s (%s)", m.ID, m.Name)
		}
		fmt.Fprintf(stdout, "  %3d) %s\n", i+1, line)
	}

	for {
		fmt.Fprint(stdout, "Pick a model (number or id): ")
		raw, err := in.ReadString('\n')
		if err != nil && (!errors.Is(err, io.EOF) || raw == "") {
			if errors.Is(err, io.EOF) {
				return "", ErrNoSelection
			}
			return "", fmt.Errorf("read input: %w", err)
		}
		text := strings.TrimSpace(raw)
		if text == "" {
			continue
		}
		if idx, convErr := strconv.Atoi(text); convErr == nil {
			if idx < 1 || idx > len(entries) {
				fmt.Fprintf(stdout, "Out of range. Enter 1-%d.\n", len(entries))
				continue
			}
			return entries[idx-1].ID, nil
		}
		for _, m := range entries {
			if m.ID == text {
				return m.ID, nil
			}
		}
		if quitWord != "" && strings.EqualFold(text, quitWord) {
			return "", ErrPickQuit
		}
		fmt.Fprintln(stdout, "Not a valid number or model id; try again.")
	}
}

func upsertModelDefault(apiBase, apiKey, alias, modelID string, stdout io.Writer) error {
	configPath := internal.GetConfigPath()
	cfg, err := config.LoadConfigForEdit(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// `model add` never sets the provider: an existing entry keeps its own,
	// a new one has none (resolution infers it from the model).
	provider := ""
	for _, m := range cfg.ModelList {
		if m != nil && m.ModelName == alias {
			provider = m.Provider
			break
		}
	}
	setup.ApplyModelChoice(cfg, setup.ModelChoice{
		Alias:    alias,
		Provider: provider,
		Model:    modelID,
		APIBase:  apiBase,
		APIKey:   apiKey,
	})

	if err := config.SaveConfig(configPath, cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Fprintf(stdout, "✓ Saved model '%s' (%s) and set as default.\n", alias, modelID)
	return nil
}
