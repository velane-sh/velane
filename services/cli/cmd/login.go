package cmd

import (
	"fmt"

	"github.com/abskrj/velane/services/cli/internal/keyring"
	"github.com/abskrj/velane/services/cli/internal/telemetry"
	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Store API key in the system keychain",
	RunE: func(cmd *cobra.Command, args []string) error {
		key, _ := cmd.Flags().GetString("key")
		if key == "" {
			return fmt.Errorf("--key is required")
		}
		if err := keyring.SaveAPIKey(key); err != nil {
			telemetry.Fire("cli.login", map[string]any{"error": true})
			return fmt.Errorf("save api key: %w", err)
		}
		telemetry.Fire("cli.login", map[string]any{"error": false})
		fmt.Println("API key saved to keychain.")
		return nil
	},
}

func init() {
	loginCmd.Flags().String("key", "", "API key (vl_xxxx)")
	_ = loginCmd.MarkFlagRequired("key")
}
