# Low-cost Azure production deployment

This stack runs Velane and the licensing service on one Azure Linux VM with
Docker Compose, a private Azure Database for PostgreSQL Flexible Server, the
existing external Redis, and GHCR images. The database server contains
separate `velane`, `nango`, and `licensing` databases. It intentionally avoids
AKS, ACR, Azure Redis, Application Gateway, ClickHouse, and object storage to
minimize baseline cost.

The production default is `westus`, where this subscription permits both
PostgreSQL Flexible Server and the selected VM SKU.

The VM currently uses `Standard_D2s_v3` because West US reported a live
capacity restriction for the less expensive `Standard_B2ms`. Re-evaluate a
resize to `Standard_B2ms` when capacity becomes available.

The VM is a single failure domain. OpenTofu can rebuild it, but the service is
unavailable while the VM is down or being replaced.

## Deploy

1. Copy `terraform.tfvars.example` to the ignored `terraform.tfvars` file.
2. Reuse every existing encryption, JWT, OAuth, Nango, and Redis value. Changing
   encryption keys makes existing encrypted credentials unusable.
3. Initialize and review the plan:

   ```bash
   tofu init
   tofu plan -out=azure.tfplan
   ```

4. Apply only after reviewing the resource count and estimated architecture:

   ```bash
   tofu apply azure.tfplan
   ```

5. Do not change DNS yet. Migrate both databases, validate through temporary
   host mappings, stop AWS writes, perform the final database copy, and then
   point the five production A records at `public_ip_address`.

The PostgreSQL server is private and can only be reached from the VM subnet.
Database migration should therefore run through the Azure VM.

## Operations

Container configuration lives at `/opt/velane` on the VM. Public SSH is denied
unless `ssh_allowed_cidrs` contains at least one explicit CIDR. Only ports 80
and 443 are publicly open by default.

Never destroy AWS until the Azure API, admin UI, MCP endpoint, OAuth/Nango flow,
executor invocation, external Redis connection, and both migrated databases
have been verified.
