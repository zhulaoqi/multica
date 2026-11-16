#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/../.."
echo "[INFO] $(date '+%Y-%m-%d %H:%M:%S') build agent-api"
version="${VERSION:-dev}"
commit="${COMMIT:-unknown}"
out="docker/agent-api/out"
mkdir -p "${out}"
(
  cd server
  # Jenkins checks out a copy whose git metadata is not readable by `go build`.
  # Version and commit are stamped with -X below when the pipeline sets them.
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false \
    -ldflags "-s -w -X main.version=${version} -X main.commit=${commit}" \
    -o "../${out}/server" ./cmd/server
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags "-s -w" \
    -o "../${out}/migrate" ./cmd/migrate
)
echo "[INFO] $(date '+%Y-%m-%d %H:%M:%S') agent-api binaries are in ${out}"
