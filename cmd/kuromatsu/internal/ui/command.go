// Package ui implements `kuromatsu ui`: the access token and the address of
// the local configuration page the gateway serves at /ui.
package ui

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/andre25costa-code/kuromatsu/cmd/kuromatsu/internal"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

// envWebUIToken overrides the stored token.
const envWebUIToken = "KUROMATSU_WEBUI_TOKEN"

// NewUICommand returns the `ui` command with its `token` and `open`
// subcommands.
func NewUICommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Local configuration page (token and address)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "token",
			Short: "Print the web UI access token, creating it if missing",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return printToken(cmd.OutOrStdout(), internal.GetConfigPath())
			},
		},
		&cobra.Command{
			Use:   "open",
			Short: "Print the address of the web UI",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return printURL(cmd.OutOrStdout(), internal.GetConfigPath())
			},
		},
	)
	return cmd
}

// printToken prints the stored token. When none is stored it creates one
// and saves it to the security file (loading the config as stored, so no
// environment value reaches the disk). A token given only through
// KUROMATSU_WEBUI_TOKEN is printed as is and never saved.
func printToken(out io.Writer, configPath string) error {
	if env := os.Getenv(envWebUIToken); env != "" {
		// The gateway uses the environment value over the stored one.
		fmt.Fprintln(out, env)
		fmt.Fprintln(out, "# from "+envWebUIToken)
		return nil
	}
	cfg, err := config.LoadConfigForEdit(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	created, err := cfg.WebUI.EnsureToken()
	if err != nil {
		return err
	}
	if created {
		if err = config.SaveConfig(configPath, cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
	}
	token := cfg.WebUI.Token.String()
	fmt.Fprintln(out, token)
	if created {
		fmt.Fprintln(out, "# token created; restart the gateway for the web UI to use it")
	}
	return nil
}

// printURL prints the loopback address of the page.
func printURL(out io.Writer, configPath string) error {
	cfg, err := config.LoadConfigForEdit(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	port := cfg.Gateway.Port
	if port <= 0 {
		port = config.DefaultConfig().Gateway.Port
	}
	fmt.Fprintf(out, "http://localhost:%s/ui/\n", strconv.Itoa(port))
	if !cfg.WebUI.Enabled {
		fmt.Fprintln(out, "# webui.enabled is false in the config; enable it and restart the gateway")
	}
	return nil
}
