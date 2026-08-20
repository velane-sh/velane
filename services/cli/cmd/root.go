package cmd

import (
	"os"
	"time"

	"github.com/spf13/cobra"
)

var (
	apiURL         string
	tenantSlug     string
	requestTimeout time.Duration
)

var rootCmd = &cobra.Command{
	Use:   "velane",
	Short: "Velane CLI — manage and invoke AI agents",
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&apiURL, "api-url", "https://api.velane.io", "Velane control plane URL")
	rootCmd.PersistentFlags().StringVar(&tenantSlug, "tenant", "", "Tenant slug")
	rootCmd.PersistentFlags().DurationVar(&requestTimeout, "request-timeout", 30*time.Second, "Sandbox API request timeout")

	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(snippetsCmd)
	rootCmd.AddCommand(invokeCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(versionsCmd)
}
