locals {
  resource_name = replace(lower(var.name_prefix), "_", "-")
  postgres_name = "${local.resource_name}-${substr(replace(var.subscription_id, "-", ""), 0, 6)}-pg"
  storage_name  = substr(replace("${local.resource_name}${substr(replace(var.subscription_id, "-", ""), 0, 6)}data", "-", ""), 0, 24)

  admin_host         = "${var.admin_subdomain}.${var.base_domain}"
  api_host           = "${var.api_subdomain}.${var.base_domain}"
  mcp_host           = "${var.mcp_subdomain}.${var.base_domain}"
  nango_connect_host = "${var.nango_connect_subdomain}.${var.base_domain}"
  nango_api_host     = "${var.nango_api_subdomain}.${var.base_domain}"
  license_host       = "license.${var.base_domain}"
  telemetry_host     = "telemetry.${var.base_domain}"

  postgres_host  = azurerm_postgresql_flexible_server.main.fqdn
  database_url   = "postgresql://${var.postgres_admin_username}:${urlencode(random_password.postgres.result)}@${local.postgres_host}:5432/velane?sslmode=require"
  nango_db_url   = "postgresql://${var.postgres_admin_username}:${urlencode(random_password.postgres.result)}@${local.postgres_host}:5432/nango?sslmode=require"
  license_db_url = "postgresql://${var.postgres_admin_username}:${urlencode(random_password.postgres.result)}@${local.postgres_host}:5432/licensing?sslmode=require"

  runtime_env = {
    ADMIN_HOST                       = local.admin_host
    API_HOST                         = local.api_host
    MCP_HOST                         = local.mcp_host
    NANGO_CONNECT_HOST               = local.nango_connect_host
    NANGO_API_HOST                   = local.nango_api_host
    LICENSE_HOST                     = local.license_host
    TELEMETRY_HOST                   = local.telemetry_host
    CONTROL_PLANE_IMAGE              = var.control_plane_image
    BUN_EXECUTOR_IMAGE               = var.bun_executor_image
    PYTHON_EXECUTOR_IMAGE            = var.python_executor_image
    ADMIN_IMAGE                      = var.admin_image
    MCP_SERVER_IMAGE                 = var.mcp_server_image
    NANGO_IMAGE                      = var.nango_image
    LICENSE_SERVER_IMAGE             = var.license_server_image
    DATABASE_URL                     = local.database_url
    NANGO_DATABASE_URL               = local.nango_db_url
    LICENSE_DATABASE_URL             = local.license_db_url
    REDIS_URL                        = var.redis_url
    ENCRYPTION_KEY                   = var.encryption_key
    JWT_PRIVATE_KEY                  = var.jwt_private_key_pem
    NANGO_ENCRYPTION_KEY             = var.nango_encryption_key
    NANGO_SECRET_KEY                 = var.nango_secret_key
    NANGO_PUBLIC_KEY                 = var.nango_public_key
    NANGO_WEBHOOK_SECRET             = var.nango_webhook_secret
    NANGO_CONNECT_URL                = "https://${local.nango_connect_host}"
    NANGO_API_URL                    = "https://${local.nango_api_host}"
    MCP_PUBLIC_URL                   = "https://${local.mcp_host}/mcp"
    PUBLIC_BASE_URL                  = "https://${local.admin_host}"
    GOOGLE_OAUTH_CLIENT_ID           = var.google_oauth_client_id
    GOOGLE_OAUTH_CLIENT_SECRET       = var.google_oauth_client_secret
    GITHUB_OAUTH_CLIENT_ID           = var.github_oauth_client_id
    GITHUB_OAUTH_CLIENT_SECRET       = var.github_oauth_client_secret
    WORKER_COUNT                     = tostring(var.worker_count)
    OBJECT_STORAGE_DRIVER            = "azure"
    OBJECT_STORAGE_BUCKET            = azurerm_storage_container.velane.name
    OBJECT_STORAGE_AZURE_ACCOUNT_URL = azurerm_storage_account.velane.primary_blob_endpoint
    OBJECT_GC_GRACE_PERIOD           = var.object_gc_grace_period
    INVOCATION_RETENTION             = var.invocation_retention
    LICENSE_PRIVATE_KEY_PEM          = var.private_key_pem
    GHCR_USERNAME                    = var.ghcr_username
    GHCR_TOKEN                       = var.ghcr_token
  }
}

