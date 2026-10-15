package setup

import (
	"fmt"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

// Preset names a permission preset: a fixed value for each of the six
// permission keys (see presetValues).
type Preset string

const (
	PresetSafe      Preset = "seguro"
	PresetAssistant Preset = "assistente"
	PresetAdvanced  Preset = "avancado"
	// PresetCustom is what MatchPermissions returns when no preset matches.
	PresetCustom Preset = "custom"
)

// Presets lists the presets from the most to the least restrictive.
var Presets = []Preset{PresetSafe, PresetAssistant, PresetAdvanced}

// permissionValues are the six keys a preset writes.
type permissionValues struct {
	RestrictToWorkspace bool // agents.defaults.restrict_to_workspace
	ExecEnabled         bool // tools.exec.enabled
	ExecAllowRemote     bool // tools.exec.allow_remote
	WebEnabled          bool // tools.web.enabled
	CronEnabled         bool // tools.cron.enabled
	CronAllowCommand    bool // tools.cron.allow_command
}

// presetValues is the preset table. No preset enables exec from external
// channels (allow_remote): with it off, exec runs only from the internal
// channels (cli, system, subagent). Turning it on stays a manual edit.
var presetValues = map[Preset]permissionValues{
	PresetSafe: {
		RestrictToWorkspace: true,
	},
	PresetAssistant: {
		RestrictToWorkspace: true,
		WebEnabled:          true,
		CronEnabled:         true,
	},
	PresetAdvanced: {
		RestrictToWorkspace: true,
		ExecEnabled:         true,
		WebEnabled:          true,
		CronEnabled:         true,
		CronAllowCommand:    true,
	},
}

// ApplyPermissions writes the six permission keys of preset p to cfg.
func ApplyPermissions(cfg *config.Config, p Preset) error {
	v, ok := presetValues[p]
	if !ok {
		return fmt.Errorf("unknown permission preset %q", p)
	}
	cfg.Agents.Defaults.RestrictToWorkspace = v.RestrictToWorkspace
	cfg.Tools.Exec.Enabled = v.ExecEnabled
	cfg.Tools.Exec.AllowRemote = v.ExecAllowRemote
	cfg.Tools.Web.Enabled = v.WebEnabled
	cfg.Tools.Cron.Enabled = v.CronEnabled
	cfg.Tools.Cron.AllowCommand = v.CronAllowCommand
	return nil
}

// MatchPermissions returns the preset whose six values all equal cfg's, or
// PresetCustom.
func MatchPermissions(cfg *config.Config) Preset {
	cur := permissionValues{
		RestrictToWorkspace: cfg.Agents.Defaults.RestrictToWorkspace,
		ExecEnabled:         cfg.Tools.Exec.Enabled,
		ExecAllowRemote:     cfg.Tools.Exec.AllowRemote,
		WebEnabled:          cfg.Tools.Web.Enabled,
		CronEnabled:         cfg.Tools.Cron.Enabled,
		CronAllowCommand:    cfg.Tools.Cron.AllowCommand,
	}
	for _, p := range Presets {
		if presetValues[p] == cur {
			return p
		}
	}
	return PresetCustom
}
