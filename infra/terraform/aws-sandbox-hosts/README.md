# AWS sandbox-host capacity

This additive OpenTofu stack provisions the AWS v1 capacity boundary for durable
sandbox hosts. It is deliberately separate from `aws-eks`: the Auto Scaling
group is homogeneous, hosts run only in dedicated private subnets, and no
resource exposes a Firecracker host or host-control API to the public Internet.

The stack is safe to apply before sandbox rollout: its default `min_hosts`,
`desired_hosts`, and `max_hosts` values are all `0`. A zero-capacity stack still
creates the private connectivity, encryption, artifact, lifecycle-notification,
and host-control endpoint contracts. It does not launch a host until an operator
explicitly changes the three capacity inputs.

## What this stack creates

- Dedicated host-only private subnets, each with a new route table that has no
  Internet/NAT route. Hosts get no public IP address and their security group has
  no ingress rule.
- Gateway S3 and interface KMS, STS, CloudWatch, and Systems Manager endpoints.
  The host security group permits TCP/443 only to the regional S3 managed prefix
  list, so gateway endpoint multipart uploads work without a NAT or broad
  Internet egress. Other host egress is limited to the private host-control
  NLB, endpoint TLS, and VPC DNS. Guest Internet access is not implied by host
  networking and must be provided by the host agent's explicit policy/egress
  design.
- An **internal TCP NLB** for the host-control listener. It preserves end-to-end
  mTLS to the private Go listener. Its targets must be pre-existing private API
  addresses; an empty `host_api_target_ips` set intentionally leaves it without
  healthy backends.
- A customer-managed, rotating KMS key plus dedicated, versioned S3 snapshot and
  access-log buckets. Snapshot writes require TLS and the stack's KMS key;
  incomplete multipart uploads are aborted after seven days. Artifact removal is
  limited to objects the control plane has explicitly tagged
  `sandbox-snapshot-state=deleted`.
- A launch template and one ASG for **one exact AMI, one instance type, one
  lineage, and one host compatibility key**. There is no mixed-instances policy.
  IMDSv2 is mandatory with hop limit one, root gp3 is encrypted, and no SSH key
  or public IP is configured.
- An instance role limited to SSM and host telemetry. It has no snapshot S3 or
  KMS permissions. Snapshot data keys and multipart URLs must come through the
  private control-plane protocol.
- Launch and termination lifecycle hooks delivered via EventBridge to SQS with a
  DLQ. The private control plane must complete launch only after compatible
  attested registration. Termination defaults to `ABANDON`, never `CONTINUE`:
  timeout is not evidence that a local VM or full bundle is safe to discard.
  During termination, the worker renews the hook before each heartbeat interval
  expires, drains, checkpoints full snapshots, verifies the remote bundle,
  releases/restores allocations, and only then explicitly completes the action
  with `CONTINUE`. A failed renewal, approaching global hook limit, missing
  acknowledgement, upload failure, or remaining VM/upload/only-local recoverable
  bundle is escalated as a durable drain failure and pages operators; it must not
  be completed as a successful drain. `ABANDON` stops any further hook chain but
  AWS may still terminate the instance, so the root watchdog and durable recovery
  head remain the fail-closed safety boundary.

## Prerequisites

- OpenTofu 1.8 or later, AWS CLI v2, and an encrypted remote state backend with
  access limited to operators. `terraform.tfvars` is ignored by Git.
- An existing VPC and a reserved CIDR range for **new** sandbox-host subnets in
  at least two AZs. Do not reuse public or EKS worker subnets.
- A promoted immutable host AMI containing the verified host agent and independent
  root watchdog. User data only configures that AMI; it never downloads host
  code, credentials, or mutable artifacts.
- One tested KVM-capable instance type. Use a bare-metal type until nested
  virtualization qualification is complete.
- Private host-control API target IPs and a TLS certificate whose private DNS
  name is supplied by `host_control_tls_server_name`.
- Secret *references* for the private host-control server certificate/key and
  host-client CA trust store. `host_control_server_certificate_secret_arn` and
  `host_control_client_ca_secret_arn` are passed only to the backend deployment
  contract; TCP NLB pass-through does not terminate TLS and cannot consume them.
  The immutable AMI contains the server CA at `host_control_ca_bundle_path`; the
  host creates its private key locally and enrollment writes its short-lived
  client certificate to the configured local paths. No PEM, client key, or
  plaintext host API route belongs in OpenTofu variables, user data, or public
  DNS.
