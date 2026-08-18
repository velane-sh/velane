data "aws_iam_policy_document" "host_assume_role" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "hosts" {
  name_prefix        = "${local.name}-host-"
  assume_role_policy = data.aws_iam_policy_document.host_assume_role.json
  description        = "Sandbox hosts: SSM and telemetry only; snapshots use control-plane-issued URLs and keys"

  tags = { Name = "${local.name}-host" }
}

resource "aws_iam_role_policy_attachment" "hosts_ssm" {
  role       = aws_iam_role.hosts.name
  policy_arn = "arn:${data.aws_partition.current.partition}:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_cloudwatch_log_group" "hosts" {
  name              = "/velane/sandbox-hosts/${local.name}"
  retention_in_days = 30
  kms_key_id        = aws_kms_key.sandbox.arn

  tags = { Name = "${local.name}-hosts" }
}

data "aws_iam_policy_document" "host_telemetry" {
  statement {
    sid       = "WriteHostTelemetry"
    effect    = "Allow"
    actions   = ["cloudwatch:PutMetricData"]
    resources = ["*"]
    condition {
      test     = "StringEquals"
      variable = "cloudwatch:namespace"
      values   = ["Velane/SandboxHosts"]
    }
  }

  statement {
    sid    = "WriteHostLogs"
    effect = "Allow"
    actions = [
      "logs:CreateLogStream",
      "logs:DescribeLogStreams",
      "logs:PutLogEvents",
    ]
    resources = ["${aws_cloudwatch_log_group.hosts.arn}:*"]
  }
}

resource "aws_iam_role_policy" "host_telemetry" {
  name   = "${local.name}-telemetry"
  role   = aws_iam_role.hosts.id
  policy = data.aws_iam_policy_document.host_telemetry.json
}

resource "aws_iam_instance_profile" "hosts" {
  name_prefix = "${local.name}-host-"
  role        = aws_iam_role.hosts.name
}

data "aws_iam_policy_document" "control_plane" {
  statement {
    sid    = "ObserveAndControlThisSandboxAsg"
    effect = "Allow"
    actions = [
      "autoscaling:CompleteLifecycleAction",
      "autoscaling:DescribeAutoScalingGroups",
      "autoscaling:DescribeAutoScalingInstances",
      "autoscaling:DescribeLifecycleHooks",
      "autoscaling:RecordLifecycleActionHeartbeat",
      "autoscaling:SetDesiredCapacity",
      "ec2:DescribeImages",
      "ec2:DescribeInstances",
      "ec2:DescribeLaunchTemplateVersions",
      "ec2:DescribeTags",
    ]
    resources = ["*"]
  }

  statement {
    sid       = "ReadSandboxLifecycleNotifications"
    effect    = "Allow"
    actions   = ["sqs:ChangeMessageVisibility", "sqs:DeleteMessage", "sqs:GetQueueAttributes", "sqs:ReceiveMessage"]
    resources = [aws_sqs_queue.lifecycle.arn]
  }

  statement {
    sid    = "UseSnapshotBucket"
    effect = "Allow"
    actions = [
      "s3:AbortMultipartUpload",
      "s3:DeleteObject",
      "s3:GetObject",
      "s3:GetObjectAttributes",
      "s3:GetObjectVersion",
      "s3:ListBucket",
      "s3:ListBucketMultipartUploads",
      "s3:ListMultipartUploadParts",
      "s3:PutObject",
      "s3:PutObjectTagging",
    ]
    resources = [aws_s3_bucket.snapshots.arn, "${aws_s3_bucket.snapshots.arn}/*"]
  }

  statement {
    sid    = "UseSnapshotEncryptionKey"
    effect = "Allow"
    actions = [
      "kms:Decrypt",
      "kms:DescribeKey",
      "kms:GenerateDataKey",
      "kms:GenerateDataKeyWithoutPlaintext",
      "kms:ReEncryptFrom",
      "kms:ReEncryptTo",
    ]
    resources = [aws_kms_key.sandbox.arn]
  }
}
