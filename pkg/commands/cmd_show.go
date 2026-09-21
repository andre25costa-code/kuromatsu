package commands

import (
	"context"
	"fmt"
)

func showCommand() Definition {
	return Definition{
		Name:        "show",
		Description: "Show current configuration",
		SubCommands: []SubCommand{
			{
				Name:        "config",
				Description: "Active configuration file and default workspace",
				Handler: func(_ context.Context, req Request, rt *Runtime) error {
					if rt == nil || rt.Config == nil {
						return req.Reply(unavailableMsg)
					}
					path := rt.Config.SourcePath
					if path == "" {
						path = "in-memory defaults"
					}
					return req.Reply(
						fmt.Sprintf(
							"Active config: %s\nDefault workspace: %s",
							path,
							rt.Config.Agents.Defaults.Workspace,
						),
					)
				},
			},
			{
				Name:        "model",
				Description: "Current model and provider",
				Handler: func(_ context.Context, req Request, rt *Runtime) error {
					if rt == nil || rt.GetModelInfo == nil {
						return req.Reply(unavailableMsg)
					}
					name, provider := rt.GetModelInfo()
					return req.Reply(fmt.Sprintf("Current Model: %s (Provider: %s)", name, provider))
				},
			},
			{
				Name:        "channel",
				Description: "Current channel",
				Handler: func(_ context.Context, req Request, _ *Runtime) error {
					return req.Reply(fmt.Sprintf("Current Channel: %s", req.Channel))
				},
			},
			{
				Name:        "agents",
				Description: "Registered agents",
				Handler:     agentsHandler(),
			},
			{
				Name:        "mcp",
				Description: "Active tools for an MCP server",
				ArgsUsage:   "<server>",
				Handler:     showMCPToolsHandler(),
			},
		},
	}
}
