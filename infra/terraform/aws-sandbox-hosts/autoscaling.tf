resource "aws_sqs_queue" "lifecycle_dlq" {
  name_prefix               = "${local.name}-lifecycle-dlq-"
  message_retention_seconds = 1209600
  sqs_managed_sse_enabled   = true

  tags = { Name = "${local.name}-lifecycle-dlq" }
}

resource "aws_sqs_queue" "lifecycle" {
  name_prefix                = "${local.name}-lifecycle-"
  visibility_timeout_seconds = 360
  message_retention_seconds  = 1209600
  receive_wait_time_seconds  = 20
  sqs_managed_sse_enabled    = true

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.lifecycle_dlq.arn
    maxReceiveCount     = 5
  })

  tags = { Name = "${local.name}-lifecycle" }
}

data "aws_iam_policy_document" "lifecycle_queue" {
  statement {
    sid       = "AllowEventBridgeLifecycleEvents"
    effect    = "Allow"
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.lifecycle.arn]

    principals {
      type        = "Service"
      identifiers = ["events.amazonaws.com"]
    }

    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_cloudwatch_event_rule.lifecycle.arn]
    }
  }
}

resource "aws_sqs_queue_policy" "lifecycle" {
  queue_url = aws_sqs_queue.lifecycle.id
  policy    = data.aws_iam_policy_document.lifecycle_queue.json
}

resource "aws_cloudwatch_event_rule" "lifecycle" {
  name_prefix = "${local.name}-lifecycle-"
  description = "Delivers sandbox-host ASG lifecycle actions to the private control-plane worker"

  event_pattern = jsonencode({
    source      = ["aws.autoscaling"]
    detail-type = ["EC2 Instance-launch Lifecycle Action", "EC2 Instance-terminate Lifecycle Action"]
    detail = {
      AutoScalingGroupName = [aws_autoscaling_group.hosts.name]
    }
  })
}

resource "aws_cloudwatch_event_target" "lifecycle" {
  rule = aws_cloudwatch_event_rule.lifecycle.name
  arn  = aws_sqs_queue.lifecycle.arn
}

resource "aws_autoscaling_lifecycle_hook" "launch" {
  name                   = "sandbox-host-registration"
  autoscaling_group_name = aws_autoscaling_group.hosts.name
  lifecycle_transition   = "autoscaling:EC2_INSTANCE_LAUNCHING"
  default_result         = "ABANDON"
  heartbeat_timeout      = var.lifecycle_heartbeat_timeout_seconds
  notification_metadata  = jsonencode({ kind = "sandbox_host_launch" })
}

resource "aws_autoscaling_lifecycle_hook" "termination" {
  name                   = "sandbox-host-drain"
  autoscaling_group_name = aws_autoscaling_group.hosts.name
  lifecycle_transition   = "autoscaling:EC2_INSTANCE_TERMINATING"
  # A timeout must never be treated as an acknowledged drain. The worker only
  # calls CompleteLifecycleAction(CONTINUE) after it has durably proved that no
  # VM, upload, or only-local full snapshot remains on the host.
  default_result        = "ABANDON"
  heartbeat_timeout     = var.lifecycle_heartbeat_timeout_seconds
  notification_metadata = jsonencode({ kind = "sandbox_host_termination" })
}
