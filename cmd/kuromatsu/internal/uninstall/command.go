// Package uninstall implements `kuromatsu uninstall`: it removes the binary
// placed by the installer, the PATH entries the installer added and, with
// --purge, the data directory (after backing it up to a zip file).
package uninstall

import (
	"os"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/pid"
)

// NewUninstallCommand returns the `uninstall` command.
func NewUninstallCommand() *cobra.Command {
	var o options

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the kuromatsu binary and its PATH entries (--purge also deletes the data directory)",
		Long: `Remove the kuromatsu binary installed by the install script and the PATH
entries the installer added. The data directory is kept unless --purge is
given; --purge first writes a kuromatsu-backup-<date>.zip to your home
directory and refuses directories that do not look like a Kuromatsu home.
A binary installed by a package manager or in a system directory is left
alone.`,
		Example: `kuromatsu uninstall
kuromatsu uninstall --purge
kuromatsu uninstall --purge --yes`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e := defaultEnv()
			e.stdin = cmd.InOrStdin()
			e.stdout = cmd.OutOrStdout()
			e.isTerminal = func() bool {
				f, ok := e.stdin.(*os.File)
				return ok && term.IsTerminal(int(f.Fd()))
			}
			return run(o, e)
		},
	}

	cmd.Flags().BoolVar(&o.purge, "purge", false,
		"Also back up and delete the data directory")
	cmd.Flags().BoolVar(&o.force, "force", false,
		"With --purge, also delete unexpected entries in the data directory")
	cmd.Flags().BoolVarP(&o.yes, "yes", "y", false,
		"Do not ask for confirmation (required without a terminal)")

	return cmd
}

// defaultEnv wires the real system into env; the command fills in the I/O.
func defaultEnv() *env {
	userHome, _ := os.UserHomeDir()
	return &env{
		home:           config.GetHome(),
		homeFromEnv:    os.Getenv(config.EnvHome) != "",
		userHome:       userHome,
		executable:     os.Executable,
		runningGateway: pid.RunningGateway,
		freeSpace:      freeSpace,
		writable:       writable,
		removeBinary:   removeBinary,
		updatePath:     removeFromPath,
		now:            time.Now,
	}
}
