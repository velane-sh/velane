package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/abskrj/velane/services/cli/internal/client"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const maxRecipeBytes = 1 << 20

var sandboxImagesCmd = &cobra.Command{Use: "sandbox-images", Short: "Manage sandbox image recipes"}
var sandboxImagesVersionCmd = &cobra.Command{Use: "version", Short: "Manage immutable image recipe versions"}

var sandboxImagesListCmd = &cobra.Command{Use: "list", Short: "List image recipes", RunE: func(cmd *cobra.Command, _ []string) error {
	output, _ := cmd.Flags().GetString("output")
	if err := outputFormat(output); err != nil {
		return err
	}
	c, err := newSandboxClient()
	if err != nil {
		return err
	}
	items, total, err := c.ListSandboxImageRecipes(commandContext(cmd), 0, defaultPageSize)
	if err != nil {
		return err
	}
	if output == "json" {
		return writeJSON(map[string]any{"items": items, "total": total})
	}
	w := newTableWriter()
	fmt.Fprintln(w, "ID\tNAME\tSLUG\tUPDATED")
	for _, item := range items {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", item.ID, item.Name, item.Slug, item.UpdatedAt)
	}
	return flushTable(w)
}}

var sandboxImagesGetCmd = &cobra.Command{Use: "get <recipe-id>", Short: "Get image recipe metadata", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	output, _ := cmd.Flags().GetString("output")
	if err := outputFormat(output); err != nil {
		return err
	}
	c, err := newSandboxClient()
	if err != nil {
		return err
	}
	recipe, err := c.GetSandboxImageRecipe(commandContext(cmd), args[0])
	if err != nil {
		return err
	}
	if output == "json" {
		return writeJSON(recipe)
	}
	fmt.Printf("ID: %s\nName: %s\nDescription: %s\n", recipe.ID, recipe.Name, recipe.Description)
	return nil
}}

var sandboxImagesCreateCmd = &cobra.Command{Use: "create <name>", Short: "Create image recipe metadata", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	description, _ := cmd.Flags().GetString("description")
	slug, _ := cmd.Flags().GetString("slug")
	output, _ := cmd.Flags().GetString("output")
	if err := outputFormat(output); err != nil {
		return err
	}
	c, err := newSandboxClient()
	if err != nil {
		return err
	}
	if err := c.RequireSandboxCapability(commandContext(cmd), "sandbox_image_recipes"); err != nil {
		return err
	}
	key, err := mutationKey(cmd)
	if err != nil {
		return err
	}
	recipe, err := c.CreateSandboxImageRecipe(commandContext(cmd), args[0], slug, description, key)
	if err != nil {
		return err
	}
	if output == "json" {
		return writeJSON(recipe)
	}
	fmt.Printf("Created image recipe %s\n", recipe.ID)
	return nil
}}

var sandboxImagesDeleteCmd = &cobra.Command{Use: "delete <recipe-id>", Short: "Delete an unreferenced image recipe", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	yes, _ := cmd.Flags().GetBool("yes")
	if err := requireConfirmation(yes, "image recipe deletion"); err != nil {
		return err
	}
	c, err := newSandboxClient()
	if err != nil {
		return err
	}
	if err := c.RequireSandboxCapability(commandContext(cmd), "sandbox_image_recipes"); err != nil {
		return err
	}
	key, err := mutationKey(cmd)
	if err != nil {
		return err
	}
	if err := c.DeleteSandboxImageRecipe(commandContext(cmd), args[0], key); err != nil {
		return err
	}
	fmt.Println("Image recipe deleted.")
	return nil
}}

var sandboxImagesVersionCreateCmd = &cobra.Command{Use: "create <recipe-id> --file <recipe.yaml>", Short: "Build an immutable image recipe version from strict YAML", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	file, _ := cmd.Flags().GetString("file")
	if file == "" {
		return fmt.Errorf("--file is required")
	}
	document, err := readStrictYAML(file)
	if err != nil {
		return err
	}
	return runMutation(cmd, "sandbox_image_recipes", func(ctx context.Context, c *client.Client, key, _ string) (*client.Operation, error) {
		_, operation, err := c.CreateSandboxImageRecipeVersion(ctx, args[0], document, key)
		return operation, err
	})
}}

var sandboxImagesVersionGetCmd = &cobra.Command{Use: "get <recipe-id> <version>", Short: "Get immutable image recipe version", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
	version, err := strconv.Atoi(args[1])
	if err != nil || version < 1 {
		return fmt.Errorf("version must be a positive integer")
	}
	output, _ := cmd.Flags().GetString("output")
	if err := outputFormat(output); err != nil {
		return err
	}
	c, err := newSandboxClient()
	if err != nil {
		return err
	}
	item, err := c.GetSandboxImageRecipeVersion(commandContext(cmd), args[0], version)
	if err != nil {
		return err
	}
	if output == "json" {
		return writeJSON(item)
	}
	fmt.Printf("Version: %d\nStatus: %s\n", item.VersionNumber, item.Status)
	return nil
}}

