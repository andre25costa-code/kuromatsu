package main

import (
	"go/build"
	"testing"
)

// N10 (audit round 2): the DNS override triggers when /etc/resolv.conf is
// missing, which is meant for Android/Termux -- but Windows never has that
// file, so every Windows build replaced the system resolver (corporate DNS,
// VPN, intranet names) with 8.8.8.8/1.1.1.1. The file must not be part of a
// Windows build; Linux and Android keep it.
func TestDNSNoResolvOverrideIsNotBuiltOnWindows(t *testing.T) {
	for goos, want := range map[string]bool{"windows": false, "linux": true, "android": true} {
		ctxt := build.Default
		ctxt.GOOS = goos
		got, err := ctxt.MatchFile(".", "dns_noresolv.go")
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("GOOS=%s: dns_noresolv.go built = %v, want %v", goos, got, want)
		}
	}
}
