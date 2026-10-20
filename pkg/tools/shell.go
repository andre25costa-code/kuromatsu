package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/creack/pty"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/constants"
	"github.com/andre25costa-code/kuromatsu/pkg/isolation"
	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

var (
	globalSessionManager = NewSessionManager()
	sessionManagerMu     sync.RWMutex
)

func getSessionManager() *SessionManager {
	sessionManagerMu.RLock()
	defer sessionManagerMu.RUnlock()
	return globalSessionManager
}

type ExecTool struct {
	workingDir          string
	timeout             time.Duration
	denyPatterns        []*regexp.Regexp
	allowPatterns       []*regexp.Regexp
	customAllowPatterns []*regexp.Regexp
	allowedPathPatterns []*regexp.Regexp
	restrictToWorkspace bool
	allowRemote         bool
	sessionManager      *SessionManager
}

var (
	defaultDenyPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\brm\s+-[rf]{1,2}\b`),
		regexp.MustCompile(`\bdel\s+/[fq]\b`),
		regexp.MustCompile(`\brmdir\s+/s\b`),
		// Match disk wiping commands (must be followed by space/args), including
		// the mkfs.<fs> helpers such as mkfs.ext4 and mkfs.vfat.
		regexp.MustCompile(
			`(^|[^-\w])\b(format|mkfs(?:\.\w+)?|diskpart)\b\s`,
		),
		regexp.MustCompile(`\bdd\s+if=`),
		// Block writes to block devices (all common naming schemes).
		regexp.MustCompile(
			`>\s*/dev/(sd[a-z]|hd[a-z]|vd[a-z]|xvd[a-z]|nvme\d|mmcblk\d|loop\d|dm-\d|md\d|sr\d|nbd\d)`,
		),
		regexp.MustCompile(`\b(shutdown|reboot|poweroff)\b`),
		regexp.MustCompile(`:\(\)\s*\{.*\};\s*:`),
		regexp.MustCompile(`\$\([^)]+\)`),
		regexp.MustCompile(`\$\{[^}]+\}`),
		regexp.MustCompile("`[^`]+`"),
		regexp.MustCompile(`\|\s*sh\b`),
		regexp.MustCompile(`\|\s*bash\b`),
		regexp.MustCompile(`;\s*rm\s+-[rf]`),
		regexp.MustCompile(`&&\s*rm\s+-[rf]`),
		regexp.MustCompile(`\|\|\s*rm\s+-[rf]`),
		regexp.MustCompile(`<<\s*EOF`),
		regexp.MustCompile(`\$\(\s*cat\s+`),
		regexp.MustCompile(`\$\(\s*curl\s+`),
		regexp.MustCompile(`\$\(\s*wget\s+`),
		regexp.MustCompile(`\$\(\s*which\s+`),
		regexp.MustCompile(`\bsudo\b`),
		regexp.MustCompile(`\bchmod\s+[0-7]{3,4}\b`),
		regexp.MustCompile(`\bchown\b`),
		regexp.MustCompile(`\bpkill\b`),
		regexp.MustCompile(`\bkillall\b`),
		regexp.MustCompile(`\bkill\b`),
		regexp.MustCompile(`\bcurl\b.*\|\s*(sh|bash)`),
		regexp.MustCompile(`\bwget\b.*\|\s*(sh|bash)`),
		regexp.MustCompile(`\bnpm\s+install\s+-g\b`),
		regexp.MustCompile(`\bpip\s+install\s+--user\b`),
		regexp.MustCompile(`\bapt\s+(install|remove|purge)\b`),
		regexp.MustCompile(`\byum\s+(install|remove)\b`),
		regexp.MustCompile(`\bdnf\s+(install|remove)\b`),
		regexp.MustCompile(`\bdocker\s+run\b`),
		regexp.MustCompile(`\bdocker\s+exec\b`),
		regexp.MustCompile(`\bgit\s+push\b`),
		regexp.MustCompile(`\bgit\s+force\b`),
		regexp.MustCompile(`\bssh\b.*@`),
		regexp.MustCompile(`\beval\b`),
		regexp.MustCompile(`\bsource\s+.*\.sh\b`),
	}

	// windowsDenyPatterns contains PowerShell-specific deny patterns that only
	// apply on Windows, where commands are executed via powershell -Command.
	windowsDenyPatterns = []*regexp.Regexp{
		// [Text.Encoding] used to construct command strings at runtime.
		// Matches [Text.Encoding] and [System.Text.Encoding] variants.
		regexp.MustCompile(`\[(?:\w+\.)?text\.encoding\]`),
		// PowerShell -EncodedCommand flag (base64-encoded command). PowerShell
		// accepts any unique prefix of a parameter name, so every prefix from
		// -e up to -encodedcommand is matched, plus the -ec alias.
		encodedCommandFlagPattern(),
		// The full flag followed by anything (e.g. "-encodedcommand:ZQ==").
		regexp.MustCompile(` -encodedcommand\b`),
		// .GetString called on byte array to decode commands.
		regexp.MustCompile(`\.getstring\s*\(\s*\[byte\[\]`),
		// FromBase64String used in command construction chain.
		regexp.MustCompile(`frombase64string\(`),
		// PowerShell variable holding byte array used in GetString.
		regexp.MustCompile(`\$[a-zA-Z_]\w*\s*=\s*\[byte\[\]`),
		// Unicode escape sequences that could be used to construct commands.
		// Matches \uXXXX format used to represent characters like i = "i"
		regexp.MustCompile(`\\u[0-9a-fA-F]{4}`),
	}

	// absolutePathPattern matches absolute file paths in commands (Unix and Windows).
	absolutePathPattern = regexp.MustCompile(`[A-Za-z]:\\[^\\\"']+|/[^\s\"']+`)

	// pathTraversalPattern matches ".." followed by a path separator, including
	// ".../.../" variants.
	pathTraversalPattern = regexp.MustCompile(`\.\.(?:[\\/]\.\.)*[\\/]`)

	// dotDotTokenPattern matches ".." standing alone as a path token ("ls ..",
	// "cd .. && x", "--directory=..", "PATH=..:$PATH", "cat <.."). Git ranges
	// such as "a..b", "..HEAD" and "HEAD~3.." stay unmatched because a letter
	// or digit touches the dots.
	dotDotTokenPattern = regexp.MustCompile(
		`(^|[\s/\\;&|"'=()<>,:` + "`" + `])\.\.($|[\s/\\;&|"'=()<>,:` + "`" + `])`,
	)

	// dotDotShortOptionPattern matches ".." attached to a one-letter option,
	// as in "make -C.. x".
	dotDotShortOptionPattern = regexp.MustCompile(
		`(^|\s)-[A-Za-z]\.\.($|[\s/\\;&|"'=()<>,:` + "`" + `])`,
	)

	// cdDotDotPattern matches "cd.." written without a space, which cmd.exe
	// and PowerShell both accept.
	cdDotDotPattern = regexp.MustCompile(`(?i)(^|[\s;&|])cd\.\.`)

	// psEnvVarPattern matches PowerShell environment variables ($env:VAR and ${env:VAR}).
	psEnvVarPattern = regexp.MustCompile(`\$\{?env:(\w+)\}?`)

	// cmdEnvVarPattern matches CMD environment variables (%VAR%).
	cmdEnvVarPattern = regexp.MustCompile(`%([^%]+)%`)

	// safePaths are kernel pseudo-devices that are always safe to reference in
	// commands, regardless of workspace restriction. They contain no user data
	// and cannot cause destructive writes.
	safePaths = map[string]bool{
		"/dev/null":    true,
		"/dev/zero":    true,
		"/dev/random":  true,
		"/dev/urandom": true,
		"/dev/stdin":   true,
		"/dev/stdout":  true,
		"/dev/stderr":  true,
	}
)

