package cmd

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/abskrj/velane/services/cli/internal/client"
	"github.com/spf13/cobra"
)

const defaultPageSize = 100

var sandboxesCmd = &cobra.Command{Use: "sandboxes", Short: "Manage durable sandboxes"}

var sandboxesListCmd = &cobra.Command{
	Use: "list", Short: "List sandboxes",
	RunE: func(cmd *cobra.Command, _ []string) error {
		output, _ := cmd.Flags().GetString("output")
		if err := outputFormat(output); err != nil {
			return err
		}
		c, err := newSandboxClient()
		if err != nil {
			return err
		}
		items, total, err := c.ListSandboxes(commandContext(cmd), 0, defaultPageSize)
		if err != nil {
			return err
		}
		if output == "json" {
			return writeJSON(map[string]any{"items": items, "total": total})
		}
		w := newTableWriter()
		fmt.Fprintln(w, "ID\tNAME\tDESIRED\tOBSERVED\tPROFILE\tRECIPE VERSION\tUPDATED")
		for _, item := range items {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", item.ID, item.Name, item.DesiredState, item.ObservedState, item.ProfileVersionID, item.RecipeVersionID, item.UpdatedAt)
		}
		return flushTable(w)
	},
}

var sandboxesGetCmd = &cobra.Command{
	Use: "get <sandbox-id>", Short: "Get a sandbox", Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		output, _ := cmd.Flags().GetString("output")
		if err := outputFormat(output); err != nil {
			return err
		}
		c, err := newSandboxClient()
		if err != nil {
			return err
		}
		sandbox, err := c.GetSandbox(commandContext(cmd), args[0])
		if err != nil {
			return err
		}
		if output == "json" {
			return writeJSON(sandbox)
		}
		fmt.Printf("ID: %s\nName: %s\nDesired state: %s\nObserved state: %s\nRecipe version: %s\nProfile version: %s\nLatest snapshot: %s\n", sandbox.ID, sandbox.Name, sandbox.DesiredState, sandbox.ObservedState, sandbox.RecipeVersionID, sandbox.ProfileVersionID, optionalString(sandbox.LatestSnapshotID))
		return nil
	},
}

var sandboxesCreateCmd = &cobra.Command{
	Use: "create <name> --recipe <recipe-version-id> --profile <profile-version-id>", Short: "Create a sandbox", Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		recipe, _ := cmd.Flags().GetString("recipe")
		profile, _ := cmd.Flags().GetString("profile")
		if recipe == "" || profile == "" {
			return fmt.Errorf("--recipe and --profile are required")
		}
		return runMutation(cmd, "sandboxes", func(ctx context.Context, c *client.Client, key, _ string) (*client.Operation, error) {
			sandbox, operation, err := c.CreateSandbox(ctx, client.CreateSandboxRequest{Name: args[0], RecipeVersionID: recipe, ProfileVersionID: profile}, key)
			if err == nil && sandbox != nil {
				fmt.Fprintf(os.Stderr, "Created sandbox %s with pinned profile %s\n", sandbox.ID, sandbox.ProfileVersionID)
			}
			return operation, err
		})
	},
}

func sandboxActionCommand(action string) *cobra.Command {
	return &cobra.Command{Use: action + " <sandbox-id>", Short: action + " a sandbox", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return runMutation(cmd, "sandboxes", func(ctx context.Context, c *client.Client, key, ifMatch string) (*client.Operation, error) {
			switch action {
			case "start":
				return c.StartSandbox(ctx, args[0], key, ifMatch)
			case "stop":
				return c.StopSandbox(ctx, args[0], key, ifMatch)
			default:
				return c.RestartSandbox(ctx, args[0], key, ifMatch)
			}
		})
	}}
}

var sandboxesRetryCmd = &cobra.Command{Use: "retry <sandbox-id> <operation-id>", Short: "Retry a failed sandbox operation", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
	return runMutation(cmd, "sandbox_operations", func(ctx context.Context, c *client.Client, key, ifMatch string) (*client.Operation, error) {
		return c.RetrySandboxOperation(ctx, args[0], args[1], key, ifMatch)
	})
}}

