package commands

// focoCommand is a metadata-only Definition (no Handler) — like useCommand
// in cmd_use.go, /foco's actual behavior needs to mutate the in-flight
// processOptions (arm session aderência, rewrite the message, route just
// one message) in ways the generic Runtime-handler contract can't express.
// It's special-cased in pkg/agent/agent_command.go's applyExplicitFocusCommand,
// checked by name before the generic executor runs — this Definition exists
// so /foco/​/focus still show up in /help and any future command-listing UI.
func focoCommand() Definition {
	return Definition{
		Name:        "foco",
		Aliases:     []string{"focus"},
		Description: "Show or set the active focus window for this session",
		Usage:       "/foco [window|auto|off] [message]",
	}
}
