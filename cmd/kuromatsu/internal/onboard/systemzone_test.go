package onboard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectSystemZone(t *testing.T) {
	t.Run("localtime symlink", func(t *testing.T) {
		root := withZoneFiles(t, "")
		require.NoError(t, os.MkdirAll(filepath.Join(root, "etc"), 0o755))
		if err := os.Symlink("/usr/share/zoneinfo/Europe/Lisbon", filepath.Join(root, "etc", "localtime")); err != nil {
			t.Skipf("symlinks unavailable here: %v", err)
		}
		assert.Equal(t, "Europe/Lisbon", detectSystemZone())
	})
	t.Run("symlink wins over etc/timezone", func(t *testing.T) {
		root := withZoneFiles(t, "Asia/Tokyo")
		if err := os.Symlink("../var/db/timezone/zoneinfo/America/New_York",
			filepath.Join(root, "etc", "localtime")); err != nil {
			t.Skipf("symlinks unavailable here: %v", err)
		}
		assert.Equal(t, "America/New_York", detectSystemZone())
	})
	t.Run("etc/timezone", func(t *testing.T) {
		withZoneFiles(t, "Asia/Tokyo")
		assert.Equal(t, "Asia/Tokyo", detectSystemZone())
	})
	t.Run("unknown names are not a zone", func(t *testing.T) {
		withZoneFiles(t, "Mars/Olympus")
		assert.Empty(t, detectSystemZone())
		withZoneFiles(t, "Local")
		assert.Empty(t, detectSystemZone())
	})
	t.Run("no zone files", func(t *testing.T) {
		withZoneFiles(t, "")
		assert.Empty(t, detectSystemZone())
	})
	t.Run("windows is undetected", func(t *testing.T) {
		withZoneFiles(t, "Asia/Tokyo")
		zoneGOOS = "windows"
		assert.Empty(t, detectSystemZone())
	})
}

func TestDefaultTimezone_TZFirst(t *testing.T) {
	withZoneFiles(t, "Asia/Tokyo")
	t.Setenv("TZ", "Europe/Lisbon")
	assert.Equal(t, "Europe/Lisbon", defaultTimezone())
	t.Setenv("TZ", "Not/AZone")
	assert.Equal(t, "Asia/Tokyo", defaultTimezone())
}