resource "azurerm_resource_group" "main" {
  name     = "${local.resource_name}-rg"
  location = var.location

  tags = {
    Project     = "velane"
    Environment = "production"
    ManagedBy   = "opentofu"
  }
}

resource "azurerm_storage_account" "velane" {
  name                            = local.storage_name
  resource_group_name             = azurerm_resource_group.main.name
  location                        = azurerm_resource_group.main.location
  account_tier                    = "Standard"
  account_replication_type        = "LRS"
  min_tls_version                 = "TLS1_2"
  allow_nested_items_to_be_public = false

  blob_properties {
    versioning_enabled = true
  }

  tags = {
    Project     = "velane"
    Environment = "production"
    ManagedBy   = "opentofu"
  }
}

resource "azurerm_storage_container" "velane" {
  name                  = "velane-data"
  storage_account_id    = azurerm_storage_account.velane.id
  container_access_type = "private"
}

resource "azurerm_virtual_network" "main" {
  name                = "${local.resource_name}-vnet"
  address_space       = ["10.40.0.0/16"]
  location            = azurerm_resource_group.main.location
  resource_group_name = azurerm_resource_group.main.name
}

resource "azurerm_subnet" "vm" {
  name                 = "vm"
  resource_group_name  = azurerm_resource_group.main.name
  virtual_network_name = azurerm_virtual_network.main.name
  address_prefixes     = ["10.40.1.0/24"]
}

resource "azurerm_subnet" "postgres" {
  name                 = "postgres"
  resource_group_name  = azurerm_resource_group.main.name
  virtual_network_name = azurerm_virtual_network.main.name
  address_prefixes     = ["10.40.2.0/24"]
  service_endpoints    = ["Microsoft.Storage"]

  delegation {
    name = "postgres-flexible-server"

    service_delegation {
      name    = "Microsoft.DBforPostgreSQL/flexibleServers"
      actions = ["Microsoft.Network/virtualNetworks/subnets/join/action"]
    }
  }
}

resource "azurerm_private_dns_zone" "postgres" {
  name                = "${local.resource_name}.postgres.database.azure.com"
  resource_group_name = azurerm_resource_group.main.name
}

resource "azurerm_private_dns_zone_virtual_network_link" "postgres" {
  name                  = "${local.resource_name}-postgres-link"
  private_dns_zone_name = azurerm_private_dns_zone.postgres.name
  virtual_network_id    = azurerm_virtual_network.main.id
  resource_group_name   = azurerm_resource_group.main.name
}

resource "random_password" "postgres" {
  length  = 32
  special = false
}

resource "azurerm_postgresql_flexible_server" "main" {
  name                          = local.postgres_name
  resource_group_name           = azurerm_resource_group.main.name
  location                      = azurerm_resource_group.main.location
  version                       = var.postgres_version
  delegated_subnet_id           = azurerm_subnet.postgres.id
  private_dns_zone_id           = azurerm_private_dns_zone.postgres.id
  public_network_access_enabled = false
  administrator_login           = var.postgres_admin_username
  administrator_password        = random_password.postgres.result
  storage_mb                    = var.postgres_storage_mb
  sku_name                      = var.postgres_sku_name
  backup_retention_days         = 7
  geo_redundant_backup_enabled  = false

  depends_on = [azurerm_private_dns_zone_virtual_network_link.postgres]
}

resource "azurerm_postgresql_flexible_server_configuration" "extensions" {
  name      = "azure.extensions"
  server_id = azurerm_postgresql_flexible_server.main.id
  value     = "PGCRYPTO,PG_STAT_STATEMENTS,UUID-OSSP"
}

resource "azurerm_postgresql_flexible_server_database" "velane" {
  name      = "velane"
  server_id = azurerm_postgresql_flexible_server.main.id
  charset   = "UTF8"
  collation = "en_US.utf8"
}

resource "azurerm_postgresql_flexible_server_database" "nango" {
  name      = "nango"
  server_id = azurerm_postgresql_flexible_server.main.id
  charset   = "UTF8"
  collation = "en_US.utf8"
}

