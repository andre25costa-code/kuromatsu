package webui

func (s *server) routes() {
	m := s.api
	m.HandleFunc("POST /api/ui/logout", s.logout)
	m.HandleFunc("GET /api/ui/session", s.sessionInfo)

	m.HandleFunc("GET /api/ui/workspace/{file}", s.getWorkspaceFile)
	m.HandleFunc("PUT /api/ui/workspace/{file}", s.putWorkspaceFile)

	m.HandleFunc("GET /api/ui/models", s.listModels)
	m.HandleFunc("POST /api/ui/models", s.addModel)
	m.HandleFunc("POST /api/ui/models/probe", s.probeModels)
	m.HandleFunc("PUT /api/ui/models/default", s.setDefaultModel)

	m.HandleFunc("GET /api/ui/channels", s.listChannels)
	m.HandleFunc("PUT /api/ui/channels/{name}/allow_from", s.setAllowFrom)
	m.HandleFunc("GET /api/ui/permissions", s.getPermissions)
	m.HandleFunc("PUT /api/ui/permissions", s.setPermissions)

	m.HandleFunc("GET /api/ui/skills", s.listSkills)
	m.HandleFunc("POST /api/ui/skills", s.installSkill)
	m.HandleFunc("DELETE /api/ui/skills/{name}", s.removeSkill)

	m.HandleFunc("GET /api/ui/cron", s.listCron)
	m.HandleFunc("GET /api/ui/cron/templates", s.cronTemplates)
	m.HandleFunc("POST /api/ui/cron", s.createCron)
	m.HandleFunc("PATCH /api/ui/cron/{id}", s.editCron)
	m.HandleFunc("POST /api/ui/cron/{id}/pause", s.pauseCron)
	m.HandleFunc("POST /api/ui/cron/{id}/resume", s.resumeCron)
	m.HandleFunc("POST /api/ui/cron/{id}/run", s.runCron)
	m.HandleFunc("GET /api/ui/cron/{id}/history", s.cronHistory)
	m.HandleFunc("DELETE /api/ui/cron/{id}", s.deleteCron)

	m.HandleFunc("GET /api/ui/heartbeat", s.getHeartbeat)
	m.HandleFunc("PUT /api/ui/heartbeat", s.setHeartbeat)

	m.HandleFunc("GET /api/ui/status", s.status)
}
