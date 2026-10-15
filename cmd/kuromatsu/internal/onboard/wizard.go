package onboard

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/andre25costa-code/kuromatsu/cmd/kuromatsu/internal/model"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/hwprofile"
	"github.com/andre25costa-code/kuromatsu/pkg/logger"
	"github.com/andre25costa-code/kuromatsu/pkg/setup"
)

// Seams for tests: the provider probe, the Telegram API base and the
// system timezone name.
var (
	probeModels     = setup.ProbeModels
	telegramAPIBase = "https://api.telegram.org"
	systemZone      = func() string { return time.Now().Location().String() }
)

// wizardAlias is the model_list alias the wizard writes on a config without
// a usable default model.
const wizardAlias = "default"

// getMeTimeout bounds the optional Telegram bot check.
const getMeTimeout = 10 * time.Second

// errWizardQuit means the user typed q (or the input ended): nothing is
// written.
var errWizardQuit = errors.New("onboard wizard aborted")

var (
	telegramTokenRe = regexp.MustCompile(`^\d+:[A-Za-z0-9_-]{30,}$`)
	digitsRe        = regexp.MustCompile(`^\d+$`)
)

// wizardIO is everything the wizard reads and writes. One buffered reader
// serves every prompt; readSecret reads a hidden line (the terminal in
// production, the same reader in tests).
type wizardIO struct {
	in         *bufio.Reader
	out        io.Writer
	readSecret func() (string, error)
}

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
}

// wizard holds the config being edited and a summary of each step.
type wizard struct {
	io         wizardIO
	msg        *messages
	lang       string
	configPath string
	cfg        *config.Config
	existing   bool
	profile    hwprofile.Name // from --profile: step 4's default
	profileSet bool

	sumModel, sumChannel, sumProfile, sumTimezone, sumPerm string
	hasChannel                                             bool
}

// runWizard runs the six onboard steps on the config at configPath (or a
// new default config), shows a summary and, once confirmed, saves the
// config, creates the workspace templates and records the language in
// USER.md. It returns errWizardQuit, with nothing written, when the user
// quits or declines.
func runWizard(wio wizardIO, configPath string, force bool, profile hwprofile.Name, profileSet bool) error {
	w := &wizard{
		io:         wio,
		msg:        messageTable[langPtBR],
		lang:       langPtBR,
		configPath: configPath,
		profile:    profile,
		profileSet: profileSet,
	}

	if _, err := os.Stat(configPath); err == nil {
		w.existing = true
	}
	var err error
	if w.existing {
		if w.cfg, err = config.LoadConfigForEdit(configPath); err != nil {
			return fmt.Errorf("load existing config: %w", err)
		}
	} else {
		w.cfg = config.DefaultConfig()
	}

	steps := []func() error{
		w.stepLanguage, w.stepProvider, w.stepChannel, w.stepHardware, w.stepTimezone, w.stepPermissions,
	}
	for _, step := range steps {
		if err = step(); err != nil {
			if errors.Is(err, errWizardQuit) {
				w.println(w.msg.quitHint)
			}
			return err
		}
	}

	if ok, err := w.confirm(); err != nil || !ok {
		if err == nil {
			w.println(w.msg.aborted)
			return errWizardQuit
		}
		return err
	}
	return w.save(force)
}

func (w *wizard) printf(format string, args ...any) { fmt.Fprintf(w.io.out, format, args...) }
func (w *wizard) println(s string)                  { fmt.Fprintln(w.io.out, s) }

// readLine reads one line; q quits; the end of input quits too.
func (w *wizard) readLine() (string, error) {
	raw, err := w.io.in.ReadString('\n')
	if err != nil && (!errors.Is(err, io.EOF) || raw == "") {
		if errors.Is(err, io.EOF) {
			return "", errWizardQuit
		}
		return "", fmt.Errorf("read input: %w", err)
	}
	text := strings.TrimSpace(raw)
	if strings.EqualFold(text, "q") {
		return "", errWizardQuit
	}
	return text, nil
}

// ask prints "label [def]: " and returns the answer, or def on Enter.
func (w *wizard) ask(label, def string) (string, error) {
	if def != "" {
		w.printf("%s [%s]: ", label, def)
	} else {
		w.printf("%s: ", label)
	}
	text, err := w.readLine()
	if err != nil {
		return "", err
	}
	if text == "" {
		return def, nil
	}
	return text, nil
}

