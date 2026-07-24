# Velane — Production Deployment Guide (EKS + ALB + HTTPS)

This guide walks you through deploying Velane on AWS EKS with a custom domain, free HTTPS via ACM, and a single Application Load Balancer for all services.

## Prerequisites

| Tool | Version | Install |
|------|---------|---------|
| OpenTofu | ≥ 1.8 | [opentofu.org](https://opentofu.org/docs/intro/install/) |
| AWS CLI | v2 | [aws.amazon.com/cli](https://aws.amazon.com/cli/) |
| kubectl | any | [kubernetes.io](https://kubernetes.io/docs/tasks/tools/) |
| helm | ≥ 3 | [helm.sh](https://helm.sh/docs/intro/install/) |

You also need:
- An AWS account with IAM permissions for EKS, EC2, IAM, ACM, and ELB
- A domain managed in Cloudflare (or any DNS provider)
- A managed Postgres database (Supabase, Neon, RDS, etc.)
- A managed Redis / Valkey instance (Aiven, Upstash, ElastiCache, etc.)

---

## Step 1 — Provision the EKS cluster

```bash
cd infra/terraform/aws-eks
cp terraform.tfvars.example terraform.tfvars   # if you haven't already
# Edit terraform.tfvars:
#   - Set region and cluster_name
#   - Set domain = "yourdomain.com"  ← creates ACM cert + ALB controller IAM role
tofu init
tofu apply
```

After apply, note the outputs you'll need in later steps:

```bash
tofu output update_kubeconfig_command   # run this to configure kubectl
tofu output acm_certificate_arn         # paste into k8s/terraform.tfvars
tofu output acm_dns_validation_records  # add these to your DNS
tofu output setup_alb_controller_command
tofu output object_storage_bucket
tofu output object_storage_role_arn
```

The EKS module creates a private, encrypted, versioned S3 bucket and an IRSA
role for the control plane. Pass `object_storage_bucket` and
`object_storage_role_arn` into the corresponding values shown in
`k8s/terraform.tfvars.example`. Workflow source and invocation payloads are
then stored under tenant-specific prefixes in this shared bucket.

---

## Step 2 — Validate the ACM certificate

ACM uses DNS validation. OpenTofu output gives you the CNAME record(s) to add:

```bash
tofu output acm_dns_validation_records
# Example output:
# [{ name = "_abc123.yourdomain.com.", type = "CNAME", value = "_def456.acm-validations.aws." }]
```

**In Cloudflare (or your DNS provider):**
1. Add a CNAME record with the `name` and `value` from the output
2. Set proxy status to **DNS only (grey cloud)** — ACM validation requires a direct CNAME
3. Wait ~2 minutes for the certificate status to change to `ISSUED`

Check certificate status:
```bash
tofu output acm_certificate_status
# or
aws acm describe-certificate --certificate-arn $(tofu output -raw acm_certificate_arn) \
  --query 'Certificate.Status' --output text
```

---

## Step 3 — Install the AWS Load Balancer Controller

The ALB controller is a Kubernetes controller that creates an Application Load Balancer from your Ingress resources. Run the script produced by step 1:

```bash
# Refresh kubeconfig first
$(tofu output -raw update_kubeconfig_command)

# Then run the install script (copy the exact command from tofu output)
bash infra/scripts/setup-alb-controller.sh \
  --cluster YOUR_CLUSTER_NAME \
  --region  us-east-1 \
  --role-arn $(tofu output -raw alb_controller_role_arn)
```

Verify it's running:
```bash
kubectl get deployment aws-load-balancer-controller -n kube-system
```

---

## Step 4 — Deploy Velane workloads

```bash
cd infra/terraform/k8s
cp terraform.tfvars.example terraform.tfvars
```

Edit `terraform.tfvars` — required values:

| Variable | Where to get it |
|----------|----------------|
| `kubeconfig_context` | From `aws eks update-kubeconfig` alias |
| `database_url` | Your Postgres DSN |
| `redis_url` | Your Redis DSN |
| `encryption_key` | `openssl rand -hex 32` |
| `jwt_private_key_pem` | `openssl genrsa 2048 \| openssl pkcs8 -topk8 -nocrypt` |
| `base_domain` | Your root domain, e.g. `yourdomain.com` |
| `acm_certificate_arn` | From Step 1: `tofu -chdir=../aws-eks output -raw acm_certificate_arn` |
| `nango_secret_key` | `python -c "import uuid; print(uuid.uuid4())"` |
| `nango_public_key` | Same command, different value |
| `google_oauth_client_id` / `google_oauth_client_secret` | [Google Cloud Console](https://console.cloud.google.com/) → APIs & Services → Credentials (optional) |
| `github_oauth_client_id` / `github_oauth_client_secret` | GitHub → Settings → Developer settings → OAuth Apps (optional) |

After apply, get OAuth redirect URIs to register with each provider:

```bash
tofu output oauth_redirect_uris
```

Then apply:

```bash
tofu init
tofu apply
```

---

## Step 5 — Point your domain to the ALB

Once OpenTofu creates the Ingress, get the ALB hostname:

```bash
kubectl get ingress -n velane velane -o jsonpath='{.status.loadBalancer.ingress[0].hostname}'
# Example: k8s-velane-velane-abc123.us-east-1.elb.amazonaws.com
```

**In Cloudflare**, add a wildcard CNAME:

| Type | Name | Target | Proxy |
|------|------|--------|-------|
| CNAME | `*` | `<ALB hostname from above>` | Proxied (orange cloud) ✓ |

Or add individual subdomains if you prefer not to use a wildcard:

| CNAME | `admin` | `<ALB hostname>` | Proxied |
| CNAME | `api` | `<ALB hostname>` | Proxied |
| CNAME | `mcp` | `<ALB hostname>` | Proxied |
| CNAME | `connect` | `<ALB hostname>` | Proxied |
| CNAME | `nango` | `<ALB hostname>` | Proxied |

With Cloudflare proxy enabled (orange cloud), Cloudflare provides an extra layer of DDoS protection and the traffic Cloudflare→ALB is HTTPS (your ACM cert handles that leg).

---

## Step 6 — Set up Nango admin account

After first deploy, create the Nango admin account:

```bash
NANGO_POD=$(kubectl get pod -n velane -l app=nango -o jsonpath='{.items[0].metadata.name}')

# Create admin account
kubectl exec -n velane "$NANGO_POD" -- node -e "
const { createAdminUser } = require('./packages/shared/lib/utils/admin.js');
createAdminUser({ email: 'admin@yourdomain.com', password: 'your-secure-password' })
  .then(() => { console.log('done'); process.exit(0); })
  .catch(e => { console.error(e); process.exit(1); });
"
```

Then verify the email directly in your Postgres database (Nango uses Supabase Auth internally):
```sql
UPDATE auth.users SET email_confirmed_at = now() WHERE email = 'admin@yourdomain.com';
```

---

## Architecture overview

```
Internet
   │
   ▼
Cloudflare (HTTPS 443, DDoS protection)
   │
   ▼
AWS ALB (single load balancer, ACM cert)
   │  ├── admin.yourdomain.com   → admin pod (port 80)
   │  ├── api.yourdomain.com     → control-plane pod (port 8080)
   │  ├── mcp.yourdomain.com     → mcp-server pod (port 8090)
   │  ├── connect.yourdomain.com → nango pod (port 3009, Connect UI)
   │  └── nango.yourdomain.com   → nango pod (port 3003, OAuth callbacks)
   │
   └── All traffic stays within the VPC after the ALB
```

---

## Subdomain reference

| URL | Service | Purpose |
|-----|---------|---------|
| `admin.yourdomain.com` | admin | Web UI for managing snippets, integrations, API keys |
| `api.yourdomain.com` | control-plane | REST API (`vl_` API keys, session tokens) |
| `mcp.yourdomain.com` | mcp-server | MCP protocol endpoint for Cursor / Claude |
| `connect.yourdomain.com` | nango | OAuth popup UI (opened in browser during connection flow) |
| `nango.yourdomain.com` | nango | OAuth callback URL (set this in your OAuth app registrations) |

---

## Local deploy (recommended for now)

From the repo root, with AWS creds in `.env`:

```bash
bash infra/scripts/deploy-local.sh          # uses latest git tag, or sha-<commit>
bash infra/scripts/deploy-local.sh 0.5.10   # pin a specific image tag
```

This updates image tags in `terraform.tfvars`, refreshes kubeconfig, and runs `tofu apply`.

---

## CI/CD (GitHub Actions)

Pushes to `main` build Docker images tagged `latest` and `sha-<commit>`. EKS deployment remains a manual OpenTofu operation using the steps below.

Semver tags are created only via **Actions → Release → Run workflow** (patch/minor/major bump). That workflow builds versioned images and publishes grouped release notes on GitHub.

### One-time setup

1. **Remote state** — CI needs S3-backed state (local `terraform.tfstate` is not available in Actions):
   ```bash
   cd infra/terraform/k8s
   cp backend.hcl.example backend.hcl   # edit bucket/table names
   tofu init -backend-config=backend.hcl -migrate-state
   ```

2. **GitHub repository secrets** — copy values from your local `.env` and `terraform.tfvars`:

   | Secret | Source |
   |--------|--------|
   | `AWS_ACCESS_KEY_ID` | `.env` |
   | `AWS_SECRET_ACCESS_KEY` | `.env` |
   | `AWS_REGION` | e.g. `us-east-1` |
   | `EKS_CLUSTER_NAME` | e.g. `velane-us-east-1` |
   | `TF_STATE_BUCKET` | S3 bucket from `backend.hcl` |
   | `TF_STATE_LOCK_TABLE` | DynamoDB table from `backend.hcl` |
   | `K8S_TFVARS` | Full contents of your local `terraform.tfvars` |

3. **Manual deploy** — run the local deployment command above or apply the OpenTofu stack directly.

Pin `control_plane_image`, executor images, and frontend images to the desired semantic-version or `sha-<commit>` tag.

---

## Updating a deployment

To deploy a new image tag:

```bash
# Option 1: update the variable and re-apply
# In terraform.tfvars:
#   control_plane_image = "ghcr.io/abskrj/velane-control-plane:sha-abc123"
tofu apply

# Option 2: quick rollout without re-applying OpenTofu
kubectl set image deployment/control-plane control-plane=ghcr.io/abskrj/velane-control-plane:sha-abc123 -n velane
```

---

## Without a custom domain (quick start)

If you just want running endpoints without a domain:

1. Keep `enable_ingress = false`
2. Set service types to `LoadBalancer`
3. Apply — three separate Classic ELBs are created
4. Get their hostnames:
   ```bash
   kubectl get svc -n velane
   ```

You can add a custom domain later by enabling ingress and following this guide from Step 3.

---

## What this stack provisions

| Resource | Count | Notes |
|----------|-------|-------|
| Kubernetes Namespace | 1 | `velane` |
| Kubernetes Secrets | 2 | control-plane + nango secrets |
| Deployments | 6 | control-plane, bun-executor, python-executor, admin, mcp-server, nango |
| Services | 6 | ClusterIP when ingress on, LoadBalancer when ingress off |
| Ingress | 1 | ALB with ACM cert (when `enable_ingress = true`) |
| Kubernetes Job | 1 | One-time Nango database creation |

## What this stack does NOT provision

- The Kubernetes cluster itself → use `infra/terraform/aws-eks`
- Postgres, Redis, ClickHouse → bring your own managed instances
- DNS records → add them manually in Cloudflare / Route53 / etc.
- The AWS Load Balancer Controller → run `infra/scripts/setup-alb-controller.sh`
