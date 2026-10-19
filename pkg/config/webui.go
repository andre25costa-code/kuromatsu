package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// WebUIConfig is the local configuration page the gateway serves at /ui.
// It answers only on loopback and only with the access token, which lives in
// the security file and is created by `kuromatsu onboard` or
// `kuromatsu ui token`, never by the gateway. Changing Enabled, AllowRemote
// or the token takes effect on the next gateway start.
//
//nolint:recvcheck // IsZero needs a value receiver (json omitzero); EnsureToken mutates.
type WebUIConfig struct {
	Enabled bool `json:"enabled" yaml:"-" env:"KUROMATSU_WEBUI_ENABLED"`
	// AllowRemote lets clients other than loopback reach the page over plain
	// HTTP. Off by default; an SSH tunnel is the safer way in.
	AllowRemote bool `json:"allow_remote" yaml:"-" env:"KUROMATSU_WEBUI_ALLOW_REMOTE"`
	// Token is the access token (64 hex characters). It is written to the
	// security file only.
	Token SecureString `json:"token,omitzero" yaml:"token,omitempty" env:"KUROMATSU_WEBUI_TOKEN"`
}

// webUITokenBytes is the number of random bytes in a web UI token.
const webUITokenBytes = 32

// EnsureToken sets a new random token when none is set and reports whether
// it did. An existing token is never replaced. It only changes c; the caller
// saves.
func (c *WebUIConfig) EnsureToken() (bool, error) {
	if c.Token.String() != "" {
		return false, nil
	}
	buf := make([]byte, webUITokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return false, fmt.Errorf("generate web UI token: %w", err)
	}
	c.Token = *NewSecureString(hex.EncodeToString(buf))
	return true, nil
}

// IsZero reports whether c holds the defaults (enabled, not remote), which
// json "omitzero" then leaves out of config.json. The token is not part of
// it: it never goes to config.json. An explicit "enabled": false is not
// zero, so it is written and survives a reload.
func (c WebUIConfig) IsZero() bool {
	return c.Enabled && !c.AllowRemote
}