// choose prints the numbered options (keep, when not empty, as option 0)
// and returns the chosen number, def on Enter. It repeats until valid.
func (w *wizard) choose(title, keep string, options []string, def int) (int, error) {
	w.printf("\n%s\n", title)
	if keep != "" {
		w.printf("  [0] %s\n", fmt.Sprintf(w.msg.keepCurrent, keep))
	}
	for i, o := range options {
		w.printf("  [%d] %s\n", i+1, o)
	}
	for {
		text, err := w.ask(">", strconv.Itoa(def))
		if err != nil {
			return 0, err
		}
		n, convErr := strconv.Atoi(text)
		lowest := 1
		if keep != "" {
			lowest = 0
		}
		if convErr == nil && n >= lowest && n <= len(options) {
			return n, nil
		}
		w.println(w.msg.invalidChoice)
	}
}

// secret reads a hidden value. If hiding fails (e.g. a terminal that does
// not support it), it says so and reads a visible line instead.
func (w *wizard) secret(prompt string) (string, error) {
	w.printf("%s", prompt)
	s, err := w.io.readSecret()
	if err != nil {
		w.printf("\n%s", w.msg.secretVisible)
		return w.readLine()
	}
	s = strings.TrimSpace(s)
	if strings.EqualFold(s, "q") {
		return "", errWizardQuit
	}
	return s, nil
}

// --- step 1: language ---------------------------------------------------

func (w *wizard) stepLanguage() error {
	w.println("Idioma / Language")
	w.println("  [1] Português (Brasil)")
	w.println("  [2] English")
	for {
		text, err := w.ask(">", "1")
		if err != nil {
			return err
		}
		switch text {
		case "1":
			w.lang = langPtBR
		case "2":
			w.lang = langEn
		default:
			w.println("Opção inválida / Invalid choice.")
			continue
		}
		w.msg = messageTable[w.lang]
		w.println(w.msg.intro)
		return nil
	}
}

// --- step 2: model provider ---------------------------------------------

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
	case "ollama", "anthropic", "gemini", "openrouter":
		return m.Provider
	}
	return setup.KindOpenAI
}

