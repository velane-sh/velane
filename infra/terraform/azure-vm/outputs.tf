output "resource_group_name" {
  value = azurerm_resource_group.main.name
}

output "location" {
  value = azurerm_resource_group.main.location
}

output "public_ip_address" {
  value = azurerm_public_ip.vm.ip_address
}

output "vm_name" {
  value = azurerm_linux_virtual_machine.main.name
}

output "postgres_fqdn" {
  value = azurerm_postgresql_flexible_server.main.fqdn
}

output "dns_records" {
  description = "Create these A records at the current DNS provider before cutover."
  value = {
    for host in [
      local.admin_host,
      local.api_host,
      local.mcp_host,
      local.nango_connect_host,
      local.nango_api_host,
      local.license_host,
      local.telemetry_host,
    ] : host => azurerm_public_ip.vm.ip_address
  }
}

output "ssh_command" {
  value = length(var.ssh_allowed_cidrs) > 0 ? "ssh ${var.admin_username}@${azurerm_public_ip.vm.ip_address}" : "Public SSH is disabled"
}

output "database_url" {
  value     = local.database_url
  sensitive = true
}

output "nango_database_url" {
  value     = local.nango_db_url
  sensitive = true
}

output "object_storage_account_name" {
  description = "Azure Storage account holding workflow versions and invocation payloads."
  value       = azurerm_storage_account.velane.name
}

output "object_storage_container_name" {
  value = azurerm_storage_container.velane.name
}
