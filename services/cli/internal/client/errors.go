package client

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// APIError is a sanitized error returned by the Velane public API.
type APIError struct {
	StatusCode  int             `json:"-"`
	Message     string          `json:"error"`
	Code        string          `json:"code"`
	Retryable   bool            `json:"retryable"`
	OperationID string          `json:"operation_id"`
	Details     json.RawMessage `json:"details"`
	RetryAfter  int             `json:"-"`
}

func (e *APIError) Error() string {
	message := e.Message
	if message == "" {
		message = "request failed"
	}
	if e.Code != "" {
		return fmt.Sprintf("API error %d (%s): %s", e.StatusCode, e.Code, message)
	}
	return fmt.Sprintf("API error %d: %s", e.StatusCode, message)
}

func parseAPIError(status int, body string, retryAfter string) error {
	apiErr := &APIError{StatusCode: status}
	if err := json.Unmarshal([]byte(body), apiErr); err != nil || apiErr.Message == "" {
		apiErr.Message = strings.TrimSpace(body)
	}
	if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds > 0 {
		apiErr.RetryAfter = seconds
	}
	return apiErr
}

// ValidateIdempotencyKey validates the public API's visible-ASCII key contract.
func ValidateIdempotencyKey(key string) error {
	if len(key) < 1 || len(key) > 128 {
		return fmt.Errorf("idempotency key must be 1–128 visible ASCII characters")
	}
	for _, character := range key {
		if character < 0x21 || character > 0x7e {
			return fmt.Errorf("idempotency key must be 1–128 visible ASCII characters")
		}
	}
	return nil
}
