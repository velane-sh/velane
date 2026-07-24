provider "aws" {
  region = var.region
}

data "aws_availability_zones" "available" {
  state = "available"
}

data "aws_caller_identity" "current" {}

locals {
  cluster_name = var.cluster_name != "" ? var.cluster_name : "${var.name_prefix}-${var.region}"
  azs          = slice(data.aws_availability_zones.available.names, 0, var.availability_zone_count)

  public_subnet_cidrs = [
    for index in range(var.availability_zone_count) :
    cidrsubnet(var.vpc_cidr, 4, index)
  ]

  private_subnet_cidrs = [
    for index in range(var.availability_zone_count) :
    cidrsubnet(var.vpc_cidr, 4, index + var.availability_zone_count)
  ]

  default_tags = merge(
    {
      Project     = "velane"
      ManagedBy   = "terraform"
      Terraform   = "true"
      Environment = "production"
    },
    var.tags,
  )
}

resource "aws_s3_bucket" "velane_data" {
  bucket = "${local.cluster_name}-${data.aws_caller_identity.current.account_id}-data"
  tags   = local.default_tags
}

resource "aws_s3_bucket_public_access_block" "velane_data" {
  bucket                  = aws_s3_bucket.velane_data.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_versioning" "velane_data" {
  bucket = aws_s3_bucket.velane_data.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "velane_data" {
  bucket = aws_s3_bucket.velane_data.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_vpc" "main" {
  cidr_block           = var.vpc_cidr
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = merge(local.default_tags, {
    Name = "${local.cluster_name}-vpc"
  })
}

resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id

  tags = merge(local.default_tags, {
    Name = "${local.cluster_name}-igw"
  })
}

resource "aws_subnet" "public" {
  for_each = {
    for index, az in local.azs :
    az => local.public_subnet_cidrs[index]
  }

  vpc_id                  = aws_vpc.main.id
  availability_zone       = each.key
  cidr_block              = each.value
  map_public_ip_on_launch = true

  tags = merge(local.default_tags, {
    Name                                          = "${local.cluster_name}-public-${each.key}"
    "kubernetes.io/cluster/${local.cluster_name}" = "shared"
    "kubernetes.io/role/elb"                      = "1"
  })
}

resource "aws_subnet" "private" {
  for_each = {
    for index, az in local.azs :
    az => local.private_subnet_cidrs[index]
  }

  vpc_id            = aws_vpc.main.id
  availability_zone = each.key
  cidr_block        = each.value

  tags = merge(local.default_tags, {
    Name                                          = "${local.cluster_name}-private-${each.key}"
    "kubernetes.io/cluster/${local.cluster_name}" = "shared"
    "kubernetes.io/role/internal-elb"             = "1"
  })
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.main.id
  }

  tags = merge(local.default_tags, {
    Name = "${local.cluster_name}-public"
  })
}

resource "aws_route_table_association" "public" {
  for_each = aws_subnet.public

  subnet_id      = each.value.id
  route_table_id = aws_route_table.public.id
}

resource "aws_route_table" "private" {
  vpc_id = aws_vpc.main.id

  tags = merge(local.default_tags, {
    Name = "${local.cluster_name}-private"
  })
}

resource "aws_route_table_association" "private" {
  for_each = aws_subnet.private

  subnet_id      = each.value.id
  route_table_id = aws_route_table.private.id
}

