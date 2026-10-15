package uninstall

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/credential"
)

// knownEntries are the top-level names Kuromatsu itself creates in its data
// directory.
var knownEntries = map[string]bool{
	"config.json":      true,
	".security.yml":    true,
	".kuromatsu.pid":   true, // gateway PID file
	"workspace":        true,
	"models":           true, // native model files
	"skills":           true, // global skills
	"run":              true, // runstate file, agent pins
	"state":            true,
	"telemetry":        true, // turns.db
	"auth.json":        true,
	"logs":             true, // gateway logs
	"cache":            true, // isolation runtime
	"runtime-user-env": true, // isolation runtime
}

// knownPatterns match names Kuromatsu creates with a variable part (path.Match
// syntax).
var knownPatterns = []string{
	"kuromatsu-backup-*.zip",
	"config.json.*.bak",   // config backups made before a migration
	".security.yml.*.bak", // the matching security-file backups
	".kuromatsu.pid.tmp",  // PID file being written
	".tmp-*",              // atomic writes in progress
}

func isKnownEntry(name string) bool {
	if knownEntries[name] {
		return true
	}
	for _, p := range knownPatterns {
		if ok, _ := path.Match(p, name); ok {
			return true
		}
	}
	return false
}

var errNotKuromatsuHome = errors.New("not a Kuromatsu data directory (no config.json or .security.yml)")

// checkKuromatsuHome checks that fsys (the data directory) holds config.json
// or .security.yml, and lists the top-level entries Kuromatsu does not
// create. Without force, any such entry is an error. force never skips the
// config check.
func checkKuromatsuHome(fsys fs.FS, force bool) (unexpected []string, err error) {
	_, cfgErr := fs.Stat(fsys, "config.json")
	_, secErr := fs.Stat(fsys, ".security.yml")
	if cfgErr != nil && secErr != nil {
		return nil, errNotKuromatsuHome
	}

	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("list data directory: %w", err)
	}
	for _, e := range entries {
		if !isKnownEntry(e.Name()) {
			unexpected = append(unexpected, e.Name())
		}
	}
	sort.Strings(unexpected)
	if len(unexpected) > 0 && !force {
		return unexpected, fmt.Errorf("unexpected entries: %s; re-run with --force to delete them too",
			strings.Join(unexpected, ", "))
	}
	return unexpected, nil
}

var driveRoot = regexp.MustCompile(`^[A-Za-z]:[\\/]*$`)

// unsafePurgeTarget refuses data directories that must never be deleted,
// whatever the flags: a relative path, a filesystem or drive root, the
// user's own home, and paths with fewer than two components.
func unsafePurgeTarget(home, userHome string) error {
	refuse := func(why string) error {
		return fmt.Errorf("refusing to delete %q: %s", home, why)
	}
	if strings.TrimSpace(home) == "" || filepath.Clean(home) == "." {
		return refuse("it is the current directory")
	}
	if driveRoot.MatchString(home) {
		return refuse("it is a drive root")
	}
	if !filepath.IsAbs(home) {
		return refuse("not an absolute path")
	}
	clean := filepath.Clean(home)
	if filepath.Dir(clean) == clean {
		return refuse("it is a filesystem root")
	}
	if userHome != "" && samePath(clean, filepath.Clean(userHome)) {
		return refuse("it is your home directory")
	}
	rest := strings.TrimPrefix(clean, filepath.VolumeName(clean))
	parts := 0
	for _, p := range strings.FieldsFunc(rest, func(r rune) bool { return r == '/' || r == filepath.Separator }) {
		if p != "" {
			parts++
		}
	}
	if parts < 2 {
		return refuse("the path is too short to be a data directory")
	}
	return nil
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// within reports whether p is dir or inside it.
func within(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// purgePlan is a checked --purge, ready to run.
type purgePlan struct {
	home       string
	backup     string
	unexpected []string
}

// preparePurge runs every check that can refuse a purge (target, ownership,
// unexpected entries, space for the backup) without changing anything.
func preparePurge(
	home, userHome string,
	force bool,
	freeSpace func(string) (uint64, error),
	now time.Time,
) (*purgePlan, error) {
	if err := unsafePurgeTarget(home, userHome); err != nil {
		return nil, err
	}
	home = filepath.Clean(home)

	info, err := os.Lstat(home)
	if err != nil {
		return nil, fmt.Errorf("data directory: %w", err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, fmt.Errorf("refusing to delete %q: it is a symlink; remove it and its target by hand", home)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("refusing to delete %q: not a directory", home)
	}

	unexpected, err := checkKuromatsuHome(os.DirFS(home), force)
	if err != nil {
		return nil, fmt.Errorf("refusing to delete %q: %w", home, err)
	}

	size, err := dirSize(home)
	if err != nil {
		return nil, fmt.Errorf("measure data directory: %w", err)
	}

	backupDir := userHome
	if backupDir == "" || within(filepath.Clean(backupDir), home) {
		backupDir = os.TempDir()
	}
	free, err := freeSpace(backupDir)
	if err != nil {
		return nil, fmt.Errorf("check free space in %s: %w", backupDir, err)
	}
	need := uint64(size) + uint64(size)/10
	if free < need {
		return nil, fmt.Errorf("not enough free space for the backup in %s: need %s, available %s",
			backupDir, formatBytes(need), formatBytes(free))
	}

	return &purgePlan{
		home:       home,
		backup:     filepath.Join(backupDir, "kuromatsu-backup-"+now.Format("20060102-150405")+".zip"),
		unexpected: unexpected,
	}, nil
}

// execute writes the backup and, only once it is closed without error,
// deletes the data directory.
func (p *purgePlan) execute(out io.Writer) error {
	links, err := zipDir(p.home, p.backup)
	if err != nil {
		return fmt.Errorf("backup failed, nothing deleted: %w", err)
	}
	if len(links) > 0 {
		fmt.Fprintf(out, "Warning: symlinks were saved as links, not followed: %s\n", strings.Join(links, ", "))
	}
	if err = os.RemoveAll(p.home); err != nil {
		return fmt.Errorf("delete %s (backup kept at %s): %w", p.home, p.backup, err)
	}
	fmt.Fprintf(out, "Deleted %s\nBackup: %s\n", p.home, p.backup)
	if key, keyErr := credential.DefaultSSHKeyPath(); keyErr == nil {
		if _, statErr := os.Stat(key); statErr == nil {
			fmt.Fprintf(out, "Note: the credential encryption key %s was NOT deleted.\n", key)
		}
	}
	return nil
}

// dirSize sums the sizes of everything under root without following symlinks.
func dirSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}

func formatBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