func (w *wizard) stepProvider() error {
	cur := currentModel(w.cfg)
	keep, def := "", 1
	if cur != nil {
		keep, def = fmt.Sprintf("%s: %s", cur.ModelName, cur.Model), 0
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
	if cur != nil {
		alias = cur.ModelName
		if kindOf(cur) == pk.kind {
			baseDef = cur.APIBase
			oldKeys = cur.APIKeys
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
			if errors.Is(err, model.ErrPickQuit) {
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

	choice := setup.ModelChoice{Alias: alias, Provider: pk.provider, Model: modelID, APIBase: base}
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
	provider := pk.provider
	if provider == "" {
		provider = w.msg.providerOpenAIC
	}
	w.sumModel = fmt.Sprintf(w.msg.sumModel, alias, provider, modelID, base, keyShown)
	return nil
}

// askEndpoint asks the API base and, except for Ollama, the key. An empty
// key keeps oldKeys when there are some (keep is then true and key holds
// the current key, for the probe only).
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
		prompt := w.msg.apiKey
		if current != "" {
			prompt = w.msg.apiKeyKeep
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

// --- step 3: channel ----------------------------------------------------

func (w *wizard) stepChannel() error {
	tg := w.cfg.Channels[config.ChannelTelegram]
	configured := tg != nil && tg.Enabled && len(tg.AllowFrom) > 0
	keep, def := "", 2
	if configured {
		keep, def = "Telegram, allow_from "+strings.Join(tg.AllowFrom, ","), 0
	}
	n, err := w.choose(w.msg.channelTitle, keep, w.msg.channelOptions, def)
	if err != nil {
		return err
	}
	switch n {
	case 0:
		w.sumChannel, w.hasChannel = w.msg.sumChannelKeep, true
		return nil
	case 2:
		w.sumChannel = w.msg.sumChannelNone
		return nil
	}

	if tg == nil {
		tg = &config.Channel{Type: config.ChannelTelegram}
		tg.SetName(config.ChannelTelegram)
		w.cfg.Channels[config.ChannelTelegram] = tg
	}
	if tg.SettingsIsEmpty() {
		tg.Settings = config.RawNode("{}")
	}
	tg.Type = config.ChannelTelegram
	decoded, err := tg.GetDecoded()
	if err != nil {
		return err
	}
	settings, ok := decoded.(*config.TelegramSettings)
	if !ok {
		return fmt.Errorf("telegram channel settings have an unexpected type %T", decoded)
	}

	current := settings.Token.String()
	var token string
	for token == "" {
		prompt := w.msg.tgToken
		if current != "" {
			prompt = w.msg.tgTokenKeep
		}
		if token, err = w.secret(prompt); err != nil {
			return err
		}
		if token == "" && current != "" {
			token = current
			break
		}
		if !telegramTokenRe.MatchString(token) {
			w.println(w.msg.tgTokenInvalid)
			token = ""
		}
	}

	ownerDef := ""
	if len(tg.AllowFrom) > 0 {
		ownerDef = strings.Join(tg.AllowFrom, ",")
	}
	var owners []string
	for owners == nil {
		text, err := w.ask(w.msg.tgOwner, ownerDef)
		if err != nil {
			return err
		}
		if text != "" && text == ownerDef {
			owners = append([]string(nil), tg.AllowFrom...)
			break
		}
		if !digitsRe.MatchString(text) {
			w.println(w.msg.tgOwnerInvalid)
			continue
		}
		owners = []string{text}
	}

	w.checkTelegram(token)

	tg.Enabled = true
	tg.AllowFrom = config.FlexibleStringSlice(owners)
	if token != current {
		settings.Token.Set(token)
	}
	w.sumChannel = fmt.Sprintf(w.msg.sumTelegram, strings.Join(owners, ","), maskKey(token))
	w.hasChannel = true
	return nil
}

// checkTelegram calls getMe and only warns on failure: the network may
// block the API. Errors are printed without the token (it is in the URL).
func (w *wizard) checkTelegram(token string) {
	ctx, cancel := context.WithTimeout(context.Background(), getMeTimeout)
	defer cancel()
	username, err := telegramGetMe(ctx, token)
	if err != nil {
		w.printf(w.msg.getMeFailed+"\n", maskSecret(err.Error(), token))
		return
	}
	w.printf(w.msg.getMeOK+"\n", username)
}

func telegramGetMe(ctx context.Context, token string) (string, error) {
	url := strings.TrimRight(telegramAPIBase, "/") + "/bot" + token + "/getMe"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var body struct {
		OK     bool `json:"ok"`
		Result struct {
			Username string `json:"username"`
		} `json:"result"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body); err != nil {
		return "", fmt.Errorf("HTTP %d: %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK || !body.OK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, body.Description)
	}
	return body.Result.Username, nil
}

// maskSecret removes secret from s, then masks anything else that looks
// like a credential.
func maskSecret(s, secret string) string {
	if secret != "" {
		s = strings.ReplaceAll(s, secret, "****")
	}
	return logger.MaskSecrets(s)
}

// maskKey shows a key as its first 3 and last 4 characters (sk-****abcd).
func maskKey(k string) string {
	if len(k) <= 8 {
		return "****"
	}
	return k[:3] + "****" + k[len(k)-4:]
}

// --- step 4: hardware profile -------------------------------------------

func (w *wizard) stepHardware() error {
	hw := detect()
	detected := hwprofile.Select(hw)
	w.printf("\n"+w.msg.hardwareLine+"\n", hwprofile.FormatGiB(hw.Budget()), hw.CPUs, detected)
	if hw.Budget() == 0 {
		w.println(hwprofile.UnknownRAMNote)
	}

	suggested := detected
	if w.profileSet && w.profile != "" {
		suggested = w.profile
	}
	keep := ""
	if w.existing && !w.profileSet {
		keep = string(hwprofile.Match(w.cfg, hw))
	}
	options := make([]string, len(hwprofile.All))
	def := 0
	for i, n := range hwprofile.All {
		options[i] = string(n)
		if n == suggested {
			def = i + 1
			options[i] += " (" + fmt.Sprintf(w.msg.hardwareUse, n) + ")"
		}
	}
	if keep != "" {
		def = 0
	}
	n, err := w.choose(w.msg.hardwareTitle, keep, options, def)
	if err != nil {
		return err
	}
	if n == 0 {
		w.sumProfile = w.msg.sumProfileKeep
		return nil
	}
	name := hwprofile.All[n-1]
	for _, warn := range hwprofile.Apply(w.cfg, name, hw) {
		w.printf(w.msg.warning+"\n", warn)
	}
	w.sumProfile = fmt.Sprintf(w.msg.sumProfile, name)
	return nil
}

// --- step 5: timezone ---------------------------------------------------

// defaultTimezone is $TZ when valid, else the system zone when it has a
// name, else America/Sao_Paulo (pt-BR) or UTC (en).
func defaultTimezone(lang string) string {
	if tz := os.Getenv("TZ"); tz != "" {
		if _, err := time.LoadLocation(tz); err == nil {
			return tz
		}
	}
	if sys := systemZone(); sys != "" && sys != "Local" {
		return sys
	}
	if lang == langPtBR {
		return "America/Sao_Paulo"
	}
	return "UTC"
}

func (w *wizard) stepTimezone() error {
	def := w.cfg.Timezone
	if def == "" {
		def = defaultTimezone(w.lang)
	}
	w.println("")
	for {
		tz, err := w.ask(w.msg.timezonePrompt, def)
		if err != nil {
			return err
		}
		if _, loadErr := time.LoadLocation(tz); loadErr != nil {
			w.printf(w.msg.timezoneInvalid+"\n", loadErr)
			continue
		}
		w.cfg.Timezone = tz
		w.sumTimezone = fmt.Sprintf(w.msg.sumTimezone, tz)
		return nil
	}
}

// --- step 6: permissions ------------------------------------------------

func (w *wizard) stepPermissions() error {
	keep, def := "", 2
	if w.existing {
		keep, def = string(setup.MatchPermissions(w.cfg)), 0
	}
	n, err := w.choose(w.msg.permTitle, keep, w.msg.permOptions, def)
	if err != nil {
		return err
	}
	if n == 0 {
		w.sumPerm = fmt.Sprintf(w.msg.sumPermKeep, keep)
		return nil
	}
	preset := setup.Presets[n-1]
	if err := setup.ApplyPermissions(w.cfg, preset); err != nil {
		return err
	}
	w.sumPerm = fmt.Sprintf(w.msg.sumPermissions, preset)
	return nil
}

// --- summary and save ---------------------------------------------------

func (w *wizard) confirm() (bool, error) {
	w.printf("\n%s\n", w.msg.summaryTitle)
	for _, line := range []string{
		w.sumModel, w.sumChannel, w.sumProfile, w.sumTimezone, w.sumPerm,
		fmt.Sprintf(w.msg.sumLanguage, w.lang),
	} {
		w.println(line)
	}
	for {
		w.printf("%s", w.msg.confirm)
		text, err := w.readLine()
		if err != nil {
			return false, err
		}
		switch strings.ToLower(text) {
		case "", "s", "sim", "y", "yes":
			return true, nil
		case "n", "nao", "não", "no":
			return false, nil
		}
		w.println(w.msg.invalidChoice)
	}
}

func (w *wizard) save(force bool) error {
	if err := config.SaveConfig(w.configPath, w.cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	// The workspace comes from the runtime view, so an environment override
	// of the workspace applies here without being written to the file.
	rt, err := config.LoadConfig(w.configPath)
	if err != nil {
		return fmt.Errorf("load saved config: %w", err)
	}
	workspace := rt.WorkspacePath()
	createWorkspaceTemplates(workspace, force)
	if err := setUserLanguage(filepath.Join(workspace, "USER.md"), w.msg.userLanguageLine); err != nil {
		w.printf(w.msg.warning+"\n", err)
	}

	w.printf("\n"+w.msg.saved+"\n", w.configPath)
	next := "kuromatsu agent"
	if w.hasChannel {
		next = "kuromatsu gateway"
	}
	w.printf(w.msg.nextCommand+"\n", next)
	return nil
}

// languageLinePrefixes start the language line in either language.
var languageLinePrefixes = []string{"- Idioma preferido:", "- Preferred language:"}

// setUserLanguage records the language line in USER.md: nothing when the
// line is already there, a replacement of the other language's line, or an
// appended line.
func setUserLanguage(path, line string) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	lines := strings.Split(string(data), "\n")
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == line {
			return nil
		}
		for _, p := range languageLinePrefixes {
			if strings.HasPrefix(trimmed, p) {
				lines[i] = line
				return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
			}
		}
	}
	content := string(data)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += line + "\n"
	return os.WriteFile(path, []byte(content), 0o644)
}