// encodedCommandFlagPattern matches, in a lower-cased command, a space followed
// by any prefix of -encodedcommand that is at least "-e", or the -ec alias,
// ending at whitespace or the end of the command.
func encodedCommandFlagPattern() *regexp.Regexp {
	const flag = "-encodedcommand"
	alts := []string{"-ec"}
	for n := len(flag); n >= 2; n-- {
		alts = append(alts, flag[:n])
	}
	return regexp.MustCompile(` (?:` + strings.Join(alts, "|") + `)(?:$|\s)`)
}

func NewExecToolWithConfig(
	workingDir string,
	restrict bool,
	cfg *config.Config,
	allowPaths ...[]*regexp.Regexp,
) (*ExecTool, error) {
	denyPatterns := make([]*regexp.Regexp, 0)
	customAllowPatterns := make([]*regexp.Regexp, 0)
	var allowedPathPatterns []*regexp.Regexp
	allowRemote := true
	if len(allowPaths) > 0 {
		allowedPathPatterns = allowPaths[0]
	}

	if cfg != nil {
		execConfig := cfg.Tools.Exec
		enableDenyPatterns := execConfig.EnableDenyPatterns
		allowRemote = execConfig.AllowRemote
		if enableDenyPatterns {
			denyPatterns = append(denyPatterns, defaultDenyPatterns...)
			if runtime.GOOS == "windows" {
				denyPatterns = append(denyPatterns, windowsDenyPatterns...)
			}
			if len(execConfig.CustomDenyPatterns) > 0 {
				logger.InfoCF("tools", "using custom deny patterns", map[string]any{
					"patterns": execConfig.CustomDenyPatterns,
				})
				for _, pattern := range execConfig.CustomDenyPatterns {
					re, err := regexp.Compile(pattern)
					if err != nil {
						return nil, fmt.Errorf("invalid custom deny pattern %q: %w", pattern, err)
					}
					denyPatterns = append(denyPatterns, re)
				}
			}
		} else {
			// If deny patterns are disabled, we won't add any patterns, allowing all commands.
			logger.WarnCF("tools", "deny patterns are disabled, all commands will be allowed", nil)
		}
		for _, pattern := range execConfig.CustomAllowPatterns {
			re, err := regexp.Compile(pattern)
			if err != nil {
				return nil, fmt.Errorf("invalid custom allow pattern %q: %w", pattern, err)
			}
			customAllowPatterns = append(customAllowPatterns, re)
		}
	} else {
		denyPatterns = append(denyPatterns, defaultDenyPatterns...)
		if runtime.GOOS == "windows" {
			denyPatterns = append(denyPatterns, windowsDenyPatterns...)
		}
	}

	var timeout time.Duration
	if cfg != nil && cfg.Tools.Exec.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.Tools.Exec.TimeoutSeconds) * time.Second
	}

	return &ExecTool{
		workingDir:          workingDir,
		timeout:             timeout,
		denyPatterns:        denyPatterns,
		allowPatterns:       nil,
		customAllowPatterns: customAllowPatterns,
		allowedPathPatterns: allowedPathPatterns,
		restrictToWorkspace: restrict,
		allowRemote:         allowRemote,
		sessionManager:      getSessionManager(),
	}, nil
}

// syncOutputLimitBytes caps the output a foreground run returns to the model
// (background sessions buffer up to maxOutputBufferSize instead).
const syncOutputLimitBytes = 10000

func (t *ExecTool) Name() string {
	return "exec"
}

func (t *ExecTool) Description() string {
	return `Execute shell commands. Use background=true for long-running commands (returns sessionId). Use pty=true for interactive commands (can combine with background=true). Use poll/read/write/send-keys/kill with sessionId to manage background sessions. Sessions auto-cleanup 30 minutes after process exits; use kill to terminate early. Output limit: 10000 bytes for a foreground run, a 1MB buffer for a background session.`
}

func (t *ExecTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"run", "list", "poll", "read", "write", "kill", "send-keys"},
				"description": "Action: run (execute command), list (show sessions), poll (check status), read (get output), write (send input), kill (terminate), send-keys (send keys to PTY)",
			},
			"command": map[string]any{
				"type":        "string",
				"description": "Shell command to execute (required for run)",
			},
			"sessionId": map[string]any{
				"type":        "string",
				"description": "Session ID (required for poll/read/write/kill/send-keys)",
			},
			"keys": map[string]any{
				"type":        "string",
				"description": "Key names for send-keys: up, down, left, right, enter, tab, escape, backspace, ctrl-c, ctrl-d, home, end, pageup, pagedown, f1-f12",
			},
			"data": map[string]any{
				"type":        "string",
				"description": "Data to write to stdin (required for write)",
			},
			"background": map[string]any{
				"type":        "string",
				"description": "Run in background immediately (at most 8 running sessions; a session still running after 1 hour is killed)",
			},
			"pty": map[string]any{
				"type":        "string",
				"description": "Run in a pseudo-terminal (PTY) when available",
			},
			"cwd": map[string]any{
				"type":        "string",
				"description": "Working directory for the command",
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Timeout in seconds for this run; 0 or omitted uses the configured default; larger values are capped by tools.exec.timeout_seconds",
			},
		},
		"required": []string{"action"},
	}
}

