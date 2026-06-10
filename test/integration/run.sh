#!/usr/bin/env bash
# Runs the integration configuration against each distribution in Docker.
# Set DISTROS to override, e.g. DISTROS="ubuntu:24.04" ./run.sh
set -euo pipefail

cd "$(dirname "$0")"

DISTROS=${DISTROS:-"ubuntu:24.04 debian:12 fedora:41 archlinux:latest"}

echo "Building integration binary"
CGO_ENABLED=0 GOOS=linux go build -o viaduct-integration .

for image in $DISTROS; do
	tag="viaduct-integration:$(echo "$image" | tr ':/' '--')"

	echo "=== Testing against ${image} ==="
	docker build --build-arg "IMAGE=${image}" -t "$tag" .
done

echo "All integration tests passed"
