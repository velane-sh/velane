package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/abskrj/velane/services/cli/internal/client"
	"github.com/abskrj/velane/services/cli/internal/keyring"
	"github.com/abskrj/velane/services/cli/internal/telemetry"
	"github.com/spf13/cobra"
)

var snippetsCmd = &cobra.Command{
	Use:   "snippets",
	Short: "Manage snippets",
}

var snippetsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all snippets",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		snippets, err := c.ListSnippets(context.Background())
		telemetry.Fire("cli.snippets.list", map[string]any{"error": err != nil})
		if err != nil {
			return err
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tLANGUAGE\tCREATED")
		for _, sn := range snippets {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", sn.ID, sn.Name, sn.Language, sn.CreatedAt)
		}
		return w.Flush()
	},
}

var snippetsPushCmd = &cobra.Command{
	Use:   "push <file>",
	Short: "Push a local file as a snippet version",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filePath := args[0]
		code, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("read file: %w", err)
		}

		lang := detectLanguage(filePath)
		snippetName := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
		workflowIDFlag, _ := cmd.Flags().GetString("id")
		publishEnv, _ := cmd.Flags().GetString("publish")

		c, err := newClient()
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		var snippetID string
		if workflowIDFlag != "" {
			snippetID = workflowIDFlag
		} else {
			snippets, err := c.ListSnippets(ctx)
			if err != nil {
				return err
			}
			for _, sn := range snippets {
				if sn.Name == snippetName {
					snippetID = sn.ID
					break
				}
			}
		}

		if snippetID == "" {
			sn, err := c.CreateSnippet(ctx, snippetName, lang)
			if err != nil {
				return fmt.Errorf("create snippet: %w", err)
			}
			snippetID = sn.ID
			fmt.Printf("Created snippet %s (%s)\n", sn.Name, sn.ID)
		} else {
			fmt.Printf("Updating existing snippet %s\n", snippetID)
		}

		v, err := c.CreateVersion(ctx, snippetID, string(code))
		telemetry.Fire("cli.push", map[string]any{"language": lang, "error": err != nil})
		if err != nil {
			return fmt.Errorf("create version: %w", err)
		}
		fmt.Printf("Created version %d (draft)\n", v.VersionNumber)

		if publishEnv != "" {
			published, err := c.PublishVersion(ctx, snippetID, v.VersionNumber, publishEnv)
			if err != nil {
				return fmt.Errorf("publish version: %w", err)
			}
			fmt.Printf("Published version %d to %s\n", published.VersionNumber, publishEnv)
		}

		return nil
	},
}

var snippetsGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a snippet by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		sn, err := c.GetSnippet(context.Background(), args[0])
		if err != nil {
			return err
		}
		fmt.Printf("ID:       %s\n", sn.ID)
		fmt.Printf("Name:     %s\n", sn.Name)
		fmt.Printf("Language: %s\n", sn.Language)
		fmt.Printf("Created:  %s\n", sn.CreatedAt)
		return nil
	},
}

var snippetsDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a snippet by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		if err := c.DeleteSnippet(context.Background(), args[0]); err != nil {
			return err
		}
		fmt.Println("Snippet deleted.")
		return nil
	},
}

func init() {
	snippetsPushCmd.Flags().String("id", "", "Existing workflow ID to update (defaults to matching name)")
	snippetsPushCmd.Flags().String("publish", "", "Publish to environment after push (dev|staging|prod)")

	snippetsCmd.AddCommand(snippetsListCmd)
	snippetsCmd.AddCommand(snippetsPushCmd)
	snippetsCmd.AddCommand(snippetsGetCmd)
	snippetsCmd.AddCommand(snippetsDeleteCmd)
}

// detectLanguage infers the runtime from a file extension.
func detectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".py":
		return "python"
	default:
		return "bun"
	}
}

// newClient constructs an authenticated API client from flags and keychain.
func newClient() (*client.Client, error) {
	key, err := keyring.LoadAPIKey()
	if err != nil {
		return nil, fmt.Errorf("no API key found — run: velane login --key <key>")
	}
	if tenantSlug == "" {
		return nil, fmt.Errorf("--tenant flag is required")
	}
	return client.New(apiURL, tenantSlug, key), nil
}
