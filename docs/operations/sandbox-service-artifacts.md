---
title: Sandbox Service Artifacts
description: Build, package, and validate the standalone sandbox host, guest, and builder services.
sidebar_position: 5
---

# Sandbox service artifacts

The standalone sandbox services are packaged separately from the invocation
runtime images. They are operational artifacts with distinct installation
boundaries:

| Artifact | Published image | Installation boundary |
|---|---|---|
| Host agent | `velane-sandbox-host-agent` | Extract into the immutable sandbox-host AMI with its systemd unit. |
| Host watchdog | `velane-sandbox-host-watchdog` | Extract into the immutable sandbox-host AMI with its systemd unit and tmpfiles policy. |
| Guest init | `velane-sandbox-guest-init` | Inject into the immutable sandbox guest rootfs. |
| Image builder | `velane-sandbox-image-builder` | Run only on a dedicated, isolated image-builder host or container. |

These artifacts are **not** invocation images. Do not add them to the normal
Docker Compose stack, executor pool, or public deployment configuration. In
particular, the host-agent and watchdog images are artifact-only OCI images:
they contain files for AMI construction and intentionally have no runtime
entrypoint. Installing the host artifacts and enabling their units remains the
responsibility of the AMI build process. The AWS sandbox-host stack configures
an already-promoted AMI; user data must not download these images or binaries.

## Local build and test

Go modules remain independent. Run the ordinary compile and unit-test coverage
on a normal development machine or hosted CI runner:

```bash
make sandbox-services-build
make sandbox-services-test
```

Build all four local OCI packaging artifacts with:

```bash
make sandbox-artifacts
```

The host images need to be extracted by the AMI build process, the guest-init
binary needs to be copied into the guest rootfs build, and the builder image
needs to be scheduled only onto its isolated builder environment. A successful
container build does not prove that Firecracker, KVM, cgroups, or nftables are
available.

## Privileged integration gate

The normal CI workflow does not emulate privileged host behavior. It has an
explicit `Sandbox privileged integration` job that is skipped unless the
repository variable `SANDBOX_PRIVILEGED_RUNNER` names a dedicated self-hosted
runner label.

That runner must be an isolated Linux sandbox-host image and provide:

- a root-capable job account (`sudo` is used by the workflow);
- `/dev/kvm` and cgroup v2;
- `firecracker` and `jailer` on `PATH` for the host-agent contract;
- `nft` and the required network administration capability for the watchdog
  contract;
- Go 1.26 or the ability for `actions/setup-go` to install it.

The job runs the checked-in privileged scripts:

```bash
sudo services/sandbox-host-agent/scripts/run-privileged-integration.sh
sudo services/sandbox-host-watchdog/scripts/run-privileged-integration.sh
```

Never repoint this job at a shared production host, a public runner, or a
runner that can reach tenant workloads. The gate validates that the dedicated
host-image prerequisites and real nftables control path exist; it does not
replace AMI promotion, rootfs construction, or a full isolated workload test.
