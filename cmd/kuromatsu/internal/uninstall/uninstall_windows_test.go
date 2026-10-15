//go:build windows

package uninstall

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// An 8.3 short name is another name for the user's home; it must be refused.
func TestPurgeHome_RefusesUserHomeByShortName(t *testing.T) {
	userHome := userDirs(t)
	// A long name gets a ~1 short name when 8.3 names are enabled.
	userHome = filepath.Join(userHome, "a long user home name")
	writeFile(t, filepath.Join(userHome, "config.json"), "{}")
	t.Setenv("USERPROFILE", userHome)

	in, err := windows.UTF16PtrFromString(userHome)
	require.NoError(t, err)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetShortPathName(in, &buf[0], uint32(len(buf)))
	require.NoError(t, err)
	short := windows.UTF16ToString(buf[:n])
	if strings.EqualFold(short, userHome) {
		t.Skip("8.3 short names are disabled on this volume")
	}

	_, err = preparePurge(short, userHome, true, plentyOfSpace, testNow)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "home directory")
}
