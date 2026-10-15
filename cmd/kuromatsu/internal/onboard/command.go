package onboard

import (
	"github.com/spf13/cobra"

	"github.com/andre25costa-code/kuromatsu"
)

var embeddedFiles = kuromatsu.OnboardWorkspace

func NewOnboardCommand() *cobra.Command {
	var encrypt, force, nonInteractive bool
	var profile string

	cmd := &cobra.Command{
		Use:     "onboard",
		Aliases: []string{"o"},
		Short:   "Initialize kuromatsu configuration and workspace",
		// With a terminal on stdin and stdout (and neither --enc nor
		// --non-interactive) onboard runs the setup wizard; otherwise it
		// writes the default config without asking anything.
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) == 0 {
				onboard(encrypt, force, profile, nonInteractive)
			} else {
				_ = cmd.Help()
			}
		},
	}

	cmd.Flags().BoolVar(&encrypt, "enc", false,
		"Enable credential encryption (generates SSH key and prompts for passphrase)")
	cmd.Flags().BoolVar(&force, "force", false,
		"Overwrite existing workspace files (AGENT.md, SOUL.md, USER.md, memory...) with the templates")

	cmd.Flags().StringVar(&profile, "profile", "",
		"Apply a hardware profile to the config: ultra-light, light, balanced, performance or auto (detected)")

	// --non-interactive skips the wizard even on a terminal, so installers
	// and containers never get a prompt.
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false,
		"Never prompt (for installers and containers without a terminal); cannot be combined with --enc")
	cmd.MarkFlagsMutuallyExclusive("enc", "non-interactive")

	return cmd
}