func (t *ExecTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	if !t.allowRemote && !constants.IsInternalChannel(strings.TrimSpace(ToolChannel(ctx))) {
		return ErrorResult("exec is restricted to internal channels")
	}
	action, _ := args["action"].(string)
	if action == "" {
		return ErrorResult("action is required")
	}

	if action != "run" && action != "list" {
		id, _ := args["sessionId"].(string)
		session, err := t.sessionManager.Get(id)
		if err != nil || session.owner != t.processOwner(ctx) {
			return ErrorResult("session not found in the current context")
		}
	}
	switch action {
	case "run":
		return t.executeRun(ctx, args)
	case "list":
		return t.executeList(ctx)
	case "poll":
		return t.executePoll(args)
	case "read":
		return t.executeRead(args)
	case "write":
		return t.executeWrite(args)
	case "kill":
		return t.executeKill(args)
	case "send-keys":
		return t.executeSendKeys(args)
	default:
		return ErrorResult(fmt.Sprintf("unknown action: %s", action))
	}
}

func (t *ExecTool) executeRun(ctx context.Context, args map[string]any) *ToolResult {
	command, ok := args["command"].(string)
	if !ok {
		return ErrorResult("command is required")
	}

	// GHSA-pv8c-p6jf-3fpp: block exec from remote channels (e.g. Telegram webhooks)
	// unless explicitly opted-in via config. Fail-closed: empty channel = blocked.
	// The channel comes only from the trusted tool context, never from args.
	if !t.allowRemote {
		channel := strings.TrimSpace(ToolChannel(ctx))
		if channel == "" || !constants.IsInternalChannel(channel) {
			return ErrorResult("exec is restricted to internal channels")
		}
	}

	getBoolArg := func(key string) bool {
		switch v := args[key].(type) {
		case bool:
			return v
		case string:
			return v == "true"
		}
		return false
	}
	isPty := getBoolArg("pty")
	isBackground := getBoolArg("background")

	timeout, err := t.effectiveTimeout(args)
	if err != nil {
		return ErrorResult(err.Error())
	}

	if isPty {
		if runtime.GOOS == "windows" {
			return ErrorResult("PTY is not supported on Windows. Use background=true without pty.")
		}
	}

	cwd := t.workingDir
	if wd, ok := args["cwd"].(string); ok && wd != "" {
		if t.restrictToWorkspace && t.workingDir != "" {
			resolvedWD, err := validatePathWithAllowPaths(wd, t.workingDir, true, t.allowedPathPatterns)
			if err != nil {
				return ErrorResult("Command blocked by safety guard (" + err.Error() + ")")
			}
			cwd = resolvedWD
		} else {
			cwd = wd
		}
	}

	if cwd == "" {
		wd, err := os.Getwd()
		if err == nil {
			cwd = wd
		}
	}

	if guardError := t.guardCommand(command, cwd); guardError != "" {
		return ErrorResult(guardError)
	}

	// Re-resolve symlinks immediately before execution to shrink the TOCTOU window
	// between validation and cmd.Dir assignment.
	if t.restrictToWorkspace && t.workingDir != "" && cwd != t.workingDir {
		resolved, err := filepath.EvalSymlinks(cwd)
		if err != nil {
			return ErrorResult(fmt.Sprintf("Command blocked by safety guard (path resolution failed: %v)", err))
		}
		if isAllowedPath(resolved, t.allowedPathPatterns) {
			cwd = resolved
		} else {
			absWorkspace, _ := filepath.Abs(t.workingDir)
			wsResolved, _ := filepath.EvalSymlinks(absWorkspace)
			if wsResolved == "" {
				wsResolved = absWorkspace
			}
			rel, err := filepath.Rel(wsResolved, resolved)
			if err != nil || !filepath.IsLocal(rel) {
				return ErrorResult("Command blocked by safety guard (working directory escaped workspace)")
			}
			cwd = resolved
		}
	}

	if isBackground {
		return t.runBackground(ctx, command, cwd, isPty)
	}

	return t.runSync(ctx, command, cwd, timeout)
}

// effectiveTimeout resolves the per-call "timeout" argument (seconds) against
// the configured exec timeout: absent or 0 uses the configured value, a
// positive value is capped by the configured value when that is set, and the
// configured value 0 means no cap. Negative, fractional or non-numeric values
// are rejected.
func (t *ExecTool) effectiveTimeout(args map[string]any) (time.Duration, error) {
	errInvalid := errors.New("timeout must be a non-negative integer")
	var seconds int64
	switch v := args["timeout"].(type) {
	case nil:
		return t.timeout, nil
	case float64:
		if v != math.Trunc(v) || v < 0 || v > math.MaxInt32 {
			return 0, errInvalid
		}
		seconds = int64(v)
	case int:
		seconds = int64(v)
	case int64:
		seconds = v
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return 0, errInvalid
		}
		seconds = n
	default:
		return 0, errInvalid
	}
	if seconds < 0 || seconds > math.MaxInt32 {
		return 0, errInvalid
	}
	if seconds == 0 {
		return t.timeout, nil
	}
	requested := time.Duration(seconds) * time.Second
	if t.timeout > 0 && requested > t.timeout {
		return t.timeout, nil
	}
	return requested, nil
}

// execWaitDelay bounds how long a finished command's output pipes may be held
// open by processes it left running in the background.
const execWaitDelay = 2 * time.Second

// waitDelayNote is appended to the output when background processes kept the
// output pipes open after the command itself finished.
const waitDelayNote = "\n(processos em segundo plano mantiveram a saída aberta; saída capturada até o fim do comando)"

