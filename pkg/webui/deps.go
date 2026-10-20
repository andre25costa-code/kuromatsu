// Package webui serves the local configuration page of the gateway: static
// files at /ui/ and a JSON API at /api/ui/. It answers only on loopback (unless
// webui.allow_remote is set), only with the access token from the security
// file, and checks Origin and a CSRF token on every state-changing call
// authenticated by the session cookie.
//
// The package depends on the gateway only through Deps, so it never imports
// pkg/gateway or pkg/agent.
package webui

import (
	"context"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/cron"
	"github.com/andre25costa-code/kuromatsu/pkg/sysinfo"
)

// Deps is what the web UI needs from the running gateway. Getters are read
// on every request because a reload replaces what they return.
type Deps struct {
	// ConfigPath is the config file the gateway loaded; edits are written
	// there (and to its security file).
	ConfigPath string
	// Workspace returns the workspace directory (SOUL.md, skills/ ...).
	Workspace func() string
	// Runtime returns the running config (environment values applied).
	// It is only read: template requirements, timezone, status, skill
	// download settings. Nil means "load the file as stored".
	Runtime func() *config.Config
	// Cron returns the current scheduler, or nil when there is none (the UI
	// answers 503). In the gateway it is never nil: during a reload it is
	// the stopped old scheduler until the new one is set; writes then land
	// in jobs.json, which the new scheduler re-reads.
	Cron func() *cron.CronService
	// Reload applies a saved config, as POST /reload does.
	Reload func() error
	// QueryStats renders the telemetry report; it fails when telemetry is off.
	QueryStats func(ctx context.Context, window string, hours int) (string, error)
	// Hardware returns the host snapshot shown on the status screen.
	Hardware func() sysinfo.Hardware
	// Now is the clock (sessions, login rate limit, automations).
	Now func() time.Time

	// Token is the access token. New refuses to serve without one.
	Token string
	// AllowRemote turns off the loopback, proxy-header and Host checks.
	AllowRemote bool
}
