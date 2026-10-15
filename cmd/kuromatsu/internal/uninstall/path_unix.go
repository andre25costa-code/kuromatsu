//go:build !windows

package uninstall

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"

	"github.com/andre25costa-code/kuromatsu/pkg/fileutil"
)

// profileFiles are the shell startup files the installer may have added a
// PATH line to.
var profileFiles = []string{".profile", ".bash_profile", ".bash_login", ".bashrc", ".zshrc"}

// writable reports whether the user may create and delete files in dir.
func writable(dir string) bool {
	return unix.Access(dir, unix.W_OK) == nil
}

func removeBinary(exe string, _ time.Time) error {
	return os.Remove(exe)
}

// removeFromPath removes the installer's PATH lines from the shell profiles
// in userHome. The PATH itself lives in those files on Unix, so exeDir is
// not needed.
func removeFromPath(_, userHome string, out io.Writer) error {
	if userHome == "" {
		return errors.New("home directory unknown")
	}
	var errs []error
	for _, name := range profileFiles {
		p := filepath.Join(userHome, name)
		changed, err := stripProfile(p)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if changed {
			fmt.Fprintf(out, "Removed the installer's PATH line from %s\n", p)
		}
	}
	return errors.Join(errs...)
}

// stripProfile rewrites p without the installer's lines, keeping its mode.
// A symlinked profile (dotfile managers) is rewritten at its target.
func stripProfile(p string) (bool, error) {
	target, err := filepath.EvalSymlinks(p)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return false, err
	}
	stripped, changed := stripInstallerLines(string(data))
	if !changed {
		return false, nil
	}
	if err := fileutil.WriteFileAtomic(target, []byte(stripped), info.Mode().Perm()); err != nil {
		return false, fmt.Errorf("rewrite %s: %w", p, err)
	}
	return true, nil
}
