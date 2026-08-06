variable "kubeconfig_path" {
  description = "Path to kubeconfig for the target cluster."
  type        = string
  default     = "~/.kube/config"
}

variable "kubeconfig_context" {
  description = "Optional kubeconfig context name."
  type        = string
  default     = ""
}

variable "namespace" {
  description = "Kubernetes namespace for Velane workloads."
  type        = string
  default     = "velane"
}

variable "create_namespace" {
  description = "Whether to create the namespace."
  type        = bool
  default     = true
}

variable "image_pull_policy" {
  description = "Image pull policy for all containers."
  type        = string
  default     = "IfNotPresent"
}

variable "control_plane_image" {
  description = "Container image for control-plane."
  type        = string
}

variable "bun_executor_image" {
  description = "Container image for Bun executor."
  type        = string
}

variable "python_executor_image" {
  description = "Container image for Python executor."
  type        = string
}

variable "admin_image" {
  description = "Container image for admin UI."
  type        = string
}

variable "mcp_server_image" {
  description = "Container image for MCP server."
  type        = string
}

variable "control_plane_replicas" {
  description = "Replica count for control-plane."
  type        = number
  default     = 2
}

variable "bun_executor_replicas" {
  description = "Replica count for Bun executor."
  type        = number
  default     = 2
}

variable "python_executor_replicas" {
  description = "Replica count for Python executor."
  type        = number
  default     = 2
}

variable "admin_replicas" {
  description = "Replica count for admin UI."
  type        = number
  default     = 2
}

variable "mcp_server_replicas" {
  description = "Replica count for MCP server."
  type        = number
  default     = 1
}

variable "admin_service_type" {
  description = "Service type for admin UI."
  type        = string
  default     = "LoadBalancer"
}

variable "control_plane_service_type" {
  description = "Service type for control-plane API."
  type        = string
  default     = "LoadBalancer"
}

variable "mcp_server_service_type" {
  description = "Service type for MCP server."
  type        = string
  default     = "LoadBalancer"
}

variable "database_url" {
  description = "Postgres DSN for control-plane."
  type        = string
  sensitive   = true
}

variable "redis_url" {
  description = "Redis address for control-plane."
  type        = string
}

variable "encryption_key" {
  description = "64-char hex AES key used to encrypt secrets."
  type        = string
  sensitive   = true
}

variable "jwt_private_key_pem" {
  description = "RS256 private key PEM for session tokens."
  type        = string
  sensitive   = true
}

variable "sso_saml_private_key_pem" {
  description = "PEM private key used to sign SAML SP requests and metadata. Rotate together with the certificate after updating each IdP."
  type        = string
  sensitive   = true
  default     = ""
}

variable "sso_saml_certificate_pem" {
  description = "PEM certificate matching sso_saml_private_key_pem."
  type        = string
  sensitive   = true
  default     = ""
}

variable "worker_count" {
  description = "Async worker concurrency for control-plane."
  type        = number
  default     = 5
}

variable "executor_type" {
  description = "Executor type for control-plane (process or firecracker)."
  type        = string
  default     = "process"
}

variable "bootstrap_email" {
  description = "Optional bootstrap admin email (first boot only)."
  type        = string
  default     = ""
}

variable "bootstrap_password" {
  description = "Optional bootstrap admin password (first boot only)."
  type        = string
  default     = ""
  sensitive   = true
}

variable "bootstrap_tenant" {
  description = "Optional bootstrap tenant slug (first boot only)."
  type        = string
  default     = "default"
}

variable "nango_internal_url" {
  description = "Internal Nango API URL, if Nango is deployed separately."
  type        = string
  default     = "http://nango:3003"
}

variable "nango_connect_url" {
  description = "Browser-facing Nango Connect URL."
  type        = string
  default     = ""
}

variable "nango_api_url" {
  description = "Browser-facing Nango API URL."
  type        = string
  default     = ""
}

variable "nango_secret_key" {
  description = "Nango secret key."
  type        = string
  default     = ""
  sensitive   = true
}

variable "nango_public_key" {
  description = "Nango public key for frontend connect."
  type        = string
  default     = ""
  sensitive   = true
}

variable "nango_webhook_secret" {
  description = "Nango webhook signing secret."
  type        = string
  default     = ""
  sensitive   = true
}

variable "clickhouse_dsn" {
  description = "Optional ClickHouse DSN."
  type        = string
  default     = ""
}

variable "logs_bucket" {
  description = "Optional logs bucket name."
  type        = string
  default     = ""
}

variable "replay_bucket" {
  description = "Optional replay bucket name."
  type        = string
  default     = ""
}

variable "object_storage_driver" {
  description = "Object storage driver: s3 or azure."
  type        = string
  default     = "s3"
}

variable "object_storage_bucket" {
  description = "Shared installation bucket or Azure Blob container."
  type        = string
  default     = "velane-data"
}

variable "object_storage_prefix" {
  description = "Optional prefix within the shared bucket/container."
  type        = string
  default     = ""
}

variable "object_storage_s3_region" {
  type    = string
  default = "us-east-1"
}

variable "object_storage_s3_endpoint" {
  description = "Optional S3-compatible endpoint, such as MinIO."
  type        = string
  default     = ""
}

variable "object_storage_s3_force_path_style" {
  type    = bool
  default = false
}

