package onboard

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/hwprofile"
	"github.com/andre25costa-code/kuromatsu/pkg/setup"
)

// errWizardQuit means the user typed q (or the input ended): nothing is
// written.
var errWizardQuit = errors.New("onboard wizard aborted")

// wizardIO is everything the wizard reads and writes. One buffered reader
// serves every prompt; readSecret reads a hidden line (the terminal in
// production, the same reader in tests).
type wizardIO struct {
	in         *bufio.Reader
	out        io.Writer
	readSecret func() (string, error)
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

// stepTimezone offers the current timezone, else $TZ or the detected system
// zone. When none is known, Enter leaves "timezone" empty, which means the
// system zone: the wizard never writes a zone the user did not type or
// confirm from a real detection.
func (w *wizard) stepTimezone() error {
	def := w.cfg.Timezone
	if def == "" {
		def = defaultTimezone()
	}
	w.println("")
	for {
		label := w.msg.timezonePrompt
		if def == "" {
			label = w.msg.timezonePromptSystem
		}
		tz, err := w.ask(label, def)
		if err != nil {
			return err
		}
		if tz == "" {
			w.sumTimezone = w.msg.sumTimezoneSystem
			return nil
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
	if _, err := w.cfg.WebUI.EnsureToken(); err != nil {
		return err
	}
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
