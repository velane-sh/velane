package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
)

func outputFormat(cmdOutput string) error {
	if cmdOutput != "table" && cmdOutput != "json" {
		return fmt.Errorf("--output must be table or json")
	}
	return nil
}

func writeJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func writeNDJSON(value any) error {
	return json.NewEncoder(os.Stdout).Encode(value)
}

func newTableWriter() *tabwriter.Writer {
	return tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
}

func flushTable(w *tabwriter.Writer) error { return w.Flush() }

func printOperation(operationID string, output string) error {
	if output == "json" {
		return writeJSON(map[string]string{"operation_id": operationID})
	}
	_, err := fmt.Fprintf(os.Stdout, "Operation: %s\n", operationID)
	return err
}

func requireConfirmation(yes bool, action string) error {
	if !yes {
		return fmt.Errorf("%s requires --yes in non-interactive mode", action)
	}
	return nil
}

func writeTo(out io.Writer, format string, value any) error {
	if format != "json" {
		return fmt.Errorf("unsupported output format %q", format)
	}
	return json.NewEncoder(out).Encode(value)
}
