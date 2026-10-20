//go:build windows

package main

// Windows has no system zoneinfo database, so time.LoadLocation (the
// "timezone" config key, cron job zones) needs the embedded copy.
import _ "time/tzdata"
