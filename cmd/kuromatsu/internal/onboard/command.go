package onboard

import (
	"github.com/spf13/cobra"

	"github.com/andre25costa-code/kuromatsu"
)

var embeddedFiles = kuromatsu.OnboardWorkspace

func NewOnboardCommand() *cobra.Command {
	var encrypt, force bool

	cmd := &cobra.Command{
		Use:     "onboard",
		Aliases: []string{"o"},
		Short:   "Initialize kuromatsu configuration and workspace",
		// Run without subcommands → original onboard flow
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) == 0 {
				onboard(encrypt, force)
			} else {
				_ = cmd.Help()
			}
		},
	}

	cmd.Flags().BoolVar(&encrypt, "enc", false,
		"Enable credential encryption (generates SSH key and prompts for passphrase)")
	cmd.Flags().BoolVar(&force, "force", false,
		"Overwrite existing workspace files (AGENT.md, SOUL.md, USER.md, memory...) with the templates")

	return cmd
}