data "aws_iam_policy_document" "eks_cluster_assume_role" {
  statement {
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["eks.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "eks_cluster" {
  name               = "${local.cluster_name}-cluster-role"
  assume_role_policy = data.aws_iam_policy_document.eks_cluster_assume_role.json

  tags = merge(local.default_tags, {
    Name = "${local.cluster_name}-cluster-role"
  })
}

resource "aws_iam_role_policy_attachment" "eks_cluster" {
  role       = aws_iam_role.eks_cluster.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonEKSClusterPolicy"
}

data "aws_iam_policy_document" "eks_node_assume_role" {
  statement {
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "eks_nodes" {
  name               = "${local.cluster_name}-node-role"
  assume_role_policy = data.aws_iam_policy_document.eks_node_assume_role.json

  tags = merge(local.default_tags, {
    Name = "${local.cluster_name}-node-role"
  })
}

resource "aws_iam_role_policy_attachment" "eks_nodes" {
  for_each = toset([
    "arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy",
    "arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy",
    "arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryPullOnly",
  ])

  role       = aws_iam_role.eks_nodes.name
  policy_arn = each.value
}

resource "aws_security_group" "cluster" {
  name        = "${local.cluster_name}-cluster-sg"
  description = "Control plane security group for ${local.cluster_name}"
  vpc_id      = aws_vpc.main.id

  egress {
    from_port        = 0
    to_port          = 0
    protocol         = "-1"
    cidr_blocks      = ["0.0.0.0/0"]
    ipv6_cidr_blocks = ["::/0"]
  }

  tags = merge(local.default_tags, {
    Name = "${local.cluster_name}-cluster-sg"
  })
}

resource "aws_security_group" "nodes" {
  name        = "${local.cluster_name}-nodes-sg"
  description = "Worker node security group for ${local.cluster_name}"
  vpc_id      = aws_vpc.main.id

  egress {
    from_port        = 0
    to_port          = 0
    protocol         = "-1"
    cidr_blocks      = ["0.0.0.0/0"]
    ipv6_cidr_blocks = ["::/0"]
  }

  tags = merge(local.default_tags, {
    Name = "${local.cluster_name}-nodes-sg"
  })
}

resource "aws_security_group_rule" "cluster_from_nodes_https" {
  type                     = "ingress"
  description              = "Allow worker nodes to talk to the cluster API"
  from_port                = 443
  to_port                  = 443
  protocol                 = "tcp"
  security_group_id        = aws_security_group.cluster.id
  source_security_group_id = aws_security_group.nodes.id
}

resource "aws_security_group_rule" "nodes_from_cluster" {
  type                     = "ingress"
  description              = "Allow the control plane to reach kubelets"
  from_port                = 1025
  to_port                  = 65535
  protocol                 = "tcp"
  security_group_id        = aws_security_group.nodes.id
  source_security_group_id = aws_security_group.cluster.id
}

resource "aws_security_group_rule" "nodes_self" {
  type              = "ingress"
  description       = "Allow nodes to communicate with each other"
  from_port         = 0
  to_port           = 65535
  protocol          = "-1"
  security_group_id = aws_security_group.nodes.id
  self              = true
}

resource "aws_eks_cluster" "main" {
  name     = local.cluster_name
  role_arn = aws_iam_role.eks_cluster.arn
  version  = var.cluster_version

  access_config {
    authentication_mode                         = "API_AND_CONFIG_MAP"
    bootstrap_cluster_creator_admin_permissions = true
  }

  vpc_config {
    endpoint_public_access  = var.cluster_endpoint_public_access
    endpoint_private_access = var.cluster_endpoint_private_access
    public_access_cidrs     = var.public_access_cidrs
    security_group_ids      = [aws_security_group.cluster.id]
    subnet_ids = concat(
      [for subnet in aws_subnet.public : subnet.id],
      [for subnet in aws_subnet.private : subnet.id],
    )
  }

  depends_on = [
    aws_iam_role_policy_attachment.eks_cluster,
  ]

  tags = merge(local.default_tags, {
    Name = local.cluster_name
  })
}

# =============================================================================
# OIDC provider — required for IAM Roles for Service Accounts (IRSA)
# =============================================================================

data "tls_certificate" "eks_oidc" {
  url = aws_eks_cluster.main.identity[0].oidc[0].issuer
}

resource "aws_iam_openid_connect_provider" "eks" {
  client_id_list  = ["sts.amazonaws.com"]
  thumbprint_list = [data.tls_certificate.eks_oidc.certificates[0].sha1_fingerprint]
  url             = aws_eks_cluster.main.identity[0].oidc[0].issuer

  tags = merge(local.default_tags, {
    Name = "${local.cluster_name}-oidc"
  })
}

# =============================================================================
# ACM wildcard certificate (only when domain is provided)
# =============================================================================

resource "aws_acm_certificate" "velane" {
  count = var.domain != "" ? 1 : 0

  domain_name               = "*.${var.domain}"
  subject_alternative_names = [var.domain]
  validation_method         = "DNS"

  lifecycle {
    create_before_destroy = true
  }

  tags = merge(local.default_tags, {
    Name = "velane-${var.domain}"
  })
}

# =============================================================================
# IAM role for AWS Load Balancer Controller (IRSA)
# =============================================================================

locals {
  oidc_issuer_host = replace(aws_eks_cluster.main.identity[0].oidc[0].issuer, "https://", "")
}

data "aws_iam_policy_document" "alb_controller_assume_role" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]
    effect  = "Allow"

    principals {
      type        = "Federated"
      identifiers = [aws_iam_openid_connect_provider.eks.arn]
    }

    condition {
      test     = "StringEquals"
      variable = "${local.oidc_issuer_host}:sub"
      values   = ["system:serviceaccount:kube-system:aws-load-balancer-controller"]
    }

    condition {
      test     = "StringEquals"
      variable = "${local.oidc_issuer_host}:aud"
      values   = ["sts.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "alb_controller" {
  name               = "${local.cluster_name}-alb-controller"
  assume_role_policy = data.aws_iam_policy_document.alb_controller_assume_role.json

  tags = merge(local.default_tags, {
    Name = "${local.cluster_name}-alb-controller"
  })
}

data "aws_iam_policy_document" "velane_storage_assume_role" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]
    effect  = "Allow"
    principals {
      type        = "Federated"
      identifiers = [aws_iam_openid_connect_provider.eks.arn]
    }
    condition {
      test     = "StringEquals"
      variable = "${local.oidc_issuer_host}:sub"
      values   = ["system:serviceaccount:${var.velane_namespace}:control-plane"]
    }
    condition {
      test     = "StringEquals"
      variable = "${local.oidc_issuer_host}:aud"
      values   = ["sts.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "velane_storage" {
  name               = "${local.cluster_name}-storage-role"
  assume_role_policy = data.aws_iam_policy_document.velane_storage_assume_role.json
  tags               = local.default_tags
}

data "aws_iam_policy_document" "velane_storage" {
  statement {
    actions   = ["s3:ListBucket", "s3:GetBucketLocation"]
    resources = [aws_s3_bucket.velane_data.arn]
  }
  statement {
    actions   = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"]
    resources = ["${aws_s3_bucket.velane_data.arn}/*"]
  }
}

resource "aws_iam_role_policy" "velane_storage" {
  name   = "${local.cluster_name}-storage"
  role   = aws_iam_role.velane_storage.id
  policy = data.aws_iam_policy_document.velane_storage.json
}

resource "aws_iam_policy" "alb_controller" {
  name   = "${local.cluster_name}-alb-controller-policy"
  policy = file("${path.module}/alb-controller-policy.json")

  tags = local.default_tags
}

resource "aws_iam_role_policy_attachment" "alb_controller" {
  role       = aws_iam_role.alb_controller.name
  policy_arn = aws_iam_policy.alb_controller.arn
}

resource "aws_eks_node_group" "primary" {
  cluster_name    = aws_eks_cluster.main.name
  node_group_name = var.node_group_name
  node_role_arn   = aws_iam_role.eks_nodes.arn
  subnet_ids      = [for subnet in aws_subnet.public : subnet.id]
  disk_size       = var.node_disk_size
  instance_types  = var.node_instance_types
  capacity_type   = var.node_capacity_type

  scaling_config {
    desired_size = var.node_desired_size
    min_size     = var.node_min_size
    max_size     = var.node_max_size
  }

  update_config {
    max_unavailable = 1
  }

  depends_on = [
    aws_iam_role_policy_attachment.eks_nodes,
  ]

  tags = merge(local.default_tags, {
    Name = "${local.cluster_name}-${var.node_group_name}"
  })
}
