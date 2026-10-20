package gateway

import (
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/agent"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/logger"
	"github.com/andre25costa-code/kuromatsu/pkg/sysinfo"
	"github.com/andre25costa-code/kuromatsu/pkg/webui"
)

// webUIDeps connects the web UI to the running gateway. Everything is read
// through getters, because a reload replaces the config and the scheduler;
// the routes themselves live on the channel manager's mux, which a reload
// keeps.
func webUIDeps(configPath string, al *agent.AgentLoop, reload func() error) webui.Deps {
	return webui.Deps{
		ConfigPath: configPath,
		Workspace:  func() string { return al.GetConfig().WorkspacePath() },
		Runtime:    al.GetConfig,
		Cron:       al.CronService,
		Reload:     reload,
		QueryStats: al.QueryStats,
		Hardware:   sysinfo.Detect,
		Now:        time.Now,
	}
}

// mountWebUI serves the web UI on m when cfg enables it and has a token.
// webui.enabled and the token are read once: changing them needs a restart.
func mountWebUI(m webui.Mounter, cfg *config.Config, d webui.Deps) bool {
	mounted, err := webui.Mount(m, cfg, d)
	if err != nil {
		logger.ErrorCF("webui", "web UI not mounted", map[string]any{"error": err.Error()})
		return false
	}
	return mounted
}
