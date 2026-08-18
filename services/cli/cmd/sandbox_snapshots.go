package cmd

import (
	"context"
	"fmt"

	"github.com/abskrj/velane/services/cli/internal/client"
	"github.com/spf13/cobra"
)

var sandboxSnapshotsCmd = &cobra.Command{Use: "snapshots", Short: "Manage full sandbox snapshots"}

var sandboxSnapshotsListCmd = &cobra.Command{Use: "list <sandbox-id>", Short: "List full snapshots", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	output, _ := cmd.Flags().GetString("output")
	if err := outputFormat(output); err != nil {
		return err
	}
	c, err := newSandboxClient()
	if err != nil {
		return err
	}
	items, total, err := c.ListSnapshots(commandContext(cmd), args[0], 0, defaultPageSize)
	if err != nil {
		return err
	}
	if output == "json" {
		return writeJSON(map[string]any{"items": items, "total": total})
	}
	w := newTableWriter()
	fmt.Fprintln(w, "ID\tSTATE\tKIND\tSIZE\tFAILURE\tCREATED")
	for _, item := range items {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\n", item.ID, item.State, item.Kind, item.TotalBytes, item.FailureMessage, item.CreatedAt)
	}
	return flushTable(w)
}}

var sandboxSnapshotsGetCmd = &cobra.Command{Use: "get <sandbox-id> <snapshot-id>", Short: "Get a full snapshot summary", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
	output, _ := cmd.Flags().GetString("output")
	if err := outputFormat(output); err != nil {
		return err
	}
	c, err := newSandboxClient()
	if err != nil {
		return err
	}
	snapshot, err := c.GetSnapshot(commandContext(cmd), args[0], args[1])
	if err != nil {
		return err
	}
	if output == "json" {
		return writeJSON(snapshot)
	}
	fmt.Printf("ID: %s\nState: %s\nKind: %s\nSize: %d bytes\nFailure: %s\n", snapshot.ID, snapshot.State, snapshot.Kind, snapshot.TotalBytes, snapshot.FailureMessage)
	return nil
}}

var sandboxSnapshotsCreateCmd = &cobra.Command{Use: "create <sandbox-id>", Short: "Create a full snapshot", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	return runMutation(cmd, "sandbox_snapshots", func(ctx context.Context, c *client.Client, key, ifMatch string) (*client.Operation, error) {
		return c.CreateSnapshot(ctx, args[0], key, ifMatch)
	})
}}

var sandboxSnapshotsRestoreCmd = &cobra.Command{Use: "restore <sandbox-id> <snapshot-id>", Short: "Restore a stopped sandbox from a full snapshot", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
	yes, _ := cmd.Flags().GetBool("yes")
	if err := requireConfirmation(yes, "snapshot restore"); err != nil {
		return err
	}
	return runMutation(cmd, "sandbox_snapshots", func(ctx context.Context, c *client.Client, key, ifMatch string) (*client.Operation, error) {
		return c.RestoreSnapshot(ctx, args[0], args[1], key, ifMatch)
	})
}}

var sandboxSnapshotsDeleteCmd = &cobra.Command{Use: "delete <sandbox-id> <snapshot-id>", Short: "Delete a snapshot", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
	yes, _ := cmd.Flags().GetBool("yes")
	if err := requireConfirmation(yes, "snapshot deletion"); err != nil {
		return err
	}
	return runMutation(cmd, "sandbox_snapshots", func(ctx context.Context, c *client.Client, key, ifMatch string) (*client.Operation, error) {
		return c.DeleteSnapshot(ctx, args[0], args[1], key, ifMatch)
	})
}}

func init() {
	sandboxesCmd.AddCommand(sandboxSnapshotsCmd)
	sandboxSnapshotsCmd.AddCommand(sandboxSnapshotsListCmd, sandboxSnapshotsGetCmd, sandboxSnapshotsCreateCmd, sandboxSnapshotsRestoreCmd, sandboxSnapshotsDeleteCmd)
	for _, command := range []*cobra.Command{sandboxSnapshotsListCmd, sandboxSnapshotsGetCmd} {
		command.Flags().String("output", "table", "Output format: table or json")
	}
	for _, command := range []*cobra.Command{sandboxSnapshotsCreateCmd, sandboxSnapshotsRestoreCmd, sandboxSnapshotsDeleteCmd} {
		addMutationFlags(command)
	}
	sandboxSnapshotsRestoreCmd.Flags().Bool("yes", false, "Confirm destructive restore")
	sandboxSnapshotsDeleteCmd.Flags().Bool("yes", false, "Confirm deletion")
}
