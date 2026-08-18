variable "region" {
  description = "AWS region for this isolated sandbox-host pool."
  type        = string
}

variable "sandbox_host_pool_id" {
  description = "Private control-plane pool ID this homogeneous ASG is authorized to enroll into. This is an identifier, not a credential or certificate."
  type        = string

  validation {
    condition     = length(trimspace(var.sandbox_host_pool_id)) > 0
    error_message = "sandbox_host_pool_id must be set."
  }
}

variable "name_prefix" {
  description = "Operator-private prefix used for sandbox-host resource names."
  type        = string
  default     = "velane-sandbox"

  validation {
    condition     = can(regex("^[a-z0-9-]{1,40}$", var.name_prefix))
    error_message = "name_prefix must contain only lowercase letters, numbers, and hyphens and be at most 40 characters."
  }
}

variable "vpc_id" {
  description = "Existing VPC in which the dedicated sandbox-host subnets and private endpoints are created."
  type        = string
}

variable "vpc_cidr" {
  description = "CIDR of vpc_id. Used only for the resolver egress rule; it must match the VPC."
  type        = string
}

variable "private_subnet_cidrs" {
  description = "Dedicated sandbox-host private subnet CIDRs keyed by availability zone. These subnets receive no public IPs and this stack creates their route tables."
  type        = map(string)

  validation {
    condition     = length(var.private_subnet_cidrs) >= 2
    error_message = "Provide dedicated private subnets in at least two availability zones for the private NLB."
  }
}

variable "ami_id" {
  description = "Immutable, promoted sandbox-host AMI ID. Do not supply an alias or a moving image reference."
  type        = string

  validation {
    condition     = can(regex("^ami-[0-9a-f]+$", var.ami_id))
    error_message = "ami_id must be a concrete AMI ID (ami-...)."
  }
}

variable "instance_type" {
  description = "One KVM-qualified EC2 instance type for this homogeneous ASG. Mixed instances are intentionally unsupported."
  type        = string

  validation {
    condition     = length(trimspace(var.instance_type)) > 0
    error_message = "instance_type must be set to one qualified instance type."
  }
}

variable "host_lineage_id" {
  description = "New, never-reused immutable lineage ID represented by this ASG. Keep operator-private."
  type        = string
  sensitive   = true

  validation {
    condition     = length(trimspace(var.host_lineage_id)) > 0
    error_message = "host_lineage_id must be set."
  }
}

variable "host_compatibility_key" {
  description = "Canonical immutable host compatibility key for this ASG. A change requires a new lineage and ASG. Keep operator-private."
  type        = string
  sensitive   = true

  validation {
    condition     = length(trimspace(var.host_compatibility_key)) > 0
    error_message = "host_compatibility_key must be set."
  }
}

variable "host_control_tls_server_name" {
  description = "Private DNS name expected by the host-control API TLS certificate. The NLB passes TCP through for end-to-end mTLS."
  type        = string

  validation {
    condition     = can(regex("^[A-Za-z0-9.-]+$", var.host_control_tls_server_name))
    error_message = "host_control_tls_server_name must be a DNS name, not a URL."
  }
}

variable "host_control_ca_bundle_path" {
  description = "Absolute path in the immutable host AMI to the PEM CA bundle used to verify the private host-control server certificate. Terraform never receives CA contents."
  type        = string
  default     = "/etc/velane-sandbox-host/tls/control-plane-ca.pem"

  validation {
    condition     = startswith(var.host_control_ca_bundle_path, "/")
    error_message = "host_control_ca_bundle_path must be an absolute path in the immutable host AMI."
  }
}

variable "host_client_certificate_path" {
  description = "Absolute path where enrollment persists the short-lived host mTLS certificate. The certificate is not supplied to Terraform or user data."
  type        = string
  default     = "/var/lib/velane-sandbox-agent/identity/client.crt"

  validation {
    condition     = startswith(var.host_client_certificate_path, "/")
    error_message = "host_client_certificate_path must be an absolute path."
  }
}

variable "host_client_private_key_path" {
  description = "Absolute path where enrollment persists the host mTLS private key. The key is locally generated and never supplied to Terraform or user data."
  type        = string
  default     = "/var/lib/velane-sandbox-agent/identity/client.key"
  sensitive   = true

  validation {
    condition     = startswith(var.host_client_private_key_path, "/")
    error_message = "host_client_private_key_path must be an absolute path."
  }
}

variable "watchdog_public_key_path" {
  description = "Absolute path in the immutable host AMI to a root-owned file containing the base64-encoded Ed25519 public key used to verify control-plane watchdog grants. Key material is not passed through Terraform or user data."
  type        = string
  default     = "/etc/velane-sandbox-host/watchdog-public-key.base64"

  validation {
    condition     = startswith(var.watchdog_public_key_path, "/")
    error_message = "watchdog_public_key_path must be an absolute path in the immutable host AMI."
  }
}

