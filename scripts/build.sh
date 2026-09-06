#!/usr/bin/env bash
#
# Builds the detector and api-server images.
#
#   ./scripts/build.sh [tag]      # defaults to :local
#
# Engine is auto-detected; override with CONTAINER_ENGINE=podman.
# Podman needs a registry for unqualified names:
#   printf 'unqualified-search-registries = ["docker.io"]\n' \
#     > ~/.config/containers/registries.conf

set -euo pipefail

TAG="${1:-local}"

ENGINE="${CONTAINER_ENGINE:-}"
if [[ -z "$ENGINE" ]]; then
	if command -v docker >/dev/null 2>&1; then
		ENGINE=docker
	elif command -v podman >/dev/null 2>&1; then
		ENGINE=podman
	else
		echo "error: neither docker nor podman found on PATH" >&2
		exit 1
	fi
fi

cd "$(dirname "${BASH_SOURCE[0]}")/.."

echo "==> building with ${ENGINE} (tag: ${TAG})"
"$ENGINE" build --target detector   -t "arb-scanner-detector:${TAG}" .
"$ENGINE" build --target api-server -t "arb-scanner-api:${TAG}" .

echo "==> built"
"$ENGINE" images --filter reference='arb-scanner-*'
