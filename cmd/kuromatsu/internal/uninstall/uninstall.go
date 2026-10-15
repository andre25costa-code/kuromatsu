package uninstall

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
)

// installerMarker ends every line the install script adds to a shell profile.
const installerMarker = "# added by kuromatsu installer"

// packageManagerMsg is printed when the binary is left in place.
const packageManagerMsg = "installed by a package manager or system-wide; " +
	"remove it with your package manager (e.g. `apt remove kuromatsu`)"

type options struct {
	purge bool
	force bool
	yes   bool
}

// env is everything run touches outside its arguments, so tests can
// replace the system.
type env struct {
	stdin  io.Reader
	stdout io.Writer

	home        string // data directory (config.GetHome())
	homeFromEnv bool   // home came from KUROMATSU_HOME
	userHome    string // the user's home directory

	executable     func() (string, error)
	isTerminal     func() bool
	runningGateway func(home string) (pid int, running bool)
	freeSpace      func(dir string) (uint64, error)
	writable       func(dir string) bool
	removeBinary   func(exe string, now time.Time) error
	updatePath     func(exeDir, userHome string, out io.Writer) error
	now            func() time.Time
}

func run(o options, e *env) error {
	out := e.stdout

	if gwPID, running := e.runningGateway(e.home); running {
		return fmt.Errorf("gateway is running (PID %d); stop it first", gwPID)
	}

	exe, err := e.executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		return fmt.Errorf("cannot locate the kuromatsu binary: %w", err)
	}
	exeDir := filepath.Dir(exe)
	managed := packageManaged(exeDir, e.writable)

	// Every purge check runs before asking, so a refusal never follows a "yes".
	var plan *purgePlan
	if o.purge {
		plan, err = preparePurge(e.home, e.userHome, o.force, e.freeSpace, e.now())
		if err != nil {
			return err
		}
	}

	if !o.yes {
		if e.isTerminal == nil || !e.isTerminal() {
			return errors.New("no terminal to confirm on; re-run with --yes to uninstall")
		}
		ok, askErr := confirm(e.stdin, out, exe, managed, plan)
		if askErr != nil {
			return askErr
		}
		if !ok {
			fmt.Fprintln(out, "Aborted; nothing was changed.")
			return nil
		}
	} else if plan != nil && e.homeFromEnv {
		fmt.Fprintf(out, "Data directory (from KUROMATSU_HOME): %s\n", plan.home)
	}

	if managed {
		fmt.Fprintf(out, "Keeping %s: %s\n", exe, packageManagerMsg)
	} else if rmErr := e.removeBinary(exe, e.now()); rmErr != nil {
		fmt.Fprintf(out, "Keeping %s (%v): %s\n", exe, rmErr, packageManagerMsg)
	} else {
		fmt.Fprintf(out, "Removed %s\n", exe)
	}

	if pathErr := e.updatePath(exeDir, e.userHome, out); pathErr != nil {
		fmt.Fprintf(out, "Warning: could not update PATH: %v\n", pathErr)
	}

	if plan == nil {
		fmt.Fprintf(out, "Data directory kept: %s (use --purge to delete it)\n", e.home)
		return nil
	}
	return plan.execute(out)
}

// confirm shows what will happen, with absolute paths, and reads y/N.
func confirm(in io.Reader, out io.Writer, exe string, managed bool, plan *purgePlan) (bool, error) {
	fmt.Fprintln(out, "kuromatsu uninstall will:")
	if managed {
		fmt.Fprintf(out, "  keep the binary   %s (%s)\n", exe, packageManagerMsg)
	} else {
		fmt.Fprintf(out, "  remove the binary %s\n", exe)
	}
	fmt.Fprintln(out, "  remove the PATH entries added by the installer")
	if plan != nil {
		fmt.Fprintf(out, "  back up to        %s\n", plan.backup)
		fmt.Fprintf(out, "  then DELETE       %s\n", plan.home)
		if len(plan.unexpected) > 0 {
			fmt.Fprintf(out, "  including unexpected entries: %s\n", strings.Join(plan.unexpected, ", "))
		}
	}
	fmt.Fprint(out, "Continue? [y/N] ")

	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("read answer: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	}
	return false, nil
}

// systemBinDirs hold binaries owned by the system or a package manager.
var systemBinDirs = map[string]bool{
	"/usr/bin":       true,
	"/usr/local/bin": true,
	"/bin":           true,
	"/sbin":          true,
}

// packageManaged reports whether a binary in exeDir must be left alone: a
// system directory, a package tree (/opt, /snap) or a directory the user
// cannot write to.
func packageManaged(exeDir string, writable func(string) bool) bool {
	dir := filepath.ToSlash(filepath.Clean(exeDir))
	if systemBinDirs[dir] ||
		strings.HasPrefix(dir, "/opt/") || strings.HasPrefix(dir, "/snap/") {
		return true
	}
	return !writable(exeDir)
}

// stripInstallerLines removes the lines ending with the installer marker and
// keeps every other byte of content as it was.
func stripInstallerLines(content string) (string, bool) {
	var b strings.Builder
	changed := false
	for _, line := range strings.SplitAfter(content, "\n") {
		if strings.HasSuffix(strings.TrimRight(line, "\r\n"), installerMarker) {
			changed = true
			continue
		}
		b.WriteString(line)
	}
	if !changed {
		return content, false
	}
	return b.String(), true
}

// removeFromUserPath drops the entries of a ';'-separated Windows PATH value
// that, once expanded, name dir. Other entries are kept literally, so
// unexpanded references like %USERPROFILE%\go\bin survive.
func removeFromUserPath(raw, dir string, expand func(string) string) (string, bool) {
	want := normalizeWinPath(dir)
	entries := strings.Split(raw, ";")
	kept := entries[:0:0]
	changed := false
	for _, entry := range entries {
		if strings.TrimSpace(entry) != "" && strings.EqualFold(normalizeWinPath(expand(entry)), want) {
			changed = true
			continue
		}
		kept = append(kept, entry)
	}
	if !changed {
		return raw, false
	}
	return strings.Join(kept, ";"), true
}

// normalizeWinPath cleans a Windows path the same way on every OS, so
// trailing separators and "." segments do not hide a match.
func normalizeWinPath(p string) string {
	p = strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
	p = strings.TrimRight(p, "/")
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(p)))
}