variable "sandbox_agent_jailer_uid" {
  description = "Dedicated non-root UID baked into the immutable AMI for jailed Firecracker processes."
  type        = number
  default     = 10001
  validation {
    condition     = var.sandbox_agent_jailer_uid > 0
    error_message = "sandbox_agent_jailer_uid must be positive."
  }
}

variable "sandbox_agent_jailer_gid" {
  description = "Dedicated non-root GID baked into the immutable AMI for jailed Firecracker processes and watchdog socket access."
  type        = number
  default     = 10001
  validation {
    condition     = var.sandbox_agent_jailer_gid > 0
    error_message = "sandbox_agent_jailer_gid must be positive."
  }
}

variable "host_control_server_certificate_secret_arn" {
  description = "Reference only to the private control-plane secret containing its host-control TLS certificate and key. TCP NLB pass-through means this is consumed by the backend listener, never by the NLB or host instances."
  type        = string
  sensitive   = true

  validation {
    condition     = can(regex("^arn:[^:]+:secretsmanager:[^:]+:[0-9]{12}:secret:.+", var.host_control_server_certificate_secret_arn))
    error_message = "host_control_server_certificate_secret_arn must be a Secrets Manager secret ARN; do not pass certificate material through Terraform."
  }
}

variable "host_control_client_ca_secret_arn" {
  description = "Reference only to the private control-plane trust-store secret for validating host mTLS client certificates. It is consumed by the backend listener, never by the NLB or host instances."
  type        = string
  sensitive   = true

  validation {
    condition     = can(regex("^arn:[^:]+:secretsmanager:[^:]+:[0-9]{12}:secret:.+", var.host_control_client_ca_secret_arn))
    error_message = "host_control_client_ca_secret_arn must be a Secrets Manager secret ARN; do not pass CA material through Terraform."
  }
}

variable "host_api_target_ips" {
  description = "Private IP addresses of already-private host-control API backends to register behind the NLB. Empty keeps the endpoint intentionally unavailable."
  type        = set(string)
  default     = []
}

variable "host_api_port" {
  description = "Private host-control API TCP port."
  type        = number
  default     = 8443

  validation {
    condition     = var.host_api_port > 0 && var.host_api_port < 65536
    error_message = "host_api_port must be a valid TCP port."
  }
}

variable "root_volume_size_gib" {
  description = "Encrypted gp3 root-volume size in GiB for sandbox hosts."
  type        = number
  default     = 100

  validation {
    condition     = var.root_volume_size_gib >= 40
    error_message = "root_volume_size_gib must be at least 40 GiB."
  }
}

variable "root_volume_iops" {
  description = "Provisioned IOPS for the encrypted gp3 root volume."
  type        = number
  default     = 6000
}

variable "root_volume_throughput" {
  description = "Provisioned throughput in MiB/s for the encrypted gp3 root volume."
  type        = number
  default     = 250
}

variable "min_hosts" {
  description = "Minimum sandbox hosts. Defaults to zero so applying this stack does not activate capacity."
  type        = number
  default     = 0
}

variable "desired_hosts" {
  description = "Initial desired sandbox-host capacity. Defaults to zero; the capacity adapter should change it only after registration is ready."
  type        = number
  default     = 0
}

variable "max_hosts" {
  description = "Maximum hosts for this one immutable lineage. Defaults to zero to keep the stack disabled."
  type        = number
  default     = 0
}

variable "lifecycle_heartbeat_timeout_seconds" {
  description = "Lifecycle-hook heartbeat interval used by the control-plane drain worker."
  type        = number
  default     = 300

  validation {
    condition     = var.lifecycle_heartbeat_timeout_seconds >= 30 && var.lifecycle_heartbeat_timeout_seconds <= 3600
    error_message = "lifecycle_heartbeat_timeout_seconds must be between 30 and 3600."
  }
}

variable "control_plane_principal_arns" {
  description = "IAM principals used by the private control plane. They receive scoped KMS key-policy access; attach the exported control-plane policy separately."
  type        = set(string)
  default     = []
}

variable "deleted_artifact_retention_days" {
  description = "How long objects explicitly tagged sandbox-snapshot-state=deleted remain before removal."
  type        = number
  default     = 30

  validation {
    condition     = var.deleted_artifact_retention_days >= 1
    error_message = "deleted_artifact_retention_days must be at least one day."
  }
}

variable "access_log_retention_days" {
  description = "Retention period for S3 server access logs."
  type        = number
  default     = 90

  validation {
    condition     = var.access_log_retention_days >= 30
    error_message = "access_log_retention_days must be at least 30 days."
  }
}

variable "tags" {
  description = "Additional non-sensitive tags. Never add tenant, host compatibility, or credentials here."
  type        = map(string)
  default     = {}
}
