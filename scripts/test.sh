#!/usr/bin/env bash
#
# Runs gofmt, go vet and the unit tests.
#
#   ./scripts/test.sh [extra go test args...]
#
# Uses a local Go toolchain when one is installed, otherwise runs inside the
# same golang image the Dockerfile builds with, so no local Go is required.
# Override the container engine with CONTAINER_ENGINE, the image with GO_IMAGE.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

CHECKS='
set -eu
unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then
	echo "gofmt: files need formatting:" >&2
	echo "$unformatted" >&2
	exit 1
fi
go vet ./...
go test ./... "$@"
'

if command -v go >/dev/null 2>&1; then
	echo "==> using local go: $(go version)"
	sh -c "$CHECKS" -- "$@"
	exit 0
fi

ENGINE="${CONTAINER_ENGINE:-}"
if [[ -z "$ENGINE" ]]; then
	if command -v docker >/dev/null 2>&1; then
		ENGINE=docker
	elif command -v podman >/dev/null 2>&1; then
		ENGINE=podman
	else
		echo "error: no local go, and neither docker nor podman found on PATH" >&2
		exit 1
	fi
fi

GO_IMAGE="${GO_IMAGE:-docker.io/library/golang:1.27-alpine}"
echo "==> no local go; running in ${GO_IMAGE} via ${ENGINE}"

exec "$ENGINE" run --rm \
	-v "$PWD":/src:z \
	-w /src \
	-e GOFLAGS=-buildvcs=false \
	-e LIVE_DURATION="${LIVE_DURATION:-}" \
	"$GO_IMAGE" sh -c "$CHECKS" -- "$@"
