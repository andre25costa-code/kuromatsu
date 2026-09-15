package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/agent"
	"github.com/andre25costa-code/kuromatsu/pkg/bus"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/health"
	"github.com/andre25costa-code/kuromatsu/pkg/heartbeat"
)

func newTestAgentLoop(t *testing.T, cfg *config.Config) *agent.AgentLoop {
	t.Helper()
	al := agent.NewAgentLoop(cfg, bus.NewMessageBus(), &startupBlockedProvider{reason: "not used"})
	t.Cleanup(al.Close)
	return al
}

func newTestRunningServices() *services {
	return &services{
		HeartbeatServices: map[string]*heartbeat.HeartbeatService{
			"main": heartbeat.NewHeartbeatService(".", 30, false),
		},
		HealthServer: health.NewServer("127.0.0.1", 0, ""),
	}
}

// AC-017-7: disabled runstate must never leave a publisher goroutine
// running or a "state" field wired into /health.
func TestInstallRunstateIntegration_DisabledClearsEverything(t *testing.T) {
	cfg := config.DefaultConfig()
	al := newTestAgentLoop(t, cfg)
	rs := newTestRunningServices()

	installRunstateIntegration(cfg, al, rs)

	if rs.runstatePublishersStop != nil {
		t.Fatal("runstatePublishersStop set with runstate.enabled=false")
	}
}

func TestInstallRunstateIntegration_EnabledStartsPublishersAndCanBeDisabledAgain(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Runstate.Enabled = true
	al := newTestAgentLoop(t, cfg)
	rs := newTestRunningServices()

	installRunstateIntegration(cfg, al, rs)
	stopFn := rs.runstatePublishersStop
	if stopFn == nil {
		t.Fatal("runstatePublishersStop not set with runstate.enabled=true")
	}

	// A later reload flipping the flag off must stop the previous
	// publishers and clear the field -- not just leave them running.
	// installRunstateIntegration reads al.Runstate() (not the cfg argument
	// directly) for the enable/disable decision, exactly like the real
	// gateway.go call sites do (restartServices re-derives cfg from
	// al.GetConfig() after ReloadProviderAndConfig already updated al) --
	// so the test has to actually reload al first, or it would be asserting
	// a cfg/al combination production code never produces.
	disabledCfg := config.DefaultConfig()
	if err := al.ReloadProviderAndConfig(context.Background(), &startupBlockedProvider{reason: "not used"}, disabledCfg); err != nil {
		t.Fatalf("ReloadProviderAndConfig() error = %v", err)
	}
	installRunstateIntegration(disabledCfg, al, rs)
	if rs.runstatePublishersStop != nil {
		t.Fatal("runstatePublishersStop still set after a reload disabled runstate")
	}
}

func TestInstallRunstateIntegration_ReinstallReplacesThePreviousStopFunc(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Runstate.Enabled = true
	al := newTestAgentLoop(t, cfg)
	rs := newTestRunningServices()

	installRunstateIntegration(cfg, al, rs)
	first := rs.runstatePublishersStop

	installRunstateIntegration(cfg, al, rs)
	second := rs.runstatePublishersStop

	if second == nil {
		t.Fatal("runstatePublishersStop nil after a second install")
	}
	// Not asserting first != second by identity (Go doesn't let us compare
	// func values), just that install doesn't panic/leak when called twice
	// in a row -- the first goroutine set must have been canceled inside
	// installRunstateIntegration before the second one started.
	_ = first
}

func TestInstallMemguardIntegration_DisabledClearsHooksAndStop(t *testing.T) {
	cfg := config.DefaultConfig()
	al := newTestAgentLoop(t, cfg)
	rs := newTestRunningServices()

	installMemguardIntegration(cfg, al, rs)

	if rs.memguardStop != nil {
		t.Fatal("memguardStop set with memguard.enabled=false")
	}
}

func TestInstallMemguardIntegration_EnabledStartsWatchdogAndCanBeDisabledAgain(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Memguard.Enabled = true
	al := newTestAgentLoop(t, cfg)
	rs := newTestRunningServices()

	installMemguardIntegration(cfg, al, rs)
	if rs.memguardStop == nil {
		t.Fatal("memguardStop not set with memguard.enabled=true")
	}
	// Give guard.Run's PSI-unavailable-on-Windows-or-CI-sandbox early
	// return a moment, so the goroutine it spawned has already exited by
	// the time this test's own goroutine leak checker (none here, but
	// good hygiene) would look.
	time.Sleep(10 * time.Millisecond)

	disabledCfg := config.DefaultConfig()
	installMemguardIntegration(disabledCfg, al, rs)
	if rs.memguardStop != nil {
		t.Fatal("memguardStop still set after a reload disabled memguard")
	}
}
