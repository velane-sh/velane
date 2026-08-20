resource "aws_kms_key" "sandbox" {
  description             = "Velane sandbox snapshot encryption; access is scoped to the private control plane"
  deletion_window_in_days = 30
  enable_key_rotation     = true
  policy                  = data.aws_iam_policy_document.sandbox_kms.json

  tags = { Name = "${local.name}-sandbox" }
}

resource "aws_kms_alias" "sandbox" {
  name          = "alias/${local.name}-sandbox"
  target_key_id = aws_kms_key.sandbox.key_id
}

resource "aws_s3_bucket" "snapshot_logs" {
  bucket_prefix = "${local.name}-snapshot-access-"

  tags = { Name = "${local.name}-snapshot-access" }
}

resource "aws_s3_bucket_public_access_block" "snapshot_logs" {
  bucket                  = aws_s3_bucket.snapshot_logs.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_ownership_controls" "snapshot_logs" {
  bucket = aws_s3_bucket.snapshot_logs.id

  rule { object_ownership = "BucketOwnerEnforced" }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "snapshot_logs" {
  bucket = aws_s3_bucket.snapshot_logs.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "snapshot_logs" {
  bucket = aws_s3_bucket.snapshot_logs.id

  rule {
    id     = "expire-access-logs"
    status = "Enabled"
    filter {}

    expiration { days = var.access_log_retention_days }
  }
}

resource "aws_s3_bucket" "snapshots" {
  bucket_prefix = "${local.name}-snapshots-"

  tags = { Name = "${local.name}-snapshots" }
}

resource "aws_s3_bucket_public_access_block" "snapshots" {
  bucket                  = aws_s3_bucket.snapshots.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_ownership_controls" "snapshots" {
  bucket = aws_s3_bucket.snapshots.id

  rule { object_ownership = "BucketOwnerEnforced" }
}

resource "aws_s3_bucket_versioning" "snapshots" {
  bucket = aws_s3_bucket.snapshots.id

  versioning_configuration { status = "Enabled" }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "snapshots" {
  bucket = aws_s3_bucket.snapshots.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm     = "aws:kms"
      kms_master_key_id = aws_kms_key.sandbox.arn
    }
    bucket_key_enabled = true
  }
}

resource "aws_s3_bucket_logging" "snapshots" {
  bucket        = aws_s3_bucket.snapshots.id
  target_bucket = aws_s3_bucket.snapshot_logs.id
  target_prefix = "snapshots/"
}

resource "aws_s3_bucket_lifecycle_configuration" "snapshots" {
  bucket = aws_s3_bucket.snapshots.id

  rule {
    id     = "abort-incomplete-multipart-uploads"
    status = "Enabled"
    filter {}

    abort_incomplete_multipart_upload { days_after_initiation = 7 }
  }

  rule {
    id     = "remove-deleted-snapshot-artifacts"
    status = "Enabled"

    filter {
      and {
        tags = { "sandbox-snapshot-state" = "deleted" }
      }
    }

    expiration { days = var.deleted_artifact_retention_days }
    noncurrent_version_expiration { noncurrent_days = var.deleted_artifact_retention_days }
  }
}

resource "aws_s3_bucket_policy" "snapshots" {
  bucket = aws_s3_bucket.snapshots.id
  policy = data.aws_iam_policy_document.snapshots_bucket.json
}

resource "aws_s3_bucket_policy" "snapshot_logs" {
  bucket = aws_s3_bucket.snapshot_logs.id
  policy = data.aws_iam_policy_document.snapshot_logs_bucket.json
}

data "aws_iam_policy_document" "sandbox_kms" {
  statement {
    sid       = "EnableRootAccountPermissions"
    effect    = "Allow"
    actions   = ["kms:*"]
    resources = ["*"]

    principals {
      type        = "AWS"
      identifiers = ["arn:${data.aws_partition.current.partition}:iam::${data.aws_caller_identity.current.account_id}:root"]
    }
  }

  statement {
    sid    = "AllowCloudWatchHostLogEncryption"
    effect = "Allow"
    actions = [
      "kms:Decrypt",
      "kms:DescribeKey",
      "kms:Encrypt",
      "kms:GenerateDataKey*",
      "kms:ReEncrypt*",
    ]
    resources = ["*"]

    principals {
      type        = "Service"
      identifiers = ["logs.${var.region}.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "kms:ViaService"
      values   = ["logs.${var.region}.amazonaws.com"]
    }
  }

  dynamic "statement" {
    for_each = length(var.control_plane_principal_arns) == 0 ? [] : [true]
    content {
      sid    = "AllowPrivateControlPlaneSnapshotCrypto"
      effect = "Allow"
      actions = [
        "kms:Decrypt",
        "kms:DescribeKey",
        "kms:GenerateDataKey",
        "kms:GenerateDataKeyWithoutPlaintext",
        "kms:ReEncryptFrom",
        "kms:ReEncryptTo",
      ]
      resources = ["*"]

      principals {
        type        = "AWS"
        identifiers = var.control_plane_principal_arns
      }

    }
  }
}

data "aws_iam_policy_document" "snapshots_bucket" {
  statement {
    sid       = "DenyInsecureTransport"
    effect    = "Deny"
    actions   = ["s3:*"]
    resources = [aws_s3_bucket.snapshots.arn, "${aws_s3_bucket.snapshots.arn}/*"]

    principals {
      type        = "*"
      identifiers = ["*"]
    }

    condition {
      test     = "Bool"
      variable = "aws:SecureTransport"
      values   = ["false"]
    }
  }

  statement {
    sid       = "DenyWritesWithoutCustomerManagedEncryption"
    effect    = "Deny"
    actions   = ["s3:PutObject"]
    resources = ["${aws_s3_bucket.snapshots.arn}/*"]

    principals {
      type        = "*"
      identifiers = ["*"]
    }

    condition {
      test     = "StringNotEquals"
      variable = "s3:x-amz-server-side-encryption-aws-kms-key-id"
      values   = [aws_kms_key.sandbox.arn]
    }
  }
}

data "aws_iam_policy_document" "snapshot_logs_bucket" {
  statement {
    sid       = "DenyInsecureTransport"
    effect    = "Deny"
    actions   = ["s3:*"]
    resources = [aws_s3_bucket.snapshot_logs.arn, "${aws_s3_bucket.snapshot_logs.arn}/*"]

    principals {
      type        = "*"
      identifiers = ["*"]
    }

    condition {
      test     = "Bool"
      variable = "aws:SecureTransport"
      values   = ["false"]
    }
  }

  statement {
    sid       = "AllowS3ServerAccessLogs"
    effect    = "Allow"
    actions   = ["s3:PutObject"]
    resources = ["${aws_s3_bucket.snapshot_logs.arn}/snapshots/*"]

    principals {
      type        = "Service"
      identifiers = ["logging.s3.amazonaws.com"]
    }
  }
}

data "aws_iam_policy_document" "s3_endpoint" {
  statement {
    sid     = "AllowOnlyDedicatedSnapshotBuckets"
    effect  = "Allow"
    actions = ["s3:*"]
    resources = [
      aws_s3_bucket.snapshots.arn,
      "${aws_s3_bucket.snapshots.arn}/*",
      aws_s3_bucket.snapshot_logs.arn,
      "${aws_s3_bucket.snapshot_logs.arn}/*",
    ]
    principals {
      type        = "*"
      identifiers = ["*"]
    }
  }
}
