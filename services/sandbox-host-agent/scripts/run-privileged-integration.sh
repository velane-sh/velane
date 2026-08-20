#!/usr/bin/env bash
set -euo pipefail

[[ $(id -u) -eq 0 ]] || { echo 'must run as root on an isolated sandbox-host image' >&2; exit 1; }
[[ -e /dev/kvm ]] || { echo '/dev/kvm is required' >&2; exit 1; }
command -v firecracker >/dev/null || { echo 'firecracker is required' >&2; exit 1; }
command -v jailer >/dev/null || { echo 'jailer is required' >&2; exit 1; }
cd "$(dirname "$0")/.."
go test -tags privileged ./...
