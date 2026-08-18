resource "aws_launch_template" "hosts" {
  name_prefix   = "${local.name}-hosts-"
  image_id      = var.ami_id
  instance_type = var.instance_type

  disable_api_stop        = true
  disable_api_termination = false
  ebs_optimized           = true
  update_default_version  = true

  iam_instance_profile { arn = aws_iam_instance_profile.hosts.arn }

  metadata_options {
    http_endpoint               = "enabled"
    http_tokens                 = "required"
    http_put_response_hop_limit = 1
    instance_metadata_tags      = "disabled"
  }

  network_interfaces {
    associate_public_ip_address = false
    delete_on_termination       = true
    security_groups             = [aws_security_group.hosts.id]
  }

  block_device_mappings {
    device_name = "/dev/xvda"

    ebs {
      delete_on_termination = true
      encrypted             = true
      volume_type           = "gp3"
      volume_size           = var.root_volume_size_gib
      iops                  = var.root_volume_iops
      throughput            = var.root_volume_throughput
    }
  }

  monitoring { enabled = true }

  tag_specifications {
    resource_type = "instance"
    tags = {
      Name                       = "${local.name}-host"
      VelaneSandboxHost          = "true"
      VelaneHostControlTLSServer = var.host_control_tls_server_name
      # These opaque identifiers are only for EC2 identity attestation; never return them from public APIs.
      VelaneHostLineage          = var.host_lineage_id
      VelaneHostCompatibilityKey = var.host_compatibility_key
    }
  }

  tag_specifications {
    resource_type = "volume"
    tags = {
      Name              = "${local.name}-host-root"
      VelaneSandboxHost = "true"
    }
  }

  user_data = base64encode(templatefile("${path.module}/user-data.sh.tftpl", {
    host_control_nlb_dns_name = aws_lb.host_api.dns_name
    host_control_port         = var.host_api_port
    host_control_tls_name     = var.host_control_tls_server_name
    host_control_ca_bundle    = var.host_control_ca_bundle_path
    host_client_certificate   = var.host_client_certificate_path
    host_client_private_key   = var.host_client_private_key_path
    watchdog_public_key_path  = var.watchdog_public_key_path
    sandbox_agent_jailer_uid  = var.sandbox_agent_jailer_uid
    sandbox_agent_jailer_gid  = var.sandbox_agent_jailer_gid
    sandbox_host_pool_id      = var.sandbox_host_pool_id
    host_lineage_id           = var.host_lineage_id
    host_compatibility_key    = var.host_compatibility_key
    cloudwatch_log_group      = aws_cloudwatch_log_group.hosts.name
  }))

  lifecycle {
    precondition {
      condition     = var.min_hosts <= var.desired_hosts && var.desired_hosts <= var.max_hosts
      error_message = "min_hosts <= desired_hosts <= max_hosts is required."
    }
  }

  tags = { Name = "${local.name}-hosts" }
}

resource "aws_autoscaling_group" "hosts" {
  name_prefix         = "${local.name}-hosts-"
  min_size            = var.min_hosts
  desired_capacity    = var.desired_hosts
  max_size            = var.max_hosts
  vpc_zone_identifier = local.private_subnet_ids
  health_check_type   = "EC2"
  force_delete        = false

  capacity_rebalance    = false
  default_cooldown      = 300
  protect_from_scale_in = true

  launch_template {
    id      = aws_launch_template.hosts.id
    version = "$Latest"
  }

  termination_policies = ["OldestLaunchTemplate"]

  tag {
    key                 = "Name"
    value               = "${local.name}-host"
    propagate_at_launch = true
  }

  tag {
    key                 = "VelaneSandboxHost"
    value               = "true"
    propagate_at_launch = true
  }

  tag {
    key                 = "VelaneHostLineage"
    value               = var.host_lineage_id
    propagate_at_launch = true
  }

  tag {
    key                 = "VelaneHostCompatibilityKey"
    value               = var.host_compatibility_key
    propagate_at_launch = true
  }

  lifecycle {
    precondition {
      condition     = var.max_hosts == 0 || var.max_hosts >= 1
      error_message = "max_hosts must be zero (disabled) or at least one."
    }
  }
}
