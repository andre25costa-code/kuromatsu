package uninstall

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testNow = time.Date(2026, 10, 15, 13, 4, 5, 0, time.UTC)

func plentyOfSpace(string) (uint64, error) { return 1 << 40, nil }

// newKuromatsuHome creates <base>/.kuromatsu with the files a real home has.
func newKuromatsuHome(t *testing.T, base string) string {
	t.Helper()
	home := filepath.Join(base, ".kuromatsu")
	writeFile(t, filepath.Join(home, "config.json"), `{"version":1}`)
	writeFile(t, filepath.Join(home, ".security.yml"), "model_list: {}\n")
	writeFile(t, filepath.Join(home, "workspace", "AGENT.md"), "# agent\n")
	return home
}

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

// userDirs returns a fresh user home and points HOME/USERPROFILE at it.
func userDirs(t *testing.T) string {
	t.Helper()
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	t.Setenv("USERPROFILE", userHome)
	return userHome
}

// testEnv builds an env over temp directories; exe is a fake installed
// binary inside userHome/.local/bin.
func testEnv(t *testing.T, userHome, home string) (*env, string, *bytes.Buffer) {
	t.Helper()
	exe := filepath.Join(userHome, ".local", "bin", "kuromatsu")
	writeFile(t, exe, "binary")
	out := &bytes.Buffer{}
	e := &env{
		stdin:          strings.NewReader(""),
		stdout:         out,
		home:           home,
		userHome:       userHome,
		executable:     func() (string, error) { return exe, nil },
		isTerminal:     func() bool { return false },
		runningGateway: func(string) (int, bool) { return 0, false },
		freeSpace:      plentyOfSpace,
		writable:       func(string) bool { return true },
		removeBinary:   func(p string, _ time.Time) error { return os.Remove(p) },
		updatePath: func(_, uh string, w io.Writer) error {
			// The registry is never touched in tests; the Unix profile
			// logic is pure file editing and runs for real.
			if runtime.GOOS == "windows" {
				return nil
			}
			return removeFromPath("", uh, w)
		},
		now: func() time.Time { return testNow },
	}
	return e, exe, out
}

func backupPath(t *testing.T, userHome string) string {
	t.Helper()
	resolved, err := realPath(userHome)
	require.NoError(t, err)
	return filepath.Join(resolved, "kuromatsu-backup-20261015-130405.zip")
}

func readZip(t *testing.T, p string) map[string]*zip.File {
	t.Helper()
	zr, err := zip.OpenReader(p)
	require.NoError(t, err)
	t.Cleanup(func() { zr.Close() })
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	return files
}

func zipContent(t *testing.T, f *zip.File) string {
	t.Helper()
	r, err := f.Open()
	require.NoError(t, err)
	defer r.Close()
	b, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(b)
}

func TestPurgeHome_BacksUpThenRemoves(t *testing.T) {
	userHome := userDirs(t)
	home := newKuromatsuHome(t, userHome)
	writeFile(t, filepath.Join(home, "logs", "gateway.log"), "log\n")
	writeFile(t, filepath.Join(home, "config.json.20261001.bak"), "{}")

	plan, err := preparePurge(home, userHome, false, plentyOfSpace, testNow)
	require.NoError(t, err)
	assert.Equal(t, backupPath(t, userHome), plan.backup)

	var out bytes.Buffer
	require.NoError(t, plan.writeBackup(&out))
	require.NoError(t, plan.removeData(&out))

	_, err = os.Stat(home)
	assert.True(t, errors.Is(err, fs.ErrNotExist), "data directory must be deleted")

	info, err := os.Stat(plan.backup)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	files := readZip(t, plan.backup)
	require.Contains(t, files, "config.json")
	require.Contains(t, files, ".security.yml")
	require.Contains(t, files, "workspace/AGENT.md")
	assert.Equal(t, `{"version":1}`, zipContent(t, files["config.json"]))
	assert.Equal(t, "# agent\n", zipContent(t, files["workspace/AGENT.md"]))
	assert.Contains(t, out.String(), plan.backup)
}

