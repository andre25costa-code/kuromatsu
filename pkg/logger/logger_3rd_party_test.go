package logger

import "testing"

func TestMaskSecrets_URLUserinfo(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"user and password", "http://alice:s3cr3t@proxy.local:8080", "http://***@proxy.local:8080"},
		{"user only", "socks5://u@h:1080", "socks5://***@h:1080"},
		{"no credentials", "http://proxy.local:8080", "http://proxy.local:8080"},
		{"password containing at sign", "http://u:p@ss@host:8080", "http://***@host:8080"},
		{
			"embedded in text",
			"proxy http://alice:s3cr3t@proxy.local:8080 failed",
			"proxy http://***@proxy.local:8080 failed",
		},
		{"at sign in path is kept", "http://host/path@x", "http://host/path@x"},
		{"bot token", "bot123456:ABCDEFGHIJKLMNOPQRSTUVWX", "bot123456:ABCD****UVWX"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := MaskSecrets(tc.in); got != tc.want {
				t.Errorf("MaskSecrets(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