func (t *ExecTool) runSync(ctx context.Context, command, cwd string, timeout time.Duration) *ToolResult {
	// timeout == 0 means no timeout
	var cmdCtx context.Context
	var cancel context.CancelFunc
	if timeout > 0 {
		cmdCtx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		cmdCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(cmdCtx, "powershell", "-NoProfile", "-NonInteractive", "-Command", command)
	} else {
		cmd = exec.CommandContext(cmdCtx, "sh", "-c", command)
	}
	if cwd != "" {
		cmd.Dir = cwd
	}
	// A grandchild left in the background (e.g. "sleep 30 &") inherits the
	// output pipes; without a WaitDelay, Wait would block until it exits.
	cmd.WaitDelay = execWaitDelay

	prepareCommandForTermination(cmd)
	prepareContextCommandForTermination(cmd)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Route shell execution through the shared isolation entry point so exec tool
	// subprocesses receive the same isolation policy as other integrations.
	if err := isolation.Start(cmd); err != nil {
		return ErrorResult(fmt.Sprintf("failed to start command: %v", err))
	}

	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.ErrorCF("shell", "cmd.Wait goroutine panic recovered",
					map[string]any{
						"panic": fmt.Sprintf("%v", r),
						"stack": string(debug.Stack()),
					})
				done <- fmt.Errorf("panic in cmd.Wait: %v", r)
			}
		}()
		done <- cmd.Wait()
	}()

	var err error
	select {
	case err = <-done:
	case <-cmdCtx.Done():
		_ = terminateProcessTree(cmd)
		select {
		case err = <-done:
		case <-time.After(2 * time.Second):
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			err = <-done
		}
	}

	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\nSTDERR:\n" + stderr.String()
	}

	if err != nil {
		if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
			msg := fmt.Sprintf("Command timed out after %v", timeout)
			if output != "" {
				msg += "\n\nPartial output before timeout:\n" + output
			}
			return &ToolResult{
				ForLLM:  msg,
				ForUser: msg,
				IsError: true,
				Err:     fmt.Errorf("command timeout after %v: %w (%w)", timeout, cmdCtx.Err(), err),
			}
		}

		// The command itself finished; only processes it left in the
		// background kept the output pipes open past execWaitDelay.
		if errors.Is(err, exec.ErrWaitDelay) && cmdCtx.Err() == nil {
			err = nil
			output += waitDelayNote
		}
	}

	if err != nil {
		// Extract detailed exit information
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode := exitErr.ExitCode()
			output += fmt.Sprintf("\n\n[Command exited with code %d]", exitCode)

			// Add signal information if killed by signal (Unix)
			if exitCode == -1 {
				output += " (killed by signal)"
			}
		} else {
			output += fmt.Sprintf("\n\n[Command failed: %v]", err)
		}
	}

	if output == "" {
		output = "(no output)"
	}

	if len(output) > syncOutputLimitBytes {
		// Cut on a character boundary: slicing mid-rune would hand the model
		// invalid UTF-8.
		cut := syncOutputLimitBytes
		for cut > 0 && !utf8.RuneStart(output[cut]) {
			cut--
		}
		output = output[:cut] + fmt.Sprintf("\n... (truncated, %d more bytes)", len(output)-cut)
	}

	if err != nil {
		return &ToolResult{
			ForLLM:  output,
			ForUser: output,
			IsError: true,
		}
	}

	return &ToolResult{
		ForLLM:  output,
		ForUser: output,
		IsError: false,
	}
}

func (t *ExecTool) runBackground(ctx context.Context, command, cwd string, ptyEnabled bool) *ToolResult {
	sessionID := generateSessionID()
	session := &ProcessSession{
		owner:        t.processOwner(ctx),
		outputBuffer: &bytes.Buffer{},
		ID:           sessionID,
		Command:      command,
		PTY:          ptyEnabled,
		Background:   true,
		StartTime:    time.Now().Unix(),
		Status:       "running",
		ptyKeyMode:   PtyKeyModeCSI,
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", command)
	} else {
		cmd = exec.Command("sh", "-c", command)
	}
	if cwd != "" {
		cmd.Dir = cwd
	}

	prepareCommandForTermination(cmd)

	var outputReader *os.File
	var outputWriter *os.File
	var stdinReader *os.File
	var stdinWriter io.WriteCloser
	var tty *os.File

	if ptyEnabled {
		var ptmx *os.File
		var err error
		ptmx, tty, err = pty.Open()
		if err != nil {
			return ErrorResult(fmt.Sprintf("failed to create PTY: %v", err))
		}

		cmd.Stdin = tty
		cmd.Stdout = tty
		cmd.Stderr = tty

		// For PTY, we need Setsid to create a new session.
		// Note: Setsid and Setpgid conflict, so we must replace SysProcAttr entirely.
		setSysProcAttrForPty(cmd)

		session.ptyMaster = ptmx
	} else {
		// stdout and stderr share one pipe, like a terminal: a single reader
		// drains both while the process runs (two pipes read one after the
		// other deadlock once stderr fills its buffer before stdout closes),
		// and the interleaving of the two streams is preserved.
		var err error
		outputReader, outputWriter, err = os.Pipe()
		if err != nil {
			return ErrorResult(fmt.Sprintf("failed to create output pipe: %v", err))
		}
		cmd.Stdout = outputWriter
		cmd.Stderr = outputWriter
		// An explicit pipe (rather than cmd.StdinPipe) keeps both ends in our
		// hands, so a run refused before Start can close them.
		var stdinW *os.File
		stdinReader, stdinW, err = os.Pipe()
		if err != nil {
			_ = outputReader.Close()
			_ = outputWriter.Close()
			return ErrorResult(fmt.Sprintf("failed to create stdin pipe: %v", err))
		}
		cmd.Stdin = stdinReader
		stdinWriter = stdinW
		session.stdinWriter = stdinWriter
	}

	// releaseIO closes every descriptor created above when the run does not start.
	releaseIO := func() {
		if session.ptyMaster != nil {
			_ = session.ptyMaster.Close()
		}
		if tty != nil {
			_ = tty.Close()
		}
		if outputReader != nil {
			_ = outputReader.Close()
			_ = outputWriter.Close()
		}
		if stdinReader != nil {
			_ = stdinReader.Close()
			_ = stdinWriter.Close()
		}
	}

	// Reserve the slot before starting so concurrent runs cannot exceed the cap.
	if err := t.sessionManager.AddIfBelow(session, maxRunningBackgroundSessions); err != nil {
		releaseIO()
		return ErrorResult(err.Error())
	}

	// Background sessions use the same startup path so isolation stays consistent
	// with synchronous exec runs.
	if err := isolation.Start(cmd); err != nil {
		t.sessionManager.Remove(session.ID)
		releaseIO()
		return ErrorResult(fmt.Sprintf("failed to start command: %v", err))
	}
	if outputWriter != nil {
		// The child holds its own copy; closing ours lets the reader see EOF
		// once the process (and anything it spawned) exits.
		_ = outputWriter.Close()
	}
	if stdinReader != nil {
		// Only the child reads stdin; it holds its own copy of this end.
		_ = stdinReader.Close()
	}

	session.mu.Lock()
	session.PID = cmd.Process.Pid
	session.mu.Unlock()

	// PTY mode: read from ptyMaster and wait for process
	// Note: On Linux, closing ptyMaster doesn't interrupt blocking Read() calls,
	// so we need cmd.Wait() in a separate goroutine to detect process exit.
	if session.PTY && session.ptyMaster != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.ErrorCF("shell", "PTY cmd.Wait goroutine panic recovered",
						map[string]any{
							"panic": fmt.Sprintf("%v", r),
							"stack": string(debug.Stack()),
						})
					session.mu.Lock()
					session.Status = "error"
					session.markEndedLocked()
					session.mu.Unlock()
				}
			}()
			cmd.Wait() // Wait for process to exit
			session.markExited(cmd.ProcessState)
		}()

		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.ErrorCF("shell", "PTY read goroutine panic recovered",
						map[string]any{
							"panic": fmt.Sprintf("%v", r),
							"stack": string(debug.Stack()),
						})
				}
			}()
			buf := make([]byte, 4096)
			for {
				n, err := session.ptyMaster.Read(buf)
				if n > 0 {
					raw := string(buf[:n])
					if mode := detectPtyKeyMode(raw); mode != PtyKeyModeNotFound && mode != session.GetPtyKeyMode() {
						session.SetPtyKeyMode(mode)
					}

					session.mu.Lock()
					if session.outputBuffer.Len() >= maxOutputBufferSize {
						if !session.outputTruncated {
							session.outputBuffer.WriteString(outputTruncateMarker)
							session.outputTruncated = true
						}
					} else {
						session.outputBuffer.Write(buf[:n])
					}
					session.mu.Unlock()
				}
				if err != nil {
					break
				}
			}
		}()
	} else {
		// Non-PTY mode: one goroutine reads the shared output pipe.
		// When Read() returns EOF (pipe closed), we break.
		// When process exits, OS closes pipe write end → Read() returns EOF → we exit.
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.ErrorCF("shell", "pipe read goroutine panic recovered",
						map[string]any{
							"panic": fmt.Sprintf("%v", r),
							"stack": string(debug.Stack()),
						})
				}
			}()
			buf := make([]byte, 4096)

			// Read the shared stdout+stderr pipe until every writer is closed.
			defer outputReader.Close()
			for {
				n, err := outputReader.Read(buf)
				if n > 0 {
					session.mu.Lock()
					if session.outputBuffer.Len() >= maxOutputBufferSize {
						if !session.outputTruncated {
							session.outputBuffer.WriteString(outputTruncateMarker)
							session.outputTruncated = true
						}
					} else {
						session.outputBuffer.Write(buf[:n])
					}
					session.mu.Unlock()
				}
				if err != nil {
					break
				}
			}

			// All pipes closed, get exit status
			if stdinWriter != nil {
				_ = stdinWriter.Close()
			}
			cmd.Wait()
			session.markExited(cmd.ProcessState)
		}()
	}

	resp := ExecResponse{
		SessionID: sessionID,
		Status:    "running",
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return ErrorResult(err.Error())
	}
	return &ToolResult{
		ForLLM:  string(data),
		ForUser: fmt.Sprintf("Session %s started", sessionID),
		IsError: false,
	}
}