func sandboxImageVersionActivityCommand(resource string, events bool) *cobra.Command {
	command := &cobra.Command{Use: resource + " <recipe-id> <version>", Short: "List image recipe version " + resource, Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		version, err := strconv.Atoi(args[1])
		if err != nil || version < 1 {
			return fmt.Errorf("version must be a positive integer")
		}
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
				items, next, err := c.ListSandboxImageRecipeVersionEvents(commandContext(cmd), args[0], version, after, defaultPageSize)
				if err != nil {
					return err
				}
				if err := printEvents(items, output, follow); err != nil {
					return err
				}
				after = next
			} else {
				items, next, err := c.ListSandboxImageRecipeVersionLogs(commandContext(cmd), args[0], version, after, defaultPageSize)
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

func readStrictYAML(path string) (any, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open recipe: %w", err)
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, maxRecipeBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read recipe YAML: %w", err)
	}
	if len(contents) > maxRecipeBytes {
		return nil, fmt.Errorf("recipe YAML exceeds %d byte limit", maxRecipeBytes)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	var node yaml.Node
	if err := decoder.Decode(&node); err != nil {
		return nil, fmt.Errorf("decode recipe YAML: %w", err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("recipe YAML must contain exactly one document")
		}
		return nil, fmt.Errorf("decode recipe YAML: %w", err)
	}
	if err := validateYAMLNode(&node); err != nil {
		return nil, err
	}
	var value any
	if err := node.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode recipe YAML: %w", err)
	}
	normalized, err := jsonCompatibleYAML(value)
	if err != nil {
		return nil, err
	}
	if _, err := json.Marshal(normalized); err != nil {
		return nil, fmt.Errorf("recipe must be JSON-compatible: %w", err)
	}
	return normalized, nil
}

func validateYAMLNode(node *yaml.Node) error {
	switch node.Kind {
	case yaml.AliasNode:
		return fmt.Errorf("recipe YAML aliases are not allowed")
	case yaml.MappingNode:
		seen := map[string]bool{}
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode {
				return fmt.Errorf("recipe YAML mapping keys must be strings")
			}
			if seen[key.Value] {
				return fmt.Errorf("recipe YAML contains duplicate key %q", key.Value)
			}
			seen[key.Value] = true
			if err := validateYAMLNode(node.Content[i+1]); err != nil {
				return err
			}
		}
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range node.Content {
			if err := validateYAMLNode(child); err != nil {
				return err
			}
		}
	}
	return nil
}
func jsonCompatibleYAML(value any) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			normalized, err := jsonCompatibleYAML(child)
			if err != nil {
				return nil, err
			}
			result[key] = normalized
		}
		return result, nil
	case []any:
		result := make([]any, len(typed))
		for i, child := range typed {
			normalized, err := jsonCompatibleYAML(child)
			if err != nil {
				return nil, err
			}
			result[i] = normalized
		}
		return result, nil
	case nil, string, bool, int, int64, uint64, float64:
		return typed, nil
	default:
		return nil, fmt.Errorf("recipe YAML has unsupported value type %T", value)
	}
}
func mutationKey(cmd *cobra.Command) (string, error) {
	key, _ := cmd.Flags().GetString("idempotency-key")
	if key != "" {
		return key, nil
	}
	return newUUID()
}

func init() {
	rootCmd.AddCommand(sandboxImagesCmd)
	sandboxImagesCmd.AddCommand(sandboxImagesListCmd, sandboxImagesGetCmd, sandboxImagesCreateCmd, sandboxImagesDeleteCmd, sandboxImagesVersionCmd)
	sandboxImagesVersionCmd.AddCommand(sandboxImagesVersionCreateCmd, sandboxImagesVersionGetCmd, sandboxImageVersionActivityCommand("events", true), sandboxImageVersionActivityCommand("logs", false))
	for _, command := range []*cobra.Command{sandboxImagesListCmd, sandboxImagesGetCmd, sandboxImagesVersionGetCmd} {
		command.Flags().String("output", "table", "Output format: table or json")
	}
	for _, command := range []*cobra.Command{sandboxImagesCreateCmd, sandboxImagesDeleteCmd} {
		command.Flags().String("idempotency-key", "", "Idempotency key for this mutation")
	}
	sandboxImagesCreateCmd.Flags().String("description", "", "Recipe description")
	sandboxImagesCreateCmd.Flags().String("slug", "", "Optional immutable recipe slug")
	sandboxImagesCreateCmd.Flags().String("output", "table", "Output format: table or json")
	sandboxImagesDeleteCmd.Flags().Bool("yes", false, "Confirm deletion")
	addMutationFlags(sandboxImagesVersionCreateCmd)
	sandboxImagesVersionCreateCmd.Flags().String("file", "", "Strict YAML recipe file")
}
