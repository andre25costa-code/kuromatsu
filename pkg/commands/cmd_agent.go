package commands

// agentCommand is a metadata-only Definition (no Handler) — like
// focoCommand in cmd_foco.go, /agent's actual behavior needs registry
// lookups and per-chat pin state (persisted across restarts, Trilho G B.3)
// the generic Runtime-handler contract can't express. Special-cased in
// pkg/agent/agent_command.go's applyExplicitAgentCommand, checked by name
// before the generic executor runs — this Definition exists so /agent
// still shows up in /help and any future command-listing UI.
func agentCommand() Definition {
	return Definition{
		Name:        "agent",
		Description: "Show or pin which registered agent handles this chat",
		Usage:       "/agent [id|auto]",
	}
}
