package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-resty/resty/v2"
)

// SandboxImageRecipe is safe public image recipe metadata.
type SandboxImageRecipe struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// SandboxImageRecipeVersion is an immutable image recipe version summary.
type SandboxImageRecipeVersion struct {
	ID             string          `json:"id"`
	RecipeID       string          `json:"recipe_id"`
	VersionNumber  int             `json:"version_number"`
	SchemaVersion  string          `json:"schema_version"`
	Status         string          `json:"status"`
	FailureCode    string          `json:"failure_code"`
	FailureMessage string          `json:"failure_message"`
	Document       json.RawMessage `json:"document,omitempty"`
	CreatedAt      string          `json:"created_at"`
}

type recipeMutationResponse struct {
	Recipe    *SandboxImageRecipe        `json:"recipe"`
	Version   *SandboxImageRecipeVersion `json:"version"`
	Operation *Operation                 `json:"operation"`
}

func (c *Client) ListSandboxImageRecipes(ctx context.Context, offset, limit int) ([]SandboxImageRecipe, int, error) {
	var response listResponse[SandboxImageRecipe]
	path := fmt.Sprintf("/v1/sandbox-image-recipes?offset=%d&limit=%d", offset, limit)
	if err := c.request(ctx, resty.MethodGet, path, nil, &response, nil); err != nil {
		return nil, 0, err
	}
	return response.Items, response.Total, nil
}

func (c *Client) GetSandboxImageRecipe(ctx context.Context, recipeID string) (*SandboxImageRecipe, error) {
	var recipe SandboxImageRecipe
	if err := c.request(ctx, resty.MethodGet, "/v1/sandbox-image-recipes/"+url.PathEscape(recipeID), nil, &recipe, nil); err != nil {
		return nil, err
	}
	return &recipe, nil
}

// CreateSandboxImageRecipe creates public recipe metadata. An empty slug lets
// the control plane derive the canonical slug from the name.
func (c *Client) CreateSandboxImageRecipe(ctx context.Context, name, slug, description, key string) (*SandboxImageRecipe, error) {
	if err := ValidateIdempotencyKey(key); err != nil {
		return nil, err
	}
	var response recipeMutationResponse
	body := map[string]string{"name": name, "description": description}
	if slug != "" {
		body["slug"] = slug
	}
	if _, err := c.requestStatus(ctx, resty.MethodPost, "/v1/sandbox-image-recipes", body, &response, map[string]string{"Idempotency-Key": key}, http.StatusCreated); err != nil {
		return nil, err
	}
	if response.Recipe == nil {
		return nil, fmt.Errorf("API response did not include a recipe")
	}
	return response.Recipe, nil
}

func (c *Client) DeleteSandboxImageRecipe(ctx context.Context, recipeID, key string) error {
	if err := ValidateIdempotencyKey(key); err != nil {
		return err
	}
	_, err := c.requestStatus(ctx, resty.MethodDelete, "/v1/sandbox-image-recipes/"+url.PathEscape(recipeID), nil, nil, map[string]string{"Idempotency-Key": key}, http.StatusNoContent)
	return err
}

func (c *Client) GetSandboxImageRecipeVersion(ctx context.Context, recipeID string, version int) (*SandboxImageRecipeVersion, error) {
	var recipeVersion SandboxImageRecipeVersion
	path := fmt.Sprintf("/v1/sandbox-image-recipes/%s/versions/%d", url.PathEscape(recipeID), version)
	if err := c.request(ctx, resty.MethodGet, path, nil, &recipeVersion, nil); err != nil {
		return nil, err
	}
	return &recipeVersion, nil
}

// CreateSandboxImageRecipeVersion submits canonical JSON derived from strict YAML input.
func (c *Client) CreateSandboxImageRecipeVersion(ctx context.Context, recipeID string, document any, key string) (*SandboxImageRecipeVersion, *Operation, error) {
	if err := ValidateIdempotencyKey(key); err != nil {
		return nil, nil, err
	}
	var response recipeMutationResponse
	path := "/v1/sandbox-image-recipes/" + url.PathEscape(recipeID) + "/versions"
	if _, err := c.requestStatus(ctx, resty.MethodPost, path, document, &response, map[string]string{"Idempotency-Key": key}, http.StatusAccepted); err != nil {
		return nil, nil, err
	}
	if response.Version == nil || response.Operation == nil {
		return nil, nil, fmt.Errorf("API response did not include a recipe version and build operation")
	}
	return response.Version, response.Operation, nil
}

func (c *Client) ListSandboxImageRecipeVersionEvents(ctx context.Context, recipeID string, version int, after string, limit int) ([]Event, string, error) {
	var response listResponse[Event]
	path := recipeVersionCursorPath(recipeID, version, "events", after, limit)
	if err := c.request(ctx, resty.MethodGet, path, nil, &response, nil); err != nil {
		return nil, "", err
	}
	return response.Items, response.NextCursor, nil
}

func (c *Client) ListSandboxImageRecipeVersionLogs(ctx context.Context, recipeID string, version int, after string, limit int) ([]LogEntry, string, error) {
	var response listResponse[LogEntry]
	path := recipeVersionCursorPath(recipeID, version, "logs", after, limit)
	if err := c.request(ctx, resty.MethodGet, path, nil, &response, nil); err != nil {
		return nil, "", err
	}
	return response.Items, response.NextCursor, nil
}

func recipeVersionCursorPath(recipeID string, version int, resource, after string, limit int) string {
	values := url.Values{}
	if after != "" {
		values.Set("after", after)
	}
	values.Set("limit", strconv.Itoa(limit))
	return fmt.Sprintf("/v1/sandbox-image-recipes/%s/versions/%d/%s?%s", url.PathEscape(recipeID), version, resource, values.Encode())
}