func (t *ExecTool) processOwner(ctx context.Context) string {
	identity := []string{t.workingDir, ToolAgentID(ctx), ToolSessionKey(ctx), ToolChannel(ctx), ToolChatID(ctx)}
	if identity[1] == "" && identity[2] == "" && identity[3] == "" && identity[4] == "" {
		// Direct local callers without runtime identity cannot share sessions
		// with other tool instances.
		return fmt.Sprintf("local:%p", t)
	}
	encoded, _ := json.Marshal(identity)
	return string(encoded)
}

func (t *ExecTool) executeList(ctx context.Context) *ToolResult {
	var sessions []SessionInfo
	for _, info := range t.sessionManager.List() {
		session, err := t.sessionManager.Get(info.ID)
		if err == nil && session.owner == t.processOwner(ctx) {
			sessions = append(sessions, info)
		}
	}
	resp := ExecResponse{
		Sessions: sessions,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return ErrorResult(err.Error())
	}
	return &ToolResult{
		ForLLM:  string(data),
		ForUser: fmt.Sprintf("%d active sessions", len(sessions)),
		IsError: false,
	}
}

func (t *ExecTool) executePoll(args map[string]any) *ToolResult {
	sessionID, ok := args["sessionId"].(string)
	if !ok {
		return ErrorResult("sessionId is required")
	}

	session, err := t.sessionManager.Get(sessionID)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return ErrorResult(fmt.Sprintf("session not found: %s", sessionID))
		}
		return ErrorResult(err.Error())
	}

	resp := ExecResponse{
		SessionID: sessionID,
		Status:    session.GetStatus(),
		ExitCode:  session.GetExitCode(),
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return ErrorResult(err.Error())
	}
	return &ToolResult{
		ForLLM:  string(data),
		IsError: false,
	}
}

func (t *ExecTool) executeRead(args map[string]any) *ToolResult {
	sessionID, ok := args["sessionId"].(string)
	if !ok {
		return ErrorResult("sessionId is required")
	}

	session, err := t.sessionManager.Get(sessionID)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return ErrorResult(fmt.Sprintf("session not found: %s", sessionID))
		}
		return ErrorResult(err.Error())
	}

	output := session.Read()

	resp := ExecResponse{
		SessionID: sessionID,
		Output:    output,
		Status:    session.GetStatus(),
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return ErrorResult(err.Error())
	}
	return &ToolResult{
		ForLLM:  string(data),
		IsError: false,
	}
}

func (t *ExecTool) executeWrite(args map[string]any) *ToolResult {
	sessionID, ok := args["sessionId"].(string)
	if !ok {
		return ErrorResult("sessionId is required")
	}

	data, ok := args["data"].(string)
	if !ok {
		return ErrorResult("data is required")
	}

	session, err := t.sessionManager.Get(sessionID)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return ErrorResult(fmt.Sprintf("session not found: %s", sessionID))
		}
		return ErrorResult(err.Error())
	}

	if session.IsDone() {
		return ErrorResult(fmt.Sprintf("process already exited with code %d", session.GetExitCode()))
	}

	if err = session.Write(data); err != nil {
		if errors.Is(err, ErrSessionDone) {
			return ErrorResult(fmt.Sprintf("process already exited with code %d", session.GetExitCode()))
		}
		return ErrorResult(fmt.Sprintf("failed to write to session: %v", err))
	}

	resp := ExecResponse{
		SessionID: sessionID,
		Status:    session.GetStatus(),
	}
	respData, err := json.Marshal(resp)
	if err != nil {
		return ErrorResult(err.Error())
	}
	return &ToolResult{
		ForLLM:  string(respData),
		IsError: false,
	}
}

