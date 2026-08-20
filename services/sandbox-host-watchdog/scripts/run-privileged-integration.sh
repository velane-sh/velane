#!/usr/bin/env bash
set -euo pipefail

[[ $(id -u) -eq 0 ]] || { echo 'must run as root on an isolated sandbox-host image' >&2; exit 1; }
command -v nft >/dev/null || { echo 'nft is required' >&2; exit 1; }
cd "$(dirname "$0")/.."
go test -tags privileged ./...
