package setup

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

func TestApplyPermissions_Table(t *testing.T) {
	t.Setenv(config.EnvHome, t.TempDir())

	type row struct {
		restrict, exec, remote, web, cron, cronCmd bool
	}
	want := map[Preset]row{
		PresetSafe:      {restrict: true},
		PresetAssistant: {restrict: true, web: true, cron: true},
		PresetAdvanced:  {restrict: true, exec: true, web: true, cron: true, cronCmd: true},
	}
	for p, w := range want {
		t.Run(string(p), func(t *testing.T) {
			// Start from the opposite of every expected value so each key
			// is proven to be written, not just left alone.
			cfg := config.DefaultConfig()
			cfg.Agents.Defaults.RestrictToWorkspace = !w.restrict
			cfg.Tools.Exec.Enabled = !w.exec
			cfg.Tools.Exec.AllowRemote = !w.remote
			cfg.Tools.Web.Enabled = !w.web
			cfg.Tools.Cron.Enabled = !w.cron
			cfg.Tools.Cron.AllowCommand = !w.cronCmd

			require.NoError(t, ApplyPermissions(cfg, p))
			got := row{
				restrict: cfg.Agents.Defaults.RestrictToWorkspace,
				exec:     cfg.Tools.Exec.Enabled,
				remote:   cfg.Tools.Exec.AllowRemote,
				web:      cfg.Tools.Web.Enabled,
				cron:     cfg.Tools.Cron.Enabled,
				cronCmd:  cfg.Tools.Cron.AllowCommand,
			}
			assert.Equal(t, w, got)
			assert.False(t, got.remote, "no preset may enable exec from external channels")
			assert.Equal(t, p, MatchPermissions(cfg))
		})
	}

	require.Error(t, ApplyPermissions(config.DefaultConfig(), "everything"))
}

func TestMatchPermissions(t *testing.T) {
	t.Setenv(config.EnvHome, t.TempDir())

	// The default config has restrict on, exec on (internal channels only),
	// web on, cron on with commands allowed: exactly the Advanced row, so
	// it matches Advanced rather than custom.
	assert.Equal(t, PresetAdvanced, MatchPermissions(config.DefaultConfig()))

	cfg := config.DefaultConfig()
	cfg.Tools.Exec.AllowRemote = true
	assert.Equal(t, PresetCustom, MatchPermissions(cfg), "allow_remote is in no preset")

	cfg = config.DefaultConfig()
	require.NoError(t, ApplyPermissions(cfg, PresetAssistant))
	cfg.Tools.Cron.AllowCommand = true
	assert.Equal(t, PresetCustom, MatchPermissions(cfg))
}
