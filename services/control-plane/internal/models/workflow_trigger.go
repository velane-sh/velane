package models

import "time"

type WorkflowTrigger struct {
	ID                string     `json:"id"`
	TenantID          string     `json:"tenant_id"`
	WorkflowID        string     `json:"workflow_id"`
	ConnectionID      string     `json:"connection_id"`
	ProviderConfigKey string     `json:"provider_config_key"`
	Model             string     `json:"model"`
	ChangeTypes       []string   `json:"change_types"`
	Environment       string     `json:"environment"`
	Enabled           bool       `json:"enabled"`
	ActivatedAt       *time.Time `json:"activated_at,omitempty"`
	LastDeliveryAt    *time.Time `json:"last_delivery_at,omitempty"`
	LastError         string     `json:"last_error,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type IntegrationEventReceipt struct {
	ID                string
	DeduplicationKey  string
	TenantID          string
	ConnectionID      string
	ProviderConfigKey string
	Model             string
	ModifiedAfter     string
	InitialSync       bool
	Payload           []byte
	Status            string
	Attempts          int
}

type WorkflowTriggerDispatch struct {
	ID            string   `json:"id"`
	ReceiptID     string   `json:"receipt_id"`
	TriggerID     string   `json:"trigger_id"`
	InvocationIDs []string `json:"invocation_ids"`
	Status        string   `json:"status"`
	Error         string   `json:"error,omitempty"`
}
