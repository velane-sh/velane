output "region" {
  description = "AWS region for the cluster."
  value       = var.region
}

output "cluster_name" {
  description = "EKS cluster name."
  value       = aws_eks_cluster.main.name
}

output "cluster_endpoint" {
  description = "Kubernetes API server endpoint."
  value       = aws_eks_cluster.main.endpoint
}

output "cluster_version" {
  description = "Kubernetes version running on the EKS control plane."
  value       = aws_eks_cluster.main.version
}

output "vpc_id" {
  description = "VPC created for the EKS deployment."
  value       = aws_vpc.main.id
}

output "public_subnet_ids" {
  description = "Public subnet IDs used by the node group and public load balancers."
  value       = [for subnet in aws_subnet.public : subnet.id]
}

output "private_subnet_ids" {
  description = "Private subnet IDs reserved for future private workloads."
  value       = [for subnet in aws_subnet.private : subnet.id]
}

output "cluster_security_group_id" {
  description = "Security group attached to the EKS control plane."
  value       = aws_security_group.cluster.id
}

output "node_security_group_id" {
  description = "Security group attached to the worker nodes."
  value       = aws_security_group.nodes.id
}

output "node_group_name" {
  description = "Managed node group name."
  value       = aws_eks_node_group.primary.node_group_name
}

output "kubeconfig_context_name" {
  description = "Recommended kubeconfig context alias to use with the Kubernetes app stack."
  value       = aws_eks_cluster.main.name
}

output "update_kubeconfig_command" {
  description = "Command to merge this EKS cluster into your local kubeconfig with a stable context alias."
  value       = "aws eks update-kubeconfig --region ${var.region} --name ${aws_eks_cluster.main.name} --alias ${aws_eks_cluster.main.name}"
}

output "oidc_provider_arn" {
  description = "ARN of the OIDC provider created for IRSA."
  value       = aws_iam_openid_connect_provider.eks.arn
}

output "alb_controller_role_arn" {
  description = "IAM role ARN to annotate on the aws-load-balancer-controller service account."
  value       = aws_iam_role.alb_controller.arn
}

output "acm_certificate_arn" {
  description = "ARN of the ACM wildcard certificate (empty when domain is not set)."
  value       = var.domain != "" ? aws_acm_certificate.velane[0].arn : ""
}

output "acm_certificate_status" {
  description = "Current validation status of the ACM certificate (PENDING_VALIDATION → ISSUED)."
  value       = var.domain != "" ? aws_acm_certificate.velane[0].status : "N/A — no domain set"
}

output "acm_dns_validation_records" {
  description = "CNAME records you must add to your DNS provider to validate the ACM certificate. Add these in Cloudflare (DNS only / grey cloud), then wait ~2 minutes."
  value = var.domain != "" ? [
    for dvo in aws_acm_certificate.velane[0].domain_validation_options : {
      name  = dvo.resource_record_name
      type  = dvo.resource_record_type
      value = dvo.resource_record_value
    }
  ] : []
}

output "setup_alb_controller_command" {
  description = "Run this script after applying this module to install the AWS Load Balancer Controller."
  value       = "bash infra/scripts/setup-alb-controller.sh --cluster ${aws_eks_cluster.main.name} --region ${var.region} --role-arn ${aws_iam_role.alb_controller.arn}"
}

output "object_storage_bucket" {
  description = "S3 bucket for workflow versions and invocation payloads."
  value       = aws_s3_bucket.velane_data.id
}

output "object_storage_role_arn" {
  description = "IRSA role ARN for the control-plane Kubernetes service account."
  value       = aws_iam_role.velane_storage.arn
}

output "object_storage_service_account_annotations" {
  description = "Pass this map to the k8s module control_plane_service_account_annotations variable."
  value = {
    "eks.amazonaws.com/role-arn" = aws_iam_role.velane_storage.arn
  }
}
