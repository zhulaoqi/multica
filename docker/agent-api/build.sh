#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/../.."
echo "[INFO] $(date '+%Y-%m-%d %H:%M:%S') build agent-api"
version="${VERSION:-dev}"
commit="${COMMIT:-unknown}"
(
  cd server
  CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${version} -X main.commit=${commit}" \
    -o bin/server ./cmd/server
  CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/migrate ./cmd/migrate
)
echo "[INFO] $(date '+%Y-%m-%d %H:%M:%S') agent-api binaries are in server/bin"
