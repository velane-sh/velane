# Sandbox image builder

Cloud-neutral image recipe validation and controlled-input contracts. Recipes require Linux, a digest-pinned base, immutable package/repository metadata, exact package versions, and immutable profile targets. Bootstrap scripts are bounded and require all external inputs to be credential-free HTTPS URLs with an exact digest and size.

The validating fetch path rejects URL userinfo, fragments, queries, literal IPs, private/loopback/link-local DNS answers, redirects, and declared oversize or checksum-mismatched bytes. Bootstrap runs in a new user, mount, PID, and network namespace with no inherited credentials and no network interfaces. Namespace enforcement belongs to a dedicated privileged image-build runner, not ordinary CI.

## Production one-shot command

The executable processes exactly one immutable recipe and exits non-zero on any partial failure. It requires:

- `VELANE_SANDBOX_IMAGE_RECIPE_JSON`: recipe JSON file;
- `VELANE_SANDBOX_IMAGE_BASE_ROOTFS_DIR`: already verified base rootfs tree;
- `VELANE_SANDBOX_IMAGE_GUEST_KERNEL` and `VELANE_SANDBOX_IMAGE_GUEST_INIT`: pinned guest artifacts;
- `VELANE_SANDBOX_IMAGE_PACKAGE_INSTALLER`: operator-owned locked-package installer command that reads the exact package schema from stdin;
- `VELANE_SANDBOX_IMAGE_SIGNING_PRIVATE_KEY_BASE64`: Ed25519 private key;
- `VELANE_SANDBOX_IMAGE_OUTPUT_DIR`: local manifest/SBOM/provenance output;
- `VELANE_SANDBOX_IMAGE_ARTIFACT_DIR`: durable staging directory for content-addressed artifacts.

The package installer must enforce `repository_snapshot`, `index_digest`, `lock_digest`, and every exact package `{name,version,digest}`. The builder never translates this schema into floating package-manager arguments. The artifact directory uploader writes with an atomic rename, fsyncs, and verifies the final object before the signed manifest can be emitted.

Example:

```bash
VELANE_SANDBOX_IMAGE_RECIPE_JSON=/run/velane/recipe.json \
VELANE_SANDBOX_IMAGE_BASE_ROOTFS_DIR=/srv/velane/base-rootfs \
VELANE_SANDBOX_IMAGE_GUEST_KERNEL=/srv/velane/kernel \
VELANE_SANDBOX_IMAGE_GUEST_INIT=/srv/velane/guest-init \
VELANE_SANDBOX_IMAGE_PACKAGE_INSTALLER=/usr/local/libexec/velane-install-locked-packages \
VELANE_SANDBOX_IMAGE_SIGNING_PRIVATE_KEY_BASE64="$SIGNING_KEY" \
VELANE_SANDBOX_IMAGE_OUTPUT_DIR=/var/lib/velane/image-build/output \
VELANE_SANDBOX_IMAGE_ARTIFACT_DIR=/var/lib/velane/image-build/published \
./velane-sandbox-image-builder
```

The signed build manifest contains the runtime guest/root-drive artifact manifest consumed by the control plane, plus uploaded CycloneDX SBOM and in-toto/SLSA provenance descriptors.
