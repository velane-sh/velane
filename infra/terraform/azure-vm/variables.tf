variable "subscription_id" {
  description = "Azure subscription ID."
  type        = string
}

variable "location" {
  description = "Azure region for all resources."
  type        = string
  default     = "westus"
}

variable "name_prefix" {
  description = "Prefix used for Azure resource names."
  type        = string
  default     = "velane-prod"
}

variable "vm_size" {
  description = "Azure VM SKU for the Docker host."
  type        = string
  default     = "Standard_D2s_v3"
}

variable "admin_username" {
  description = "Linux administrator username."
  type        = string
  default     = "velaneadmin"
}

variable "ssh_public_key" {
  description = "OpenSSH public key used to access the VM."
  type        = string
  sensitive   = true
}

variable "ssh_allowed_cidrs" {
  description = "CIDRs permitted to access SSH. Keep empty to disable public SSH."
  type        = list(string)
  default     = []
}

variable "base_domain" {
  description = "Production base domain."
  type        = string
  default     = "velane.sh"
}

variable "admin_subdomain" {
  type    = string
  default = "app"
}

variable "api_subdomain" {
  type    = string
  default = "api"
}

variable "mcp_subdomain" {
  type    = string
  default = "mcp"
}

variable "nango_connect_subdomain" {
  type    = string
  default = "connect"
}

variable "nango_api_subdomain" {
  type    = string
  default = "nango"
}

variable "control_plane_image" {
  type    = string
  default = "ghcr.io/abskrj/velane-control-plane:0.8.0"
}

variable "bun_executor_image" {
  type    = string
  default = "ghcr.io/abskrj/velane-bun-executor:0.8.0"
}

variable "python_executor_image" {
  type    = string
  default = "ghcr.io/abskrj/velane-python-executor:0.8.0"
}

variable "admin_image" {
  type    = string
  default = "ghcr.io/abskrj/velane-admin:azure-ui-stream-fix-20260723"
}

variable "mcp_server_image" {
  type    = string
  default = "ghcr.io/abskrj/velane-mcp-server:0.8.0"
}

variable "nango_image" {
  type    = string
  default = "nangohq/nango-server:hosted"
}

variable "license_server_image" {
  description = "Immutable image for the shared-VM licensing service."
  type        = string
  default     = "ghcr.io/abskrj/velane-cloud-server:sha-7763357"
}

variable "private_key_pem" {
  description = "Existing licensing Ed25519 signing key; it must not change during migration."
  type        = string
  sensitive   = true
}

variable "ghcr_token" {
  description = "GitHub token with permission to pull the private licensing image."
  type        = string
  sensitive   = true
}

variable "ghcr_username" {
  description = "GitHub account used to pull the private licensing image."
  type        = string
  default     = "abskrj"
}

variable "redis_url" {
  description = "Existing external Redis DSN."
  type        = string
  sensitive   = true
}

variable "encryption_key" {
  description = "Existing Velane encryption key; it must not change during migration."
  type        = string
  sensitive   = true
}

variable "jwt_private_key_pem" {
  description = "Existing session JWT private key; it must not change during migration."
  type        = string
  sensitive   = true
}

variable "nango_encryption_key" {
  description = "Existing Nango encryption key; it must not change during migration."
  type        = string
  sensitive   = true
}

variable "nango_secret_key" {
  type      = string
  sensitive = true
}

variable "nango_public_key" {
  type      = string
  sensitive = true
}

variable "nango_webhook_secret" {
  type      = string
  sensitive = true
  default   = ""
}

variable "google_oauth_client_id" {
  type    = string
  default = ""
}

variable "google_oauth_client_secret" {
  type      = string
  sensitive = true
  default   = ""
}

variable "github_oauth_client_id" {
  type    = string
  default = ""
}

variable "github_oauth_client_secret" {
  type      = string
  sensitive = true
  default   = ""
}

variable "worker_count" {
  type    = number
  default = 5
}

variable "object_gc_grace_period" {
  description = "Delay before objects belonging to a deleted workflow are garbage-collected."
  type        = string
  default     = "168h"
}

variable "invocation_retention" {
  description = "Invocation payload retention duration. Zero disables automatic deletion."
  type        = string
  default     = "0"
}

variable "postgres_sku_name" {
  description = "Azure PostgreSQL Flexible Server compute SKU."
  type        = string
  default     = "B_Standard_B1ms"
}

variable "postgres_storage_mb" {
  description = "PostgreSQL allocated storage in MiB."
  type        = number
  default     = 32768
}

variable "postgres_version" {
  type    = string
  default = "16"
}

variable "postgres_admin_username" {
  type    = string
  default = "velaneadmin"
}