var sandboxesDeleteCmd = &cobra.Command{Use: "delete <sandbox-id>", Short: "Delete a sandbox", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	yes, _ := cmd.Flags().GetBool("yes")
	if err := requireConfirmation(yes, "sandbox deletion"); err != nil {
		return err
	}
	deleteSnapshots, _ := cmd.Flags().GetBool("delete-snapshots")
	return runMutation(cmd, "sandboxes", func(ctx context.Context, c *client.Client, key, ifMatch string) (*client.Operation, error) {
		return c.DeleteSandbox(ctx, args[0], deleteSnapshots, key, ifMatch)
	})
}}

var sandboxesStatusCmd = &cobra.Command{Use: "status <sandbox-id>", Short: "Show sandbox operation status", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	watch, _ := cmd.Flags().GetBool("watch")
	output, _ := cmd.Flags().GetString("output")
	if err := outputFormat(output); err != nil {
		return err
	}
	c, err := newSandboxClient()
	if err != nil {
		return err
	}
	for {
		sandbox, err := c.GetSandbox(commandContext(cmd), args[0])
		if err != nil {
			return err
		}
		if watch {
			if err := writeNDJSON(sandbox); err != nil {
				return err
			}
		} else if output == "json" {
			if err := writeJSON(sandbox); err != nil {
				return err
			}
		} else {
			fmt.Printf("%s\t%s\t%s\n", sandbox.ID, sandbox.DesiredState, sandbox.ObservedState)
			return nil
		}
		if !watch {
			return nil
		}
		select {
		case <-cmd.Context().Done():
			return cmd.Context().Err()
		case <-time.After(time.Second):
		}
	}
}}

var sandboxesWaitCmd = &cobra.Command{Use: "wait <operation-id>", Short: "Wait for a sandbox operation", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	output, _ := cmd.Flags().GetString("output")
	if err := outputFormat(output); err != nil {
		return err
	}
	c, err := newSandboxClient()
	if err != nil {
		return err
	}
	operation, err := waitForOperation(commandContext(cmd), c, args[0], cmd)
	if err != nil {
		return err
	}
	if output == "json" {
		return writeJSON(operation)
	}
	fmt.Printf("Operation %s: %s\n", operation.ID, operation.State)
	return nil
}}

var sandboxesWatchCmd = &cobra.Command{Use: "watch <sandbox-id>", Short: "Watch sandbox status as NDJSON", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	c, err := newSandboxClient()
	if err != nil {
		return err
	}
	for {
		sandbox, err := c.GetSandbox(commandContext(cmd), args[0])
		if err != nil {
			return err
		}
		if err := writeNDJSON(sandbox); err != nil {
			return err
		}
		select {
		case <-cmd.Context().Done():
			return cmd.Context().Err()
		case <-time.After(time.Second):
		}
	}
}}

func runMutation(cmd *cobra.Command, capability string, mutation func(context.Context, *client.Client, string, string) (*client.Operation, error)) error {
	output, _ := cmd.Flags().GetString("output")
	if err := outputFormat(output); err != nil {
		return err
	}
	c, err := newSandboxClient()
	if err != nil {
		return err
	}
	if err := c.RequireSandboxCapability(commandContext(cmd), capability); err != nil {
		return err
	}
	key, _ := cmd.Flags().GetString("idempotency-key")
	if key == "" {
		key, err = newUUID()
		if err != nil {
			return err
		}
	}
	ifMatch, _ := cmd.Flags().GetString("generation")
	operation, err := mutation(commandContext(cmd), c, key, ifMatch)
	if err != nil {
		return err
	}
	wait, _ := cmd.Flags().GetBool("wait")
	if !wait {
		return printOperation(operation.ID, output)
	}
	operation, err = waitForOperation(commandContext(cmd), c, operation.ID, cmd)
	if err != nil {
		return err
	}
	if output == "json" {
		return writeJSON(operation)
	}
	fmt.Printf("Operation %s: %s\n", operation.ID, operation.State)
	return nil
}