- A private control-plane IAM role. Attach the sensitive
  `control_plane_policy_json` output to it and list its ARN in
  `control_plane_principal_arns`. That role—not a host instance role—performs
  lifecycle, capacity, snapshot, and KMS work.

## Configure and plan

```bash
cd infra/terraform/aws-sandbox-hosts
cp terraform.tfvars.example terraform.tfvars
# Set real VPC, CIDRs, AMI, exactly one qualified instance type, fresh lineage,
# canonical compatibility key, private TLS name, and private control-plane role.
tofu init
# Use your reviewed remote backend configuration in real environments.
tofu fmt -check
tofu validate
tofu plan -out=sandbox-hosts.tfplan
```

Review the plan before applying. The example intentionally keeps capacity at
zero. After the private host API, enrollment verifier, lifecycle worker, and
metrics are deployed, run an isolated canary with all three capacity inputs set
as follows:

```hcl
min_hosts     = 1
desired_hosts = 1
max_hosts     = 1
```

Apply only the reviewed plan:

```bash
tofu apply sandbox-hosts.tfplan
```

Keep the NLB and outputs operator-private. Do not create public DNS, an Internet
load balancer, public subnets, an SSH ingress rule, or a host API route in the
public control-plane router.

The generated `/etc/velane-sandbox-host/agent.env` is mode `0600` and is consumed
by both `velane-sandbox-host-agent.service` and the independent root watchdog via
`EnvironmentFile=`. It supplies only endpoint, CA/path, lineage, and local
runtime configuration; it does not contain certificate or private-key material.
It also supplies the private pool identifier, boot ID, and local runtime paths;
the agent measures usable inventory and reports explicit command capabilities
only after receiving its enrollment-issued mTLS certificate.

The private control-plane deployment must configure the same immutable values
as this ASG using `SANDBOX_HOST_EXPECTED_LINEAGE_ID` and
`SANDBOX_HOST_EXPECTED_COMPATIBILITY_KEY`, together with its AWS account,
region, ASG, AMI, launch-template, pool, and IID-root-CA settings. The listener
fails closed if either expected identity value is absent. These are deployment
configuration values, not host credentials and not user-data certificates.

The private host listener additionally requires
`SANDBOX_WATCHDOG_SIGNING_PRIVATE_KEY_BASE64`, a stable base64-encoded Ed25519
private key. Bake the matching base64 public key into the immutable host AMI at
`watchdog_public_key_path`; never put either key in user data or Terraform state.

## Capacity and lineage changes

The control-plane AWS capacity adapter must call `DescribeAutoScalingGroups`
before `SetDesiredCapacity`, no-op if already converged, and persist its
idempotent capacity action before making the AWS call. It scales compatible
CPU/RAM/disk/staging demand and oldest wait, not aggregate sandbox count.

Treat `ami_id`, `instance_type`, `host_lineage_id`, and
`host_compatibility_key` as an immutable set. A host compatibility change
requires a newly generated, never-reused lineage and a separate blue/green ASG.
Do not use Instance Refresh across changed lineage, host key, or VM restore
descriptors. Keep the old ASG available until no recovery head requires its
exact lineage/key pair.

ASG scale-in protection is enabled by default. The control plane is responsible
for removing protection only after a host is drained safely. Never use `force`
operations, disk-only restore, or a cold boot as a recovery fallback.

## Snapshot controls

The snapshot bucket is for complete full Firecracker bundles only: memory,
VM/device state, and every mutable disk. It is not a general object store and
must not be mounted or granted directly to hosts. The control plane issues
operation-scoped multipart URLs and wrapped data keys after fenced host
protocol checks. A snapshot becomes recoverable only after complete upload,
checksum, manifest, lineage/key, and VM restore descriptor verification.

The lifecycle expiration rule intentionally requires the control plane's deleted
object tag; lifecycle policies must never remove a current or only recoverable
snapshot by age alone.

## Validation

Run the static checks before every change:

```bash
tofu -chdir=infra/terraform/aws-sandbox-hosts fmt -check
tofu -chdir=infra/terraform/aws-sandbox-hosts init -backend=false
tofu -chdir=infra/terraform/aws-sandbox-hosts validate
tofu -chdir=infra/terraform/aws-sandbox-hosts plan -refresh=false \
  -var-file=terraform.tfvars
```

`terraform.tfvars.example` contains placeholders and is not applyable. Never
place lineage keys, target IPs, or other provider/operator details in public API
responses, tenant-visible logs, or committed configuration.
