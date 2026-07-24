variable "region" {
  description = "AWS region for the EKS deployment."
  type        = string
  default     = "us-east-1"
}

variable "name_prefix" {
  description = "Prefix used when constructing resource names."
  type        = string
  default     = "velane"
}

variable "velane_namespace" {
  description = "Kubernetes namespace used by the Velane workload module."
  type        = string
  default     = "velane"
}

variable "cluster_name" {
  description = "Explicit EKS cluster name. Leave empty to derive one from name_prefix and region."
  type        = string
  default     = ""
}

variable "cluster_version" {
  description = "EKS Kubernetes version."
  type        = string
  default     = "1.35"
}

variable "vpc_cidr" {
  description = "CIDR block for the EKS VPC."
  type        = string
  default     = "10.42.0.0/16"
}

variable "availability_zone_count" {
  description = "How many availability zones to spread subnets across."
  type        = number
  default     = 2

  validation {
    condition     = var.availability_zone_count >= 2 && var.availability_zone_count <= 4
    error_message = "availability_zone_count must be between 2 and 4."
  }
}

variable "cluster_endpoint_public_access" {
  description = "Expose the Kubernetes API server publicly."
  type        = bool
  default     = true
}

variable "cluster_endpoint_private_access" {
  description = "Expose the Kubernetes API server privately inside the VPC."
  type        = bool
  default     = true
}

variable "public_access_cidrs" {
  description = "CIDR blocks allowed to reach the public Kubernetes API endpoint."
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

variable "node_group_name" {
  description = "Name for the primary managed node group."
  type        = string
  default     = "primary"
}

variable "node_instance_types" {
  description = "EC2 instance types for the primary node group."
  type        = list(string)
  default     = ["t3.large"]
}

variable "node_capacity_type" {
  description = "Capacity type for the node group."
  type        = string
  default     = "ON_DEMAND"
}

variable "node_disk_size" {
  description = "Disk size in GiB for each worker node."
  type        = number
  default     = 50
}

variable "node_desired_size" {
  description = "Desired node count for the primary node group."
  type        = number
  default     = 2
}

variable "node_min_size" {
  description = "Minimum node count for the primary node group."
  type        = number
  default     = 1
}

variable "node_max_size" {
  description = "Maximum node count for the primary node group."
  type        = number
  default     = 3
}

variable "tags" {
  description = "Extra tags applied to all AWS resources."
  type        = map(string)
  default     = {}
}

variable "domain" {
  description = "Root domain for the deployment (e.g. 'velane.sh'). When set, an ACM wildcard certificate for *.DOMAIN is requested and an IAM role for the AWS Load Balancer Controller is created. Leave empty to skip both."
  type        = string
  default     = ""
}