func commandContext(cmd *cobra.Command) context.Context {
	return cmd.Context()
}

func waitForOperation(ctx context.Context, c *client.Client, operationID string, cmd *cobra.Command) (*client.Operation, error) {
	timeout, _ := cmd.Flags().GetDuration("timeout")
	interval, _ := cmd.Flags().GetDuration("poll-interval")
	if interval <= 0 {
		interval = time.Second
	}
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		operation, retryAfter, err := c.GetOperation(ctx, operationID)
		if err != nil {
			return nil, err
		}
		if client.IsTerminalOperation(operation.State) {
			if operation.State != "succeeded" {
				return nil, fmt.Errorf("operation %s %s: %s", operation.ID, operation.State, operation.FailureMessage)
			}
			return operation, nil
		}
		wait := interval
		if retryAfter > 0 {
			wait = time.Duration(retryAfter) * time.Second
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for operation: %w", ctx.Err())
		case <-time.After(wait):
		}
	}
}

func newUUID() (string, error) {
	var bytes [16]byte
	if _, err := io.ReadFull(rand.Reader, bytes[:]); err != nil {
		return "", fmt.Errorf("generate idempotency key: %w", err)
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16]), nil
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func addMutationFlags(cmd *cobra.Command) {
	cmd.Flags().String("idempotency-key", "", "Idempotency key for this mutation")
	cmd.Flags().String("generation", "", "Expected sandbox generation")
	cmd.Flags().Bool("wait", false, "Wait for the operation to finish")
	cmd.Flags().Duration("timeout", 5*time.Minute, "Maximum time to wait")
	cmd.Flags().Duration("poll-interval", time.Second, "Operation polling interval")
	cmd.Flags().String("output", "table", "Output format: table or json")
}

func init() {
	rootCmd.AddCommand(sandboxesCmd)
	for _, command := range []*cobra.Command{sandboxesListCmd, sandboxesGetCmd, sandboxesCreateCmd, sandboxActionCommand("start"), sandboxActionCommand("stop"), sandboxActionCommand("restart"), sandboxesRetryCmd, sandboxesDeleteCmd, sandboxesStatusCmd, sandboxesWaitCmd, sandboxesWatchCmd} {
		sandboxesCmd.AddCommand(command)
	}
	sandboxesListCmd.Flags().String("output", "table", "Output format: table or json")
	sandboxesGetCmd.Flags().String("output", "table", "Output format: table or json")
	sandboxesStatusCmd.Flags().Bool("watch", false, "Refresh while the sandbox is transitional")
	sandboxesStatusCmd.Flags().String("output", "table", "Output format: table or json")
	sandboxesWaitCmd.Flags().String("output", "table", "Output format: table or json")
	sandboxesWaitCmd.Flags().Duration("timeout", 5*time.Minute, "Maximum time to wait")
	sandboxesWaitCmd.Flags().Duration("poll-interval", time.Second, "Operation polling interval")
	for _, command := range []*cobra.Command{sandboxesCreateCmd, sandboxesRetryCmd, sandboxesDeleteCmd} {
		addMutationFlags(command)
	}
	sandboxesCreateCmd.Flags().String("recipe", "", "Ready image recipe version ID")
	sandboxesCreateCmd.Flags().String("profile", "", "Immutable sandbox profile version ID")
	sandboxesDeleteCmd.Flags().Bool("yes", false, "Confirm deletion")
	sandboxesDeleteCmd.Flags().Bool("delete-snapshots", false, "Delete retained snapshots")
	for _, command := range sandboxesCmd.Commands() {
		if command.Name() == "start" || command.Name() == "stop" || command.Name() == "restart" {
			addMutationFlags(command)
		}
	}
}
