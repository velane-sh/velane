package cmd

import (
	"fmt"
	"time"

	"github.com/abskrj/velane/services/cli/internal/client"
	"github.com/spf13/cobra"
)

var sandboxProfilesCmd = &cobra.Command{Use: "sandbox-profiles", Short: "List provider-neutral sandbox profiles"}
var sandboxProfilesListCmd = &cobra.Command{Use: "list", Short: "List active sandbox profiles", RunE: func(cmd *cobra.Command, _ []string) error {
	output, _ := cmd.Flags().GetString("output")
	if err := outputFormat(output); err != nil {
		return err
	}
	c, err := newSandboxClient()
	if err != nil {
		return err
	}
	profiles, err := c.ListSandboxProfiles(commandContext(cmd))
	if err != nil {
		return err
	}
	if output == "json" {
		return writeJSON(map[string]any{"items": profiles})
	}
	w := newTableWriter()
	fmt.Fprintln(w, "ID\tNAME\tFAMILY\tVERSION\tSTATUS\tVCPU\tMEMORY (MB)")
	for _, profile := range profiles {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%d\n", profile.ID, profile.Name, profile.ProfileFamily, profile.Version, profile.Status, profile.VCPU, profile.MemoryMB)
	}
	return flushTable(w)
}}

var sandboxEventsCmd = cursorCommand("events", true)
var sandboxLogsCmd = cursorCommand("logs", false)

func cursorCommand(name string, events bool) *cobra.Command {
	command := &cobra.Command{Use: name + " <sandbox-id>", Short: "List sandbox " + name, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		after, _ := cmd.Flags().GetString("after")
		follow, _ := cmd.Flags().GetBool("follow")
		output, _ := cmd.Flags().GetString("output")
		if err := outputFormat(output); err != nil {
			return err
		}
		c, err := newSandboxClient()
		if err != nil {
			return err
		}
		for {
			if events {
				items, next, err := c.ListSandboxEvents(commandContext(cmd), args[0], after, defaultPageSize)
				if err != nil {
					return err
				}
				if err := printEvents(items, output, follow); err != nil {
					return err
				}
				after = next
			} else {
				items, next, err := c.ListSandboxLogs(commandContext(cmd), args[0], after, defaultPageSize)
				if err != nil {
					return err
				}
				if err := printLogs(items, output, follow); err != nil {
					return err
				}
				after = next
			}
			if !follow {
				return nil
			}
			select {
			case <-cmd.Context().Done():
				return cmd.Context().Err()
			case <-time.After(time.Second):
			}
		}
	}}
	command.Flags().String("after", "", "Resume after this durable cursor")
	command.Flags().Bool("follow", false, "Follow new entries as NDJSON")
	command.Flags().String("output", "table", "Output format: table or json")
	return command
}

func printEvents(items []client.Event, output string, follow bool) error {
	if output == "json" && !follow {
		return writeJSON(map[string]any{"items": items})
	}
	if output == "json" {
		for _, item := range items {
			if err := writeNDJSON(item); err != nil {
				return err
			}
		}
		return nil
	}
	w := newTableWriter()
	fmt.Fprintln(w, "TIME\tLEVEL\tTYPE\tMESSAGE")
	for _, item := range items {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", item.CreatedAt, item.Level, item.Type, item.Message)
	}
	return flushTable(w)
}
func printLogs(items []client.LogEntry, output string, follow bool) error {
	if output == "json" && !follow {
		return writeJSON(map[string]any{"items": items})
	}
	if output == "json" {
		for _, item := range items {
			if err := writeNDJSON(item); err != nil {
				return err
			}
		}
		return nil
	}
	w := newTableWriter()
	fmt.Fprintln(w, "TIME\tSOURCE\tSTREAM\tMESSAGE")
	for _, item := range items {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", item.CreatedAt, item.Source, item.Stream, item.Message)
	}
	return flushTable(w)
}

func init() {
	rootCmd.AddCommand(sandboxProfilesCmd)
	sandboxProfilesCmd.AddCommand(sandboxProfilesListCmd)
	sandboxProfilesListCmd.Flags().String("output", "table", "Output format: table or json")
	sandboxesCmd.AddCommand(sandboxEventsCmd, sandboxLogsCmd)
}