resource "azurerm_postgresql_flexible_server_database" "licensing" {
  name      = "licensing"
  server_id = azurerm_postgresql_flexible_server.main.id
  charset   = "UTF8"
  collation = "en_US.utf8"
}

resource "azurerm_network_security_group" "vm" {
  name                = "${local.resource_name}-nsg"
  location            = azurerm_resource_group.main.location
  resource_group_name = azurerm_resource_group.main.name

  security_rule {
    name                       = "http"
    priority                   = 100
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "80"
    source_address_prefix      = "*"
    destination_address_prefix = "*"
  }

  security_rule {
    name                       = "https"
    priority                   = 110
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "443"
    source_address_prefix      = "*"
    destination_address_prefix = "*"
  }

  dynamic "security_rule" {
    for_each = length(var.ssh_allowed_cidrs) == 0 ? [] : [1]
    content {
      name                       = "ssh"
      priority                   = 120
      direction                  = "Inbound"
      access                     = "Allow"
      protocol                   = "Tcp"
      source_port_range          = "*"
      destination_port_range     = "22"
      source_address_prefixes    = var.ssh_allowed_cidrs
      destination_address_prefix = "*"
    }
  }
}

resource "azurerm_public_ip" "vm" {
  name                = "${local.resource_name}-ip"
  location            = azurerm_resource_group.main.location
  resource_group_name = azurerm_resource_group.main.name
  allocation_method   = "Static"
  sku                 = "Standard"
}

resource "azurerm_network_interface" "vm" {
  name                = "${local.resource_name}-nic"
  location            = azurerm_resource_group.main.location
  resource_group_name = azurerm_resource_group.main.name

  ip_configuration {
    name                          = "primary"
    subnet_id                     = azurerm_subnet.vm.id
    private_ip_address_allocation = "Dynamic"
    public_ip_address_id          = azurerm_public_ip.vm.id
  }
}

resource "azurerm_network_interface_security_group_association" "vm" {
  network_interface_id      = azurerm_network_interface.vm.id
  network_security_group_id = azurerm_network_security_group.vm.id
}

resource "azurerm_linux_virtual_machine" "main" {
  name                            = "${local.resource_name}-vm"
  resource_group_name             = azurerm_resource_group.main.name
  location                        = azurerm_resource_group.main.location
  size                            = var.vm_size
  admin_username                  = var.admin_username
  disable_password_authentication = true
  network_interface_ids           = [azurerm_network_interface.vm.id]

  identity {
    type = "SystemAssigned"
  }

  admin_ssh_key {
    username   = var.admin_username
    public_key = var.ssh_public_key
  }

  os_disk {
    caching              = "ReadWrite"
    storage_account_type = "StandardSSD_LRS"
    disk_size_gb         = 64
  }

  source_image_reference {
    publisher = "Canonical"
    offer     = "ubuntu-24_04-lts"
    sku       = "server"
    version   = "latest"
  }

  custom_data = base64encode(templatefile("${path.module}/cloud-init.yaml.tftpl", {
    runtime_env_base64 = base64encode(join("\n", [
      for key in sort(keys(local.runtime_env)) : "${key}='${replace(local.runtime_env[key], "'", "\\'")}'"
    ]))
    compose_base64 = base64encode(file("${path.module}/docker-compose.yml"))
    caddy_base64   = base64encode(file("${path.module}/Caddyfile"))
    seccomp_base64 = base64encode(file("${path.module}/seccomp-executor.json"))
  }))

  boot_diagnostics {}

  lifecycle {
    # custom_data bootstraps a new VM. Runtime Compose changes are deployed
    # in place and must not force replacement of the production host.
    ignore_changes = [custom_data]
  }

  depends_on = [
    azurerm_postgresql_flexible_server_database.velane,
    azurerm_postgresql_flexible_server_database.nango,
    azurerm_postgresql_flexible_server_database.licensing,
    azurerm_network_interface_security_group_association.vm,
  ]
}

resource "azurerm_role_assignment" "vm_blob_data" {
  scope                = azurerm_storage_account.velane.id
  role_definition_name = "Storage Blob Data Contributor"
  principal_id         = azurerm_linux_virtual_machine.main.identity[0].principal_id
}
