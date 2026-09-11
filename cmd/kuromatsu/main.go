// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/andre25costa-code/kuromatsu/cmd/kuromatsu/internal"
	"github.com/andre25costa-code/kuromatsu/cmd/kuromatsu/internal/agent"
	"github.com/andre25costa-code/kuromatsu/cmd/kuromatsu/internal/auth"
	"github.com/andre25costa-code/kuromatsu/cmd/kuromatsu/internal/cliui"
	configcmd "github.com/andre25costa-code/kuromatsu/cmd/kuromatsu/internal/config"
	"github.com/andre25costa-code/kuromatsu/cmd/kuromatsu/internal/cron"
	"github.com/andre25costa-code/kuromatsu/cmd/kuromatsu/internal/gateway"
	"github.com/andre25costa-code/kuromatsu/cmd/kuromatsu/internal/mcp"
	"github.com/andre25costa-code/kuromatsu/cmd/kuromatsu/internal/migrate"
	"github.com/andre25costa-code/kuromatsu/cmd/kuromatsu/internal/model"
	"github.com/andre25costa-code/kuromatsu/cmd/kuromatsu/internal/onboard"
	"github.com/andre25costa-code/kuromatsu/cmd/kuromatsu/internal/skills"
	"github.com/andre25costa-code/kuromatsu/cmd/kuromatsu/internal/status"
	"github.com/andre25costa-code/kuromatsu/cmd/kuromatsu/internal/version"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/updater"
)

var rootNoColor bool

// initTermuxSSL detects Termux environment and sets SSL_CERT_FILE if not already set.
// This fixes X509 certificate errors when running Kuromatsu inside Termux or termux-chroot.
// See: https://github.com/andre25costa-code/kuromatsu/issues/2944
func initTermuxSSL() {
	// Only applicable on Linux/Android
	if runtime.GOOS != "linux" && runtime.GOOS != "android" {
		return
	}

	// Skip if already set
	if os.Getenv("SSL_CERT_FILE") != "" {
		return
	}

	// Check for Termux prefix in PATH or HOME
	home := os.Getenv("HOME")
	path := os.Getenv("PATH")

	isTermux := strings.Contains(home, "com.termux") ||
		strings.Contains(path, "com.termux") ||
		strings.Contains(home, "/data/data/com.termux")

	if !isTermux {
		return
	}

	// Check common CA bundle locations in Termux
	caPaths := []string{
		"$PREFIX/etc/tls/cert.pem",
		os.Getenv("PREFIX") + "/etc/tls/cert.pem",
		"/data/data/com.termux/files/usr/etc/tls/cert.pem",
		"/usr/etc/tls/cert.pem",
	}

	for _, caPath := range caPaths {
		expanded := os.ExpandEnv(caPath)
		if _, err := os.Stat(expanded); err == nil {
			os.Setenv("SSL_CERT_FILE", expanded)
			return
		}
	}
}

func syncCliUIColor(root *cobra.Command) {
	no, _ := root.PersistentFlags().GetBool("no-color")
	cliui.Init(no || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb")
}

// earlyColorDisabled matches lipgloss/banner behavior from env and argv before Cobra parses flags.
func earlyColorDisabled() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return true
	}
	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		if arg == "--no-color" || arg == "--no-color=true" || arg == "--no-color=1" {
			return true
		}
	}
	return false
}

func NewPicoclawCommand() *cobra.Command {
	short := fmt.Sprintf("%s Kuromatsu — personal AI assistant with an embedded native model", internal.Logo)
	long := fmt.Sprintf(`%s Kuromatsu is a personal AI assistant, forked from PicoClaw, that runs
Bonsai-1.7B-Q1_0 (1-bit quantized) in-process as a fallback with no API key
required, and any configured provider as the primary model.

Version: %s`, internal.Logo, config.FormatVersion())

	cmd := &cobra.Command{
		Use:   "kuromatsu",
		Short: short,
		Long:  long,
		Example: `kuromatsu version
kuromatsu onboard
kuromatsu --no-color status`,
		SilenceErrors: true,
		// Avoid plain UsageString() on stderr/stdout when a command fails; cliui
		// renders matching panels on stderr instead.
		SilenceUsage: true,
		PersistentPreRun: func(c *cobra.Command, _ []string) {
			syncCliUIColor(c.Root())
		},
	}

	cmd.PersistentFlags().BoolVar(&rootNoColor, "no-color", false,
		"Disable colors (boxed layout unchanged)")

	cmd.SetHelpFunc(func(c *cobra.Command, _ []string) {
		syncCliUIColor(c.Root())
		fmt.Fprint(c.OutOrStdout(), cliui.RenderCommandHelp(c))
	})

	cmd.AddCommand(
		configcmd.NewConfigCommand(),
		onboard.NewOnboardCommand(),
		agent.NewAgentCommand(),
		auth.NewAuthCommand(),
		gateway.NewGatewayCommand(),
		status.NewStatusCommand(),
		cron.NewCronCommand(),
		mcp.NewMCPCommand(),
		migrate.NewMigrateCommand(),
		skills.NewSkillsCommand(),
		model.NewModelCommand(),
		updater.NewUpdateCommand("kuromatsu"),
		version.NewVersionCommand(),
	)

	return cmd
}

const colorGreen = "\033[1;38;2;45;106;79m"

// buildBanner draws a bordered title box sized by strings.Repeat rather
// than a hand-aligned block-letter const, so the border always matches the
// content width exactly regardless of title length.
func buildBanner(colored bool) string {
	const title = "K U R O M A T S U"
	const padding = 6
	border := strings.Repeat("─", len(title)+padding*2)
	content := strings.Repeat(" ", padding) + title + strings.Repeat(" ", padding)

	var b strings.Builder
	b.WriteString("\r\n")
	if colored {
		b.WriteString(colorGreen)
	}
	fmt.Fprintf(&b, "  ╭%s╮\n", border)
	fmt.Fprintf(&b, "  │%s│\n", content)
	fmt.Fprintf(&b, "  ╰%s╯\n", border)
	if colored {
		b.WriteString("\033[0m")
	}
	b.WriteString("  🌲 resiliência em condições extremas\r\n\r\n")
	return b.String()
}

func main() {
	// Initialize Termux SSL certificate detection before anything else
	initTermuxSSL()

	cliui.Init(earlyColorDisabled())

	fmt.Print(buildBanner(!earlyColorDisabled()))

	tzEnv := os.Getenv("TZ")
	if tzEnv != "" {
		fmt.Println("TZ environment:", tzEnv)
		zoneinfoEnv := os.Getenv("ZONEINFO")
		fmt.Println("ZONEINFO environment:", zoneinfoEnv)
		loc, err := time.LoadLocation(tzEnv)
		if err != nil {
			fmt.Println("Error loading time zone:", err)
		} else {
			fmt.Println("Time zone loaded successfully:", loc)
			time.Local = loc //nolint:gosmopolitan // We intentionally set local timezone from TZ env
		}
	}

	cmd := NewPicoclawCommand()
	last, err := cmd.ExecuteC()
	if err != nil {
		syncCliUIColor(cmd)
		fmt.Fprint(os.Stderr, cliui.FormatCLIError(err.Error(), last))
		os.Exit(1)
	}
}