func (t *ExecTool) executeKill(args map[string]any) *ToolResult {
	sessionID, ok := args["sessionId"].(string)
	if !ok {
		return ErrorResult("sessionId is required")
	}

	session, err := t.sessionManager.Get(sessionID)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return ErrorResult(fmt.Sprintf("session not found: %s", sessionID))
		}
		return ErrorResult(err.Error())
	}

	if session.IsDone() {
		return ErrorResult(fmt.Sprintf("process already exited with code %d", session.GetExitCode()))
	}

	if err = session.Kill(); err != nil {
		return ErrorResult(fmt.Sprintf("failed to kill session: %v", err))
	}

	t.sessionManager.Remove(sessionID)

	resp := ExecResponse{
		SessionID: sessionID,
		Status:    "done",
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return ErrorResult(err.Error())
	}
	return &ToolResult{
		ForLLM:  string(data),
		ForUser: fmt.Sprintf("Session %s killed", sessionID),
		IsError: false,
	}
}

// keyMap maps key names to their escape sequences.
var keyMap = map[string]string{
	"enter":     "\r",
	"return":    "\r",
	"tab":       "\t",
	"escape":    "\x1b",
	"esc":       "\x1b",
	"space":     " ",
	"backspace": "\x7f",
	"bspace":    "\x7f",
	"up":        "\x1b[A",
	"down":      "\x1b[B",
	"right":     "\x1b[C",
	"left":      "\x1b[D",
	"home":      "\x1b[1~",
	"end":       "\x1b[4~",
	"pageup":    "\x1b[5~",
	"pagedown":  "\x1b[6~",
	"pgup":      "\x1b[5~",
	"pgdn":      "\x1b[6~",
	"insert":    "\x1b[2~",
	"ic":        "\x1b[2~",
	"delete":    "\x1b[3~",
	"del":       "\x1b[3~",
	"dc":        "\x1b[3~",
	"btab":      "\x1b[Z",
	"f1":        "\x1bOP",
	"f2":        "\x1bOQ",
	"f3":        "\x1bOR",
	"f4":        "\x1bOS",
	"f5":        "\x1b[15~",
	"f6":        "\x1b[17~",
	"f7":        "\x1b[18~",
	"f8":        "\x1b[19~",
	"f9":        "\x1b[20~",
	"f10":       "\x1b[21~",
	"f11":       "\x1b[23~",
	"f12":       "\x1b[24~",
}

// ss3KeysMap maps key names to SS3 escape sequences
var ss3KeysMap = map[string]string{
	"up":    "\x1bOA",
	"down":  "\x1bOB",
	"right": "\x1bOC",
	"left":  "\x1bOD",
	"home":  "\x1bOH",
	"end":   "\x1bOF",
}

func detectPtyKeyMode(raw string) PtyKeyMode {
	const SMKX = "\x1b[?1h"
	const RMKX = "\x1b[?1l"

	lastSmkx := strings.LastIndex(raw, SMKX)
	lastRmkx := strings.LastIndex(raw, RMKX)

	if lastSmkx == -1 && lastRmkx == -1 {
		return PtyKeyModeNotFound
	}

	if lastSmkx > lastRmkx {
		return PtyKeyModeSS3
	}
	return PtyKeyModeCSI
}

// encodeKeyToken encodes a single key token into its escape sequence.
// Supports:
//   - Named keys: "enter", "tab", "up", "ctrl-c", "alt-x", etc.
//   - Ctrl modifier: "ctrl-c" or "c-c" (sends Ctrl+char)
//   - Alt modifier: "alt-x" or "m-x" (sends ESC+char)
func encodeKeyToken(token string, ptyKeyMode PtyKeyMode) (string, error) {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" {
		return "", nil
	}

	// Handle ctrl-X format (c-x)
	if strings.HasPrefix(token, "c-") {
		char := token[2]
		if char >= 'a' && char <= 'z' {
			return string(rune(char) & 0x1f), nil // ctrl-a through ctrl-z
		}
		return "", fmt.Errorf("invalid ctrl key: %s", token)
	}

	// Handle ctrl-X format (ctrl-x)
	if strings.HasPrefix(token, "ctrl-") {
		char := token[5]
		if char >= 'a' && char <= 'z' {
			return string(rune(char) & 0x1f), nil
		}
		return "", fmt.Errorf("invalid ctrl key: %s", token)
	}

	// Handle alt-X format (m-x or alt-x)
	if strings.HasPrefix(token, "m-") || strings.HasPrefix(token, "alt-") {
		var char string
		if strings.HasPrefix(token, "m-") {
			char = token[2:]
		} else {
			char = token[4:]
		}
		if len(char) == 1 {
			return "\x1b" + char, nil
		}
		return "", fmt.Errorf("invalid alt key: %s", token)
	}

	// Handle shift modifier for special keys (shift-up, shift-down, etc.)
	if strings.HasPrefix(token, "s-") || strings.HasPrefix(token, "shift-") {
		var key string
		if strings.HasPrefix(token, "s-") {
			key = token[2:]
		} else {
			key = token[6:]
		}
		// Apply shift modifier: for single-char keys, return uppercase
		if seq, ok := keyMap[key]; ok {
			// For escape sequences, we can't easily add shift
			// For single-char keys (letters), return uppercase
			if len(seq) == 1 {
				return strings.ToUpper(seq), nil
			}
			return seq, nil
		}
		return "", fmt.Errorf("unknown key with shift: %s", key)
	}

	if ptyKeyMode == PtyKeyModeSS3 {
		if seq, ok := ss3KeysMap[token]; ok {
			return seq, nil
		}
	}

	if seq, ok := keyMap[token]; ok {
		return seq, nil
	}

	return "", fmt.Errorf("unknown key: %s (use write action for text input)", token)
}

// encodeKeySequence encodes a slice of key tokens into a single string.
func encodeKeySequence(tokens []string, ptyKeyMode PtyKeyMode) (string, error) {
	var result string
	for _, token := range tokens {
		seq, err := encodeKeyToken(token, ptyKeyMode)
		if err != nil {
			return "", err
		}
		result += seq
	}
	return result, nil
}

