output "region" {
  description = "AWS region containing the immutable sandbox-host pool."
  value       = var.region
}

output "sandbox_host_asg_name" {
  description = "Operator-only ASG reference for the AWS capacity adapter."
  value       = aws_autoscaling_group.hosts.name
}

output "sandbox_host_launch_template_id" {
  description = "Launch template reference attested during host enrollment."
  value       = aws_launch_template.hosts.id
}

output "sandbox_host_private_subnet_ids" {
  description = "Dedicated private subnet IDs for the sandbox-host ASG."
  value       = local.private_subnet_ids
}

output "sandbox_host_security_group_id" {
  description = "No-ingress security group attached to every sandbox host."
  value       = aws_security_group.hosts.id
}

output "host_control_nlb_dns_name" {
  description = "Private TCP NLB DNS name. Configure the host agent with this endpoint and its private TLS server name."
  value       = aws_lb.host_api.dns_name
}

output "host_control_nlb_zone_id" {
  description = "Private NLB Route 53 zone ID for an internal alias record, if needed."
  value       = aws_lb.host_api.zone_id
}

output "host_control_mtls_configuration" {
  description = "Private host-control mTLS configuration contract. Secret ARNs are references for the backend deployment only; certificate, CA, and private-key bytes are never Terraform values."
  value = {
    nlb_dns_name                  = aws_lb.host_api.dns_name
    port                          = var.host_api_port
    server_name                   = var.host_control_tls_server_name
    server_certificate_secret_arn = var.host_control_server_certificate_secret_arn
    client_ca_secret_arn          = var.host_control_client_ca_secret_arn
    host_ca_bundle_path           = var.host_control_ca_bundle_path
  }
  sensitive = true
}

output "snapshot_bucket_name" {
  description = "Dedicated encrypted/versioned full-snapshot artifact bucket. Do not expose it to sandbox hosts."
  value       = aws_s3_bucket.snapshots.id
}

output "snapshot_kms_key_arn" {
  description = "Customer-managed KMS key used by snapshot artifacts and data-key wrapping."
  value       = aws_kms_key.sandbox.arn
}

output "lifecycle_queue_url" {
  description = "Private control-plane worker SQS input for lifecycle drain/registration events."
  value       = aws_sqs_queue.lifecycle.url
}

output "lifecycle_dead_letter_queue_url" {
  description = "Operator alarm target for unprocessed lifecycle notifications."
  value       = aws_sqs_queue.lifecycle_dlq.url
}

output "control_plane_policy_json" {
  description = "Attach this scoped policy to the private control-plane capacity/host-control role; never attach it to host instances."
  value       = data.aws_iam_policy_document.control_plane.json
  sensitive   = true
}
