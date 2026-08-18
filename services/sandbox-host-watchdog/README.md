# Sandbox host watchdog

A separate root-owned watchdog contract that independently default-denies sandbox traffic.

The watchdog accepts only control-plane-signed Ed25519 lease grants. It persists the accepted grant and local monotonic fail-close and kill deadlines before opening connectivity. On restart it default-denies every persisted sandbox path. Before expiry it denies network and pauses the VM; at expiry it kills the VM, removes network state, and deletes the lease record.

The host agent must run under an unprivileged account and cannot own the watchdog state, socket, nftables table, or VM fence handles. The watchdog exposes a root-owned Unix control socket, group-readable only by the agent service account, and accepts only signed grants. It programs an nftables hook chain in each sandbox network namespace and fences the PID from a root-owned handle. Missing nftables, namespace, or PID handles are command failures, never successful fencing.

Run `scripts/run-privileged-integration.sh` on the isolated root-capable Linux host-image gate to exercise real nftables; ordinary unit tests use recording fakes and do not claim privileged behavior succeeded.
