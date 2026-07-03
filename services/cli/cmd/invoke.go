package cmd

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/abskrj/velane/services/cli/internal/client"
	"github.com/abskrj/velane/services/cli/internal/keyring"
	"github.com/abskrj/velane/services/cli/internal/telemetry"
	"github.com/spf13/cobra"
)

var invokeCmd = &cobra.Command{
	Use:   "invoke <slug-or-id>",
	Short: "Invoke a snippet",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		snippetSlug := args[0]
		env, _ := cmd.Flags().GetString("env")
		input, _ := cmd.Flags().GetString("input")
		stream, _ := cmd.Flags().GetBool("stream")

		key, err := keyring.LoadAPIKey()
		if err != nil {
			return fmt.Errorf("no API key found — run: velane login --key <key>")
		}
		if tenantSlug == "" {
			return fmt.Errorf("--tenant flag is required")
		}

		if stream {
			err := invokeStream(apiURL, tenantSlug, snippetSlug, env, input, key)
			telemetry.Fire("cli.invoke", map[string]any{"streaming": true, "error": err != nil})
			return err
		}

		start := time.Now()
		c := client.New(apiURL, tenantSlug, key)
		result, err := c.Invoke(context.Background(), tenantSlug, snippetSlug, env, input)
		telemetry.Fire("cli.invoke", map[string]any{
			"streaming":   false,
			"error":       err != nil,
			"duration_ms": time.Since(start).Milliseconds(),
		})
		if err != nil {
			return err
		}

		fmt.Printf("Invocation ID: %s\n", result.InvocationID)
		fmt.Printf("Status:        %s\n", result.Status)
		fmt.Printf("Duration:      %dms\n", result.DurationMs)
		if result.Error != "" {
			fmt.Printf("Error:         %s\n", result.Error)
		}
		fmt.Printf("Output:        %v\n", result.Output)
		if result.Stderr != "" {
			fmt.Printf("Stderr:\n%s\n", result.Stderr)
		}
		return nil
	},
}

func invokeStream(baseURL, tenant, slug, env, input, key string) error {
	url := fmt.Sprintf("%s/v1/invoke/%s/%s?env=%s", baseURL, tenant, slug, env)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Invoke-Mode", "stream")
	req.Header.Set("Accept", "text/event-stream")
	req.Body = http.NoBody

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d from server", resp.StatusCode)
	}

	fmt.Printf("Invocation ID: %s\n", resp.Header.Get("X-Invocation-Id"))
	fmt.Println("Streaming output:")

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			fmt.Println(line)
		}
	}
	return scanner.Err()
}

func init() {
	invokeCmd.Flags().String("env", "prod", "Environment to invoke (dev|staging|prod)")
	invokeCmd.Flags().String("input", "{}", "JSON input payload")
	invokeCmd.Flags().Bool("stream", false, "Stream output via SSE")
}
