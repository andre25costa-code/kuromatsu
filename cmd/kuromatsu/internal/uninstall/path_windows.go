//go:build windows

package uninstall

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// writable is always true on Windows: whether the binary can be moved is
// only known by trying the rename in removeBinary.
func writable(string) bool { return true }

// uninstalledSuffix marks a binary that was renamed because Windows does not
// delete a running executable. The installer and the next uninstall delete
// these leftovers.
const uninstalledSuffix = ".uninstalled-"

// removeBinary renames the running executable out of the way and deletes
// leftovers from earlier runs.
func removeBinary(exe string, now time.Time) error {
	old, _ := filepath.Glob(exe + uninstalledSuffix + "*")
	for _, p := range old {
		_ = os.Remove(p)
	}
	return os.Rename(exe, exe+uninstalledSuffix+strconv.FormatInt(now.Unix(), 10))
}

// removeFromPath removes exeDir from the user's PATH in the registry when
// the installer added it (see removeOwnedPathEntry), keeping the other
// entries and the value type (REG_EXPAND_SZ stays REG_EXPAND_SZ, so %VARS%
// keep working).
func removeFromPath(exeDir, _ string, out io.Writer) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	changed, err := removeOwnedPathEntry(exeDir, registryPath{k}, expandEnv, out)
	if changed {
		broadcastEnvChange()
	}
	return err
}

// registryPath is the Path value of an open registry key.
type registryPath struct{ k registry.Key }

func (r registryPath) Read() (string, bool, bool, error) {
	val, typ, err := r.k.GetStringValue("Path")
	if errors.Is(err, registry.ErrNotExist) {
		return "", false, false, nil
	}
	if err != nil {
		return "", false, false, err
	}
	return val, typ == registry.EXPAND_SZ, true, nil
}

func (r registryPath) Write(value string, expand bool) error {
	if expand {
		return r.k.SetExpandStringValue("Path", value)
	}
	return r.k.SetStringValue("Path", value)
}

func expandEnv(s string) string {
	expanded, err := registry.ExpandString(s)
	if err != nil {
		return s
	}
	return expanded
}

// broadcastEnvChange tells running programs (Explorer, new terminals) that
// the environment changed. Failing here is harmless: the change applies at
// the next logon.
func broadcastEnvChange() {
	const (
		hwndBroadcast   = 0xffff
		wmSettingChange = 0x001A
		smtoAbortIfHung = 0x0002
	)
	env, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	proc := windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")
	if proc.Find() != nil {
		return
	}
	var result uintptr
	_, _, _ = proc.Call(hwndBroadcast, wmSettingChange, 0,
		uintptr(unsafe.Pointer(env)), smtoAbortIfHung, 5000, uintptr(unsafe.Pointer(&result)))
}

// longPath expands 8.3 short names (C:\Users\ANDREC~1) so the same
// directory always compares equal. On failure it returns p unchanged; the
// file-identity check in containsDir still applies.
func longPath(p string) string {
	in, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return p
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetLongPathName(in, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) > len(buf) {
		return p
	}
	return windows.UTF16ToString(buf[:n])
}
