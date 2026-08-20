data "aws_caller_identity" "current" {}

data "aws_partition" "current" {}

data "aws_prefix_list" "s3" {
  name = "com.amazonaws.${var.region}.s3"
}

locals {
  name = var.name_prefix

  default_tags = merge({
    Project   = "velane"
    Component = "sandbox-hosts"
    ManagedBy = "opentofu"
  }, var.tags)

  private_subnet_ids      = [for subnet in aws_subnet.sandbox_hosts : subnet.id]
  private_route_table_ids = [for route_table in aws_route_table.sandbox_hosts : route_table.id]

  interface_endpoint_services = toset([
    "ec2messages",
    "kms",
    "logs",
    "monitoring",
    "ssm",
    "ssmmessages",
    "sts",
  ])
}

resource "aws_subnet" "sandbox_hosts" {
  for_each = var.private_subnet_cidrs

  vpc_id                  = var.vpc_id
  availability_zone       = each.key
  cidr_block              = each.value
  map_public_ip_on_launch = false

  tags = {
    Name = "${local.name}-hosts-${each.key}"
    Tier = "sandbox-host-private"
  }
}

resource "aws_route_table" "sandbox_hosts" {
  for_each = var.private_subnet_cidrs

  vpc_id = var.vpc_id

  tags = {
    Name = "${local.name}-hosts-${each.key}"
    Tier = "sandbox-host-private"
  }
}

resource "aws_route_table_association" "sandbox_hosts" {
  for_each = var.private_subnet_cidrs

  subnet_id      = aws_subnet.sandbox_hosts[each.key].id
  route_table_id = aws_route_table.sandbox_hosts[each.key].id
}

resource "aws_security_group" "hosts" {
  name_prefix            = "${local.name}-hosts-"
  description            = "No ingress; sandbox hosts can reach only the private host API and required VPC endpoints"
  vpc_id                 = var.vpc_id
  revoke_rules_on_delete = true

  tags = { Name = "${local.name}-hosts" }
}

resource "aws_security_group" "host_api_nlb" {
  name_prefix            = "${local.name}-host-api-nlb-"
  description            = "Private TCP entry point for sandbox hosts to reach the host-control API"
  vpc_id                 = var.vpc_id
  revoke_rules_on_delete = true

  ingress {
    description     = "mTLS host-control traffic from sandbox hosts only"
    from_port       = var.host_api_port
    to_port         = var.host_api_port
    protocol        = "tcp"
    security_groups = [aws_security_group.hosts.id]
  }

  tags = { Name = "${local.name}-host-api-nlb" }
}

resource "aws_security_group" "interface_endpoints" {
  name_prefix            = "${local.name}-endpoints-"
  description            = "TLS access to AWS private endpoints from sandbox hosts only"
  vpc_id                 = var.vpc_id
  revoke_rules_on_delete = true

  ingress {
    description     = "TLS from sandbox hosts"
    from_port       = 443
    to_port         = 443
    protocol        = "tcp"
    security_groups = [aws_security_group.hosts.id]
  }

  tags = { Name = "${local.name}-endpoints" }
}

resource "aws_security_group_rule" "hosts_to_host_api" {
  type                     = "egress"
  description              = "End-to-end mTLS to the private host-control API"
  from_port                = var.host_api_port
  to_port                  = var.host_api_port
  protocol                 = "tcp"
  security_group_id        = aws_security_group.hosts.id
  source_security_group_id = aws_security_group.host_api_nlb.id
}

resource "aws_security_group_rule" "hosts_to_endpoints" {
  type                     = "egress"
  description              = "TLS to required AWS VPC interface endpoints"
  from_port                = 443
  to_port                  = 443
  protocol                 = "tcp"
  security_group_id        = aws_security_group.hosts.id
  source_security_group_id = aws_security_group.interface_endpoints.id
}

resource "aws_security_group_rule" "hosts_to_s3_gateway" {
  type              = "egress"
  description       = "HTTPS snapshot multipart uploads through the regional S3 gateway endpoint only"
  from_port         = 443
  to_port           = 443
  protocol          = "tcp"
  security_group_id = aws_security_group.hosts.id
  prefix_list_ids   = [data.aws_prefix_list.s3.id]
}

resource "aws_security_group_rule" "hosts_to_vpc_resolver_udp" {
  type              = "egress"
  description       = "DNS queries to the VPC resolver only"
  from_port         = 53
  to_port           = 53
  protocol          = "udp"
  security_group_id = aws_security_group.hosts.id
  cidr_blocks       = [var.vpc_cidr]
}

resource "aws_security_group_rule" "hosts_to_vpc_resolver_tcp" {
  type              = "egress"
  description       = "Large DNS responses to the VPC resolver only"
  from_port         = 53
  to_port           = 53
  protocol          = "tcp"
  security_group_id = aws_security_group.hosts.id
  cidr_blocks       = [var.vpc_cidr]
}

resource "aws_vpc_endpoint" "s3" {
  vpc_id            = var.vpc_id
  service_name      = "com.amazonaws.${var.region}.s3"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = local.private_route_table_ids

  policy = data.aws_iam_policy_document.s3_endpoint.json

  tags = { Name = "${local.name}-s3" }
}

resource "aws_vpc_endpoint" "interface" {
  for_each = local.interface_endpoint_services

  vpc_id              = var.vpc_id
  service_name        = "com.amazonaws.${var.region}.${each.value}"
  vpc_endpoint_type   = "Interface"
  subnet_ids          = local.private_subnet_ids
  security_group_ids  = [aws_security_group.interface_endpoints.id]
  private_dns_enabled = true

  tags = { Name = "${local.name}-${each.value}" }
}

resource "aws_lb" "host_api" {
  name               = substr("${local.name}-host-api", 0, 32)
  internal           = true
  load_balancer_type = "network"
  subnets            = local.private_subnet_ids
  security_groups    = [aws_security_group.host_api_nlb.id]

  enable_cross_zone_load_balancing = true
  enable_deletion_protection       = true

  tags = { Name = "${local.name}-host-api" }
}

resource "aws_lb_target_group" "host_api" {
  name_prefix = substr("${local.name}-host-api-", 0, 6)
  port        = var.host_api_port
  protocol    = "TCP"
  target_type = "ip"
  vpc_id      = var.vpc_id

  health_check {
    enabled  = true
    protocol = "TCP"
    port     = tostring(var.host_api_port)
  }

  deregistration_delay = 30

  tags = { Name = "${local.name}-host-api" }
}

resource "aws_lb_target_group_attachment" "host_api" {
  for_each = var.host_api_target_ips

  target_group_arn = aws_lb_target_group.host_api.arn
  target_id        = each.value
  port             = var.host_api_port
}

resource "aws_lb_listener" "host_api" {
  load_balancer_arn = aws_lb.host_api.arn
  port              = var.host_api_port
  protocol          = "TCP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.host_api.arn
  }
}
