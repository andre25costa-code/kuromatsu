package onboard

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Seams for tests: the filesystem root and the OS the system timezone is
// detected from.
var (
	zoneRoot = string(filepath.Separator)
	zoneGOOS = runtime.GOOS
)

// detectSystemZone returns the IANA name of the host's timezone, or "" when
// it cannot be told. Go names the system zone "Local", so the name is read
// from the files the system itself uses: the target of the /etc/localtime
// symlink after "zoneinfo/" (Linux, macOS), then /etc/timezone (Debian
// family). Windows has no such files and no IANA name to read, so its zone
// counts as undetected.
func detectSystemZone() string {
	if zoneGOOS == "windows" {
		return ""
	}
	if target, err := os.Readlink(filepath.Join(zoneRoot, "etc", "localtime")); err == nil {
		target = filepath.ToSlash(target)
		if i := strings.LastIndex(target, "zoneinfo/"); i >= 0 {
			if name := target[i+len("zoneinfo/"):]; knownZone(name) {
				return name
			}
		}
	}
	if data, err := os.ReadFile(filepath.Join(zoneRoot, "etc", "timezone")); err == nil {
		name, _, _ := strings.Cut(string(data), "\n")
		if name = strings.TrimSpace(name); knownZone(name) {
			return name
		}
	}
	return ""
}

// knownZone reports whether name is an IANA zone time.LoadLocation knows
// (and not Go's "Local" placeholder).
func knownZone(name string) bool {
	if name == "" || name == "Local" {
		return false
	}
	_, err := time.LoadLocation(name)
	return err == nil
}

// defaultTimezone is $TZ when it names a known zone, else the detected
// system zone, else "" (keep the system zone without naming it).
func defaultTimezone() string {
	if tz := os.Getenv("TZ"); knownZone(tz) {
		return tz
	}
	return detectSystemZone()
}
