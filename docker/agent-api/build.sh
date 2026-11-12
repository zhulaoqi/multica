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
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
    -ldflags "-s -w -X main.version=${version} -X main.commit=${commit}" \
    -o "../${out}/server" ./cmd/server
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" \
    -o "../${out}/migrate" ./cmd/migrate
)
echo "[INFO] $(date '+%Y-%m-%d %H:%M:%S') agent-api binaries are in ${out}"