func TestPurgeHome_RefusesAncestorOfUserHome(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "data", "team")
	writeFile(t, filepath.Join(home, "config.json"), "{}")
	userHome := filepath.Join(home, "workspace") // a known entry
	writeFile(t, filepath.Join(userHome, "thesis.odt"), "years of work")

	for _, force := range []bool{false, true} {
		_, err := preparePurge(home, userHome, force, plentyOfSpace, testNow)
		require.Error(t, err, "force=%v", force)
		assert.Contains(t, err.Error(), "your home directory")
	}
	assert.FileExists(t, filepath.Join(userHome, "thesis.odt"))
}

// A symlink in a parent component makes the data directory another name
// for the user's home (or an ancestor of it); both must be refused.
func TestPurgeHome_RefusesUserHomeThroughSymlinkedParent(t *testing.T) {
	userHome := userDirs(t)
	writeFile(t, filepath.Join(userHome, "config.json"), "{}")
	writeFile(t, filepath.Join(userHome, "thesis.odt"), "years of work")

	alias := filepath.Join(t.TempDir(), "plink")
	if err := os.Symlink(filepath.Dir(userHome), alias); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
	sameDir := filepath.Join(alias, filepath.Base(userHome))
	require.NotEqual(t, userHome, sameDir)

	// The user's home's parent, reached through a link to its grandparent,
	// is an ancestor of the user's home whose last component is real.
	parent := filepath.Dir(userHome)
	alias2 := filepath.Join(t.TempDir(), "gplink")
	require.NoError(t, os.Symlink(filepath.Dir(parent), alias2))
	ancestor := filepath.Join(alias2, filepath.Base(parent))
	writeFile(t, filepath.Join(parent, "config.json"), "{}")

	for _, target := range []string{sameDir, ancestor} {
		_, err := preparePurge(target, userHome, true, plentyOfSpace, testNow)
		require.Error(t, err, target)
		assert.Contains(t, err.Error(), "home directory", target)
	}
	assert.FileExists(t, filepath.Join(userHome, "thesis.odt"))
}