func (t *ExecTool) executeSendKeys(args map[string]any) *ToolResult {
	sessionID, ok := args["sessionId"].(string)
	if !ok {
		return ErrorResult("sessionId is required")
	}

	keysStr, ok := args["keys"].(string)
	if !ok {
		return ErrorResult("keys must be a string")
	}

	if keysStr == "" {
		return ErrorResult("keys cannot be empty")
	}

	// Parse comma-separated key names
	keyNames := strings.Split(keysStr, ",")
	var keys []string
	for _, k := range keyNames {
		k = strings.TrimSpace(k)
		if k != "" {
			keys = append(keys, k)
		}
	}

	if len(keys) == 0 {
		return ErrorResult("keys cannot be empty")
	}

	session, err := t.sessionManager.Get(sessionID)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return ErrorResult(fmt.Sprintf("session not found: %s", sessionID))
		}
		return ErrorResult(err.Error())
	}

	ptyKeyMode := session.GetPtyKeyMode()

	data, err := encodeKeySequence(keys, ptyKeyMode)
	if err != nil {
		return ErrorResult(fmt.Sprintf("invalid key: %v", err))
	}

	if session.IsDone() {
		return ErrorResult(fmt.Sprintf("process already exited with code %d", session.GetExitCode()))
	}

	if err = session.Write(data); err != nil {
		if errors.Is(err, ErrSessionDone) {
			return ErrorResult(fmt.Sprintf("process already exited with code %d", session.GetExitCode()))
		}
		return ErrorResult(fmt.Sprintf("failed to send keys: %v", err))
	}

	resp := ExecResponse{
		SessionID: sessionID,
		Status:    "running",
		Output:    fmt.Sprintf("Sent keys: %v", keys),
	}
	respData, err := json.Marshal(resp)
	if err != nil {
		return ErrorResult(err.Error())
	}
	return &ToolResult{
		ForLLM:  string(respData),
		IsError: false,
	}
}

// expandPowerShellEnvVars expands environment variable syntax used by both
// PowerShell ($env:VAR) and CMD (%VAR%) to their actual values.
func expandPowerShellEnvVars(cmd string) string {
	// Handle PowerShell style: $env:VAR and ${env:VAR}
	cmd = psEnvVarPattern.ReplaceAllStringFunc(cmd, func(match string) string {
		varName := psEnvVarPattern.FindStringSubmatch(match)[1]
		if val := os.Getenv(varName); val != "" {
			return val
		}
		return match
	})

	// Handle CMD style: %VAR%
	return cmdEnvVarPattern.ReplaceAllStringFunc(cmd, func(match string) string {
		varName := cmdEnvVarPattern.FindStringSubmatch(match)[1]
		if val := os.Getenv(varName); val != "" {
			return val
		}
		return match
	})
}

func (t *ExecTool) commandMatchesAllowPattern(lower string) bool {
	for _, pattern := range t.allowPatterns {
		if pattern.MatchString(lower) {
			return true
		}
	}
	for _, pattern := range t.customAllowPatterns {
		if pattern.MatchString(lower) {
			return true
		}
	}
	return false
}

