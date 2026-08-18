# Sandbox host agent

Cloud-neutral, unprivileged host-side contracts for Firecracker sandbox lifecycle.

Safety properties encoded here:

- command fences are atomically journaled before VM side effects;
- restore validates exact lineage, immutable source host key, and VM descriptor before it can load a full bundle;
- manifests require `snapshot_mode: full`, Firecracker `Full`, memory, VM state, and every configured mutable drive;
- encrypted chunks require AES-256-GCM, unique nonces, associated data, and plaintext/ciphertext checksums;
- the privileged Firecracker/jailer/network operations are narrow interfaces that have fake-backed unit tests.

Production Firecracker, cgroup, XFS, namespace, nftables, and vsock execution requires the dedicated privileged Linux gate; this module does not claim to run those operations in ordinary CI. Startup validates executable Firecracker/jailer paths, `/dev/kvm`, cgroup v2, mTLS inputs, and the independent watchdog socket. Missing prerequisites fail startup, keeping the host non-placeable. `scripts/run-privileged-integration.sh` is the root/KVM host-image test gate.

## Wire contract

`internal/snapshot/testdata/snapshot-manifest-v1.json` is the canonical cross-service fixture shape. The private host API/control-plane validator must mirror every field in `SnapshotManifestV1`; it must not down-convert this manifest to a digest-only artifact list before verification. In particular it must preserve ordered drive inventory, topology/image digests, encryption/chunk metadata, full source key, and canonical checksum.