func TestPurgeHome_RefusesDangerousTargets(t *testing.T) {
	userHome := userDirs(t)
	// The user's home looks like a Kuromatsu home: still refused.
	writeFile(t, filepath.Join(userHome, "config.json"), "{}")

	targets := []string{"/", `C:\`, "C:/", "c:", userHome, ".", "", "relative/dir", "/home"}
	if runtime.GOOS == "windows" {
		targets = append(targets, `D:\`, filepath.VolumeName(userHome)+`\`)
	}
	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			for _, force := range []bool{false, true} {
				_, err := preparePurge(target, userHome, force, plentyOfSpace, testNow)
				require.Error(t, err, "force=%v", force)
				assert.Contains(t, err.Error(), "refusing to delete")
			}
		})
	}
	_, err := os.Stat(filepath.Join(userHome, "config.json"))
	require.NoError(t, err, "nothing may be touched")
}

func TestPurgeHome_RefusesForeignDirectory(t *testing.T) {
	userHome := userDirs(t)
	docs := filepath.Join(userHome, "Documents")
	writeFile(t, filepath.Join(docs, "thesis.odt"), "years of work")

	for _, force := range []bool{false, true} {
		_, err := preparePurge(docs, userHome, force, plentyOfSpace, testNow)
		require.Error(t, err, "force=%v", force)
		assert.ErrorIs(t, err, errNotKuromatsuHome)
	}
	_, err := os.Stat(filepath.Join(docs, "thesis.odt"))
	require.NoError(t, err)
}

func TestPurgeHome_UnexpectedEntriesNeedForce(t *testing.T) {
	userHome := userDirs(t)
	home := newKuromatsuHome(t, userHome)
	// Entries Kuromatsu itself creates are never unexpected.
	for _, known := range []string{
		"logs/gateway.log", "cache/x", "runtime-user-env/home/x", "run/state", "telemetry/turns.db",
		"models/m.gguf", "skills/s/SKILL.md", "state/s", "auth.json", ".kuromatsu.pid",
		"config.json.20261001.bak", ".security.yml.20261001.bak", ".tmp-1-2",
		"kuromatsu-backup-20260101-000000.zip",
	} {
		writeFile(t, filepath.Join(home, filepath.FromSlash(known)), "x")
	}
	writeFile(t, filepath.Join(home, "notes.txt"), "mine")
	writeFile(t, filepath.Join(home, "photos", "a.jpg"), "mine")

	_, err := preparePurge(home, userHome, false, plentyOfSpace, testNow)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected entries: notes.txt, photos; re-run with --force")

	plan, err := preparePurge(home, userHome, true, plentyOfSpace, testNow)
	require.NoError(t, err)
	assert.Equal(t, []string{"notes.txt", "photos"}, plan.unexpected)
}

func TestPurgeHome_InsufficientSpace(t *testing.T) {
	userHome := userDirs(t)
	home := newKuromatsuHome(t, userHome)
	writeFile(t, filepath.Join(home, "models", "m.gguf"), strings.Repeat("x", 1000))

	var asked string
	tight := func(dir string) (uint64, error) { asked = dir; return 1000, nil }
	_, err := preparePurge(home, userHome, false, tight, testNow)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not enough free space")
	assert.Contains(t, err.Error(), "available 1000 B")
	assert.Equal(t, userHome, asked, "free space is checked where the backup goes")

	_, err = os.Stat(filepath.Join(home, "config.json"))
	require.NoError(t, err)
}

func TestZipDir_DoesNotFollowSymlinks(t *testing.T) {
	userHome := userDirs(t)
	home := newKuromatsuHome(t, userHome)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	writeFile(t, outside, "TOPSECRET")
	link := filepath.Join(home, "workspace", "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}

	plan, err := preparePurge(home, userHome, false, plentyOfSpace, testNow)
	require.NoError(t, err)
	var out bytes.Buffer
	require.NoError(t, plan.writeBackup(&out))
	require.NoError(t, plan.removeData(&out))
	assert.Contains(t, out.String(), "symlinks were saved as links")

	files := readZip(t, plan.backup)
	f := files["workspace/link"]
	require.NotNil(t, f)
	assert.NotZero(t, f.Mode()&fs.ModeSymlink, "stored as a link entry")
	assert.Equal(t, outside, zipContent(t, f), "content is the link target, not the file")

	data, err := os.ReadFile(outside)
	require.NoError(t, err)
	assert.Equal(t, "TOPSECRET", string(data), "the link target survives the purge")
}

func TestZipDir_NeverOverwritesABackup(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a"), "a")
	dst := filepath.Join(t.TempDir(), "b.zip")
	writeFile(t, dst, "exists")

	_, err := zipDir(src, dst)
	require.Error(t, err, "an existing backup is never overwritten")
	data, _ := os.ReadFile(dst)
	assert.Equal(t, "exists", string(data))
}

func TestUninstall_RefusesWhenGatewayRunning(t *testing.T) {
	userHome := userDirs(t)
	home := newKuromatsuHome(t, userHome)
	e, exe, _ := testEnv(t, userHome, home)
	e.runningGateway = func(string) (int, bool) { return 4242, true }

	err := run(options{purge: true, yes: true}, e)
	require.Error(t, err)
	assert.Equal(t, "gateway is running (PID 4242); stop it first", err.Error())
	assert.FileExists(t, exe)
	assert.DirExists(t, home)
}

func TestUninstall_NonTTYRequiresYes(t *testing.T) {
	userHome := userDirs(t)
	home := newKuromatsuHome(t, userHome)
	profile := filepath.Join(userHome, ".profile")
	writeFile(t, profile, "export PATH=\"/x:$PATH\" "+installerMarker+"\n")
	e, exe, _ := testEnv(t, userHome, home)

	err := run(options{purge: true}, e)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--yes")
	assert.FileExists(t, exe)
	assert.DirExists(t, home)
	data, _ := os.ReadFile(profile)
	assert.Contains(t, string(data), installerMarker)
}

func TestUninstall_PromptDeclinedChangesNothing(t *testing.T) {
	userHome := userDirs(t)
	home := newKuromatsuHome(t, userHome)
	e, exe, out := testEnv(t, userHome, home)
	e.isTerminal = func() bool { return true }
	e.stdin = strings.NewReader("n\n")

	require.NoError(t, run(options{purge: true}, e))
	assert.Contains(t, out.String(), exe, "prompt shows the binary's absolute path")
	assert.Contains(t, out.String(), home, "prompt shows the data directory's absolute path")
	assert.Contains(t, out.String(), "Aborted")
	assert.FileExists(t, exe)
	assert.DirExists(t, home)
}

func TestUninstall_YesRemovesBinaryPathLinesAndData(t *testing.T) {
	userHome := userDirs(t)
	home := newKuromatsuHome(t, userHome)
	profile := filepath.Join(userHome, ".profile")
	writeFile(t, profile, "umask 022\nexport PATH=\"/x:$PATH\" "+installerMarker+"\n")
	e, exe, out := testEnv(t, userHome, home)
	e.homeFromEnv = true

	require.NoError(t, run(options{purge: true, yes: true}, e))
	assert.NoFileExists(t, exe)
	assert.NoDirExists(t, home)
	assert.FileExists(t, backupPath(t, userHome))
	assert.Contains(t, out.String(), "Data directory (from KUROMATSU_HOME): "+home)
	if runtime.GOOS != "windows" {
		data, _ := os.ReadFile(profile)
		assert.Equal(t, "umask 022\n", string(data))
	}
}

func TestUninstall_WithoutPurgeKeepsData(t *testing.T) {
	userHome := userDirs(t)
	home := newKuromatsuHome(t, userHome)
	e, exe, out := testEnv(t, userHome, home)

	require.NoError(t, run(options{yes: true}, e))
	assert.NoFileExists(t, exe)
	assert.DirExists(t, home)
	assert.Contains(t, out.String(), "Data directory kept: "+home)
}

func TestUninstall_PackageManagedKeepsBinary(t *testing.T) {
	userHome := userDirs(t)
	home := newKuromatsuHome(t, userHome)
	e, exe, out := testEnv(t, userHome, home)
	e.writable = func(string) bool { return false }

	require.NoError(t, run(options{yes: true}, e))
	assert.FileExists(t, exe)
	assert.Contains(t, out.String(), "installed by a package manager or system-wide")
}

func TestPackageManaged_Table(t *testing.T) {
	userBin := filepath.Join(t.TempDir(), ".local", "bin")
	readOnly := filepath.Join(t.TempDir(), "ro")
	writableDirs := func(d string) bool { return d != readOnly }

	for _, tc := range []struct {
		dir     string
		managed bool
	}{
		{"/usr/bin", true},
		{"/usr/local/bin", true},
		{"/bin", true},
		{"/sbin", true},
		{"/opt/kuromatsu/bin", true},
		{"/snap/bin", true},
		{readOnly, true},
		{userBin, false},
	} {
		assert.Equal(t, tc.managed, packageManaged(tc.dir, writableDirs), tc.dir)
	}
}

func TestStripInstallerLines(t *testing.T) {
	in := "# ~/.profile\r\n" +
		"export PATH=\"$HOME/.local/bin:$PATH\" " + installerMarker + "\n" +
		"alias ll='ls -l'  \n" +
		"export PATH=\"/a:$PATH\" " + installerMarker + "\r\n" +
		"# mentions " + installerMarker + " in the middle, then more\n" +
		"no trailing newline"
	want := "# ~/.profile\r\n" +
		"alias ll='ls -l'  \n" +
		"# mentions " + installerMarker + " in the middle, then more\n" +
		"no trailing newline"

	got, changed := stripInstallerLines(in)
	assert.True(t, changed)
	assert.Equal(t, want, got)

	again, changed := stripInstallerLines(got)
	assert.False(t, changed)
	assert.Equal(t, got, again)

	last := "a\nexport PATH=x " + installerMarker
	got, changed = stripInstallerLines(last)
	assert.True(t, changed)
	assert.Equal(t, "a\n", got)
}

func TestRemoveFromUserPath_PreservesUnexpandedEntries(t *testing.T) {
	vars := map[string]string{
		"%USERPROFILE%":  `C:\Users\u`,
		"%LOCALAPPDATA%": `C:\Users\u\AppData\Local`,
	}
	expand := func(s string) string {
		for k, v := range vars {
			s = strings.ReplaceAll(s, k, v)
		}
		return s
	}
	dir := `C:\Users\u\AppData\Local\Kuromatsu\bin`
	raw := `%USERPROFILE%\go\bin;C:\Windows;%LOCALAPPDATA%\Kuromatsu\bin;;D:\tools\;c:\users\U\appdata\local\kuromatsu\BIN\`

	got, changed := removeFromUserPath(raw, dir, expand)
	assert.True(t, changed)
	assert.Equal(t, `%USERPROFILE%\go\bin;C:\Windows;;D:\tools\`, got)

	same, changed := removeFromUserPath(got, dir, expand)
	assert.False(t, changed)
	assert.Equal(t, got, same)
}

func TestNewUninstallCommand(t *testing.T) {
	cmd := NewUninstallCommand()
	assert.Equal(t, "uninstall", cmd.Use)
	for _, name := range []string{"purge", "force", "yes"} {
		f := cmd.Flags().Lookup(name)
		require.NotNil(t, f, name)
		assert.Equal(t, "false", f.DefValue, name)
	}
}

func TestUninstall_BackupFailureChangesNothing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes do not make a file unreadable on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can read a 0000 file")
	}
	userHome := userDirs(t)
	home := newKuromatsuHome(t, userHome)
	locked := filepath.Join(home, "state", "locked")
	writeFile(t, locked, "x")
	require.NoError(t, os.Chmod(locked, 0))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o600) })
	profile := filepath.Join(userHome, ".profile")
	line := "export PATH=\"/x:$PATH\" " + installerMarker + "\n"
	writeFile(t, profile, line)
	e, exe, _ := testEnv(t, userHome, home)

	err := run(options{purge: true, yes: true}, e)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backup failed; nothing was changed")
	assert.FileExists(t, exe, "binary kept")
	assert.DirExists(t, home, "data kept")
	data, _ := os.ReadFile(profile)
	assert.Equal(t, line, string(data), "PATH line kept")
	assert.NoFileExists(t, backupPath(t, userHome), "partial zip removed")
}

// fakePath is an in-memory PATH value.
type fakePath struct {
	value  string
	expand bool
	found  bool
	writes int
}

func (f *fakePath) Read() (string, bool, bool, error) { return f.value, f.expand, f.found, nil }

func (f *fakePath) Write(v string, expand bool) error {
	f.value, f.expand = v, expand
	f.writes++
	return nil
}

func TestRemoveOwnedPathEntry(t *testing.T) {
	expand := func(s string) string { return strings.ReplaceAll(s, "%USERPROFILE%", `C:\Users\u`) }

	t.Run("entry the installer did not add stays", func(t *testing.T) {
		// e.g. `go install` put kuromatsu.exe in %USERPROFILE%\go\bin.
		dir := t.TempDir()
		store := &fakePath{value: `%USERPROFILE%\go\bin;` + dir, expand: true, found: true}
		var out bytes.Buffer
		changed, err := removeOwnedPathEntry(dir, store, expand, &out)
		require.NoError(t, err)
		assert.False(t, changed)
		assert.Zero(t, store.writes)
		assert.Contains(t, out.String(), "left unchanged")
	})

	t.Run("installer entry removed, type kept, marker deleted", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, pathMarker), dir)
		store := &fakePath{value: `%USERPROFILE%\go\bin;` + dir, expand: true, found: true}
		changed, err := removeOwnedPathEntry(dir, store, expand, io.Discard)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, `%USERPROFILE%\go\bin`, store.value)
		assert.True(t, store.expand, "REG_EXPAND_SZ stays REG_EXPAND_SZ")
		assert.NoFileExists(t, filepath.Join(dir, pathMarker))
	})

	t.Run("REG_SZ stays REG_SZ", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, pathMarker), dir)
		store := &fakePath{value: `C:\a;` + dir, found: true}
		changed, err := removeOwnedPathEntry(dir, store, expand, io.Discard)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, `C:\a`, store.value)
		assert.False(t, store.expand)
	})

	t.Run("no Path value", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, pathMarker), dir)
		store := &fakePath{}
		changed, err := removeOwnedPathEntry(dir, store, expand, io.Discard)
		require.NoError(t, err)
		assert.False(t, changed)
		assert.Zero(t, store.writes)
	})
}