variable "object_storage_azure_account_url" {
  type    = string
  default = ""
}

variable "object_storage_azure_connection_string" {
  type      = string
  default   = ""
  sensitive = true
}

variable "object_storage_access_key_id" {
  type      = string
  default   = ""
  sensitive = true
}

variable "object_storage_secret_access_key" {
  type      = string
  default   = ""
  sensitive = true
}

variable "control_plane_service_account_annotations" {
  description = "Cloud workload-identity annotations for the control-plane service account."
  type        = map(string)
  default     = {}
}

variable "object_gc_grace_period" {
  type    = string
  default = "168h"
}

variable "invocation_retention" {
  type    = string
  default = "0"
}

# ==================== Ingress ====================

variable "enable_ingress" {
  description = "Create an Ingress resource for subdomain-based routing."
  type        = bool
  default     = true
}

variable "ingress_class_name" {
  description = "Ingress class name. Use 'alb' for AWS Application Load Balancer (requires aws-load-balancer-controller installed on the cluster), 'nginx' for NGINX Ingress Controller, or your cloud's equivalent."
  type        = string
  default     = "nginx"
}

variable "base_domain" {
  description = "Base domain for the ingress (e.g. 'yourdomain.com'). Subdomains will be created under this."
  type        = string
  default     = "example.com"
}

variable "admin_subdomain" {
  description = "Subdomain prefix for the admin UI."
  type        = string
  default     = "admin"
}

variable "api_subdomain" {
  description = "Subdomain prefix for the control-plane API."
  type        = string
  default     = "api"
}

variable "mcp_subdomain" {
  description = "Subdomain prefix for the MCP server."
  type        = string
  default     = "mcp"
}

variable "mcp_public_url" {
  description = "Public MCP URL exposed to users (e.g. https://mcp.yourdomain.com/mcp). If empty, it is derived from ingress host when ingress is enabled."
  type        = string
  default     = ""
}

# ==================== Social login (Google / GitHub) ====================

variable "public_base_url" {
  description = "Browser-facing admin portal origin used for OAuth redirect URIs (e.g. https://admin.yourdomain.com). If empty and ingress is enabled, defaults to https://admin_subdomain.base_domain."
  type        = string
  default     = ""
}

variable "google_oauth_client_id" {
  description = "Google OAuth client ID for admin portal sign-in. Leave empty to disable Google login."
  type        = string
  default     = ""
  sensitive   = true
}

variable "google_oauth_client_secret" {
  description = "Google OAuth client secret for admin portal sign-in."
  type        = string
  default     = ""
  sensitive   = true
}

variable "github_oauth_client_id" {
  description = "GitHub OAuth client ID (App) for admin portal sign-in. Leave empty to disable GitHub login."
  type        = string
  default     = ""
  sensitive   = true
}

variable "github_oauth_client_secret" {
  description = "GitHub OAuth client secret for admin portal sign-in."
  type        = string
  default     = ""
  sensitive   = true
}

variable "nango_connect_subdomain" {
  description = "Subdomain prefix for the Nango Connect UI."
  type        = string
  default     = "connect"
}

variable "nango_api_subdomain" {
  description = "Subdomain prefix for the Nango API."
  type        = string
  default     = "nango"
}

variable "ingress_annotations" {
  description = "Extra annotations to put on the Ingress (useful for ALB: alb.ingress.kubernetes.io/scheme, alb.ingress.kubernetes.io/target-type, etc.)."
  type        = map(string)
  default     = {}
}

variable "acm_certificate_arn" {
  description = "ARN of an ACM certificate to attach to the ALB (enables HTTPS on port 443). Leave empty for HTTP-only. Get this from: tofu -chdir=infra/terraform/aws-eks output -raw acm_certificate_arn"
  type        = string
  default     = ""
}

# ==================== Nango (in-cluster) ====================

variable "deploy_nango" {
  description = "Deploy Nango server inside the cluster (recommended for full integrations support)."
  type        = bool
  default     = true
}

variable "nango_image" {
  description = "Nango server container image."
  type        = string
  default     = "nangohq/nango-server:hosted"
}

variable "nango_replicas" {
  description = "Replica count for Nango."
  type        = number
  default     = 1
}

variable "nango_encryption_key" {
  description = "Encryption key for Nango (32 bytes base64). If empty, a default dev key is used (not for production)."
  type        = string
  default     = "6ProXeOGZC0HLT+Kd+2TfneHJmyqcMviCkH8aqwdF4I="
  sensitive   = true
}

# ==================== Nango Database ====================

variable "nango_database_url" {
  description = "Full Postgres DSN for Nango (e.g. postgres://user:pass@host:5432/nango?sslmode=require). If empty and create_nango_database=true, a separate 'nango' database will be created on the same Postgres server used by Velane."
  type        = string
  default     = ""
  sensitive   = true
}

variable "create_nango_database" {
  description = "If nango_database_url is not provided, automatically create a dedicated 'nango' database on the Velane Postgres instance (one-time Job)."
  type        = bool
  default     = true
}

variable "nango_db_name" {
  description = "Name of the separate database to use/create for Nango when nango_database_url is not supplied."
  type        = string
  default     = "nango"
}

variable "velane_cloud" {
  description = "Set VELANE_CLOUD=true on the control-plane to enable cloud-only features (billing UI, tenant plan API)."
  type        = bool
  default     = false
}