func (t *ExecTool) guardCommand(command, cwd string) string {
	cmd := strings.TrimSpace(command)
	lower := strings.ToLower(cmd)

	// Deny patterns always apply, even when a command matches a custom allow rule.
	// Custom allow rules can permit a command, but must not disable secret-safety
	// deny rules such as jq env access checks (#3079).
	for _, pattern := range t.denyPatterns {
		if pattern.MatchString(lower) {
			return "Command blocked by safety guard (dangerous pattern detected)"
		}
	}

	if len(t.allowPatterns) > 0 {
		if !t.commandMatchesAllowPattern(lower) {
			return "Command blocked by safety guard (not in allowlist)"
		}
	}

	if t.restrictToWorkspace {
		// Block path traversal: ".." followed by a separator (including
		// .../.../ variants), ".." as a path token of its own, ".." attached
		// to a one-letter option, and "cd.." without a space.
		if pathTraversalPattern.MatchString(cmd) ||
			dotDotTokenPattern.MatchString(cmd) ||
			dotDotShortOptionPattern.MatchString(cmd) ||
			cdDotDotPattern.MatchString(cmd) {
			return "Command blocked by safety guard (path traversal detected)"
		}

		cwdPath, err := filepath.Abs(cwd)
		if err != nil {
			return "Command blocked by safety guard (cannot resolve working directory)"
		}

		// Web URL schemes whose path components (starting with //) should be exempt
		// from workspace sandbox checks. file: is intentionally excluded so that
		// file:// URIs are still validated against the workspace boundary.
		webSchemes := []string{"http:", "https:", "ftp:", "ftps:", "sftp:", "ssh:", "git:"}

		// On Windows, expand ~ and PowerShell environment variables ($env:VAR) before path checking
		if runtime.GOOS == "windows" {
			// Expand PowerShell environment variables ($env:VAR and ${env:VAR})
			cmd = expandPowerShellEnvVars(cmd)
			// Also expand ~ for completeness
			if home, err := os.UserHomeDir(); err == nil {
				cmd = strings.ReplaceAll(cmd, "~", filepath.FromSlash(home))
			}
		}

		matchIndices := absolutePathPattern.FindAllStringIndex(cmd, -1)

		for _, loc := range matchIndices {
			raw := cmd[loc[0]:loc[1]]

			// Skip URL path components that look like they're from web URLs.
			// When a URL like "https://github.com" is parsed, the regex captures
			// "//github.com" as a match (the path portion after "https:").
			// Use the exact match position (loc[0]) so that duplicate //path substrings
			// in the same command are each evaluated at their own position.
			if strings.HasPrefix(raw, "//") && loc[0] > 0 {
				before := cmd[:loc[0]]
				isWebURL := false

				for _, scheme := range webSchemes {
					if strings.HasSuffix(before, scheme) {
						isWebURL = true
						break
					}
				}

				if isWebURL {
					continue
				}
			}

			// Skip scheme-less URL paths like "wttr.in/Beijing".
			// When a /path is immediately preceded by a token that looks
			// like a domain name and that token does NOT exist as a local
			// filesystem entry, treat the path as part of a URL and skip
			// workspace sandbox validation.
			//
			// The local-path-exists guard prevents symlink bypass: if
			// "foo.bar" exists as a local symlink or directory, the path
			// still undergoes full workspace validation (see #2965).
			if loc[0] > 0 && raw[0] == '/' {
				// Find the token immediately before the "/".
				j := loc[0] - 1
				for j >= 0 && !isShellTokenBoundary(cmd[j]) {
					j--
				}
				token := cmd[j+1 : loc[0]]
				if looksLikeDomain(token) && !localPathExists(cwd, token) {
					continue
				}
			}

			p, err := commandPathAbs(commandPathTextFromMatch(cmd, loc[0], loc[1]), cwdPath)
			if err != nil {
				return "Command blocked by safety guard (cannot resolve path)"
			}

			// Windows-specific: normalize paths to block ADS and extended-length paths
			if runtime.GOOS == "windows" {
				// Strip \\?\ prefix (extended-length path)
				p = strings.TrimPrefix(p, `\\?\`)
				// Strip NTFS alternate data streams (only if colon is not at position 1 = drive letter)
				if idx := strings.Index(p, ":"); idx > 1 {
					p = p[:idx]
				}
			}

			// Check symlinks and junctions
			resolved, err := filepath.EvalSymlinks(p)
			if err == nil {
				p = resolved
			}

			if safePaths[p] {
				continue
			}
			if isAllowedPath(p, t.allowedPathPatterns) {
				continue
			}

			rel, err := filepath.Rel(cwdPath, p)
			if err != nil {
				return "Command blocked by safety guard (path outside working dir)"
			}

			if strings.HasPrefix(rel, "..") {
				return "Command blocked by safety guard (path outside working dir)"
			}
		}
	}

	return ""
}

func commandPathAbs(pathText, cwdPath string) (string, error) {
	if filepath.IsAbs(pathText) {
		return filepath.Abs(pathText)
	}
	return filepath.Abs(filepath.Join(cwdPath, pathText))
}

func commandPathTextFromMatch(cmd string, start, end int) string {
	raw := cmd[start:end]
	if !strings.HasPrefix(raw, "/") || isUnixAbsolutePathMatchStart(cmd, start) {
		return raw
	}

	tokenStart, tokenEnd := shellTokenBounds(cmd, start)
	prefix := cmd[tokenStart:start]
	// For --flag=rel/path, validate the value. For ambiguous attached option
	// forms like -isystem/path, keep the slash-starting path conservative.
	if eq := strings.IndexByte(prefix, '='); eq >= 0 {
		return cmd[tokenStart+eq+1 : tokenEnd]
	}
	if strings.HasPrefix(prefix, "-") {
		return raw
	}
	return cmd[tokenStart:tokenEnd]
}

func shellTokenBounds(cmd string, idx int) (int, int) {
	start := idx
	for start > 0 && !isShellTokenBoundary(cmd[start-1]) {
		start--
	}
	end := idx
	for end < len(cmd) && !isShellTokenBoundary(cmd[end]) {
		end++
	}
	return start, end
}

// isUnixAbsolutePathMatchStart returns true when a regex match beginning with
// "/" is actually an absolute path token, not the separator inside a relative
// path such as "skills/foo.py".
func isUnixAbsolutePathMatchStart(cmd string, idx int) bool {
	if idx <= 0 {
		return true
	}

	prev := cmd[idx-1]
	if isShellTokenBoundary(prev) || prev == '=' || prev == ',' || prev == '(' || prev == '[' || prev == '{' {
		return true
	}

	j := idx - 1
	for j >= 0 && !isShellTokenBoundary(cmd[j]) {
		j--
	}
	prefix := cmd[j+1 : idx]

	return strings.HasPrefix(prefix, "-") && !strings.Contains(prefix, "=")
}

// isShellTokenBoundary returns true when b is a byte that separates
// tokens in a shell command (space, tab, colon, semicolon, pipe, etc.).
func isShellTokenBoundary(b byte) bool {
	switch b {
	case ' ', '\t', ':', ';', '|', '&', '<', '>', '\'', '"', '`', '\n', '\r':
		return true
	}
	return false
}

// looksLikeDomain returns true when s looks like a DNS domain name:
// it contains at least one dot, starts with an alphanumeric character,
// and does not end with a common file extension.
func looksLikeDomain(s string) bool {
	if len(s) < 3 || !strings.ContainsRune(s, '.') {
		return false
	}
	first := s[0]
	if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') || (first >= '0' && first <= '9')) {
		return false
	}
	// Exclude tokens ending with common file/programming extensions,
	// e.g. "script.py", "main.go", "app.exe".
	if idx := strings.LastIndexByte(s, '.'); idx >= 0 {
		ext := strings.ToLower(s[idx+1:])
		if commonFileExtension(ext) {
			return false
		}
	}
	return true
}

// commonFileExtension returns true when ext is a file extension that
// strongly indicates a local file rather than a domain TLD.
func commonFileExtension(ext string) bool {
	switch ext {
	case "py", "js", "ts", "tsx", "jsx", "go", "rs", "rb", "php",
		"java", "c", "cpp", "h", "hpp", "cs", "swift", "kt", "scala",
		"sh", "bash", "zsh", "fish", "ps1", "bat", "cmd",
		"txt", "md", "rst", "log", "json", "yaml", "yml", "toml",
		"xml", "html", "css", "scss", "ini", "cfg", "conf", "env",
		"exe", "dll", "so", "dylib", "lib", "a", "o", "obj",
		"zip", "tar", "gz", "bz2", "xz", "7z", "rar",
		"png", "jpg", "jpeg", "gif", "svg", "ico", "bmp", "webp",
		"mp3", "mp4", "wav", "avi", "mov", "mkv", "flac",
		"pdf", "doc", "docx", "xls", "xlsx", "ppt", "pptx",
		"pub", "pem", "key", "crt", "cer", "p12", "pfx",
		"bak", "tmp", "swp", "lock",
		"ttf", "otf", "woff", "woff2", "eot",
		"deb", "rpm", "apk", "msi", "dmg",
		"sql", "sqlite", "db":
		return true
	}
	return false
}

// localPathExists returns true when the given token resolves to an
// existing filesystem entry relative to cwd.
func localPathExists(cwd, token string) bool {
	info, err := os.Lstat(filepath.Join(cwd, token))
	return err == nil && info != nil
}

func (t *ExecTool) SetTimeout(timeout time.Duration) {
	t.timeout = timeout
}

func (t *ExecTool) SetRestrictToWorkspace(restrict bool) {
	t.restrictToWorkspace = restrict
}
