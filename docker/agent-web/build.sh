#!/bin/bash
cd "$(dirname "$0")/../.."
echo "[INFO] $(date '+%Y-%m-%d %H:%M:%S') build agent-web"
# The shared compile image is Node 22.6. Its corepack only has the old pnpm
# signing key, so `corepack prepare pnpm@10.28.2` exits with
# "Cannot find matching keyid". Newer corepack accepts COREPACK_INTEGRITY_KEYS=0.
# On the old image that variable is ignored, so install the same pnpm with npm.
if ! COREPACK_INTEGRITY_KEYS=0 corepack enable \
  || ! COREPACK_INTEGRITY_KEYS=0 corepack prepare pnpm@10.28.2 --activate; then
  echo "[WARN] $(date '+%Y-%m-%d %H:%M:%S') corepack could not activate pnpm@10.28.2; installing it with npm"
  corepack disable >/dev/null 2>&1 || true
  npm install -g pnpm@10.28.2
fi
pnpm install --frozen-lockfile
if [ $? -ne 0 ]; then
  echo "[ERROR] $(date '+%Y-%m-%d %H:%M:%S') Compilation failed, request the developer to confirm it."
  exit 1
fi
NODE_ENV=production STANDALONE=true pnpm --filter @multica/web build
if [ $? -ne 0 ]; then
  echo "[ERROR] $(date '+%Y-%m-%d %H:%M:%S') Compilation failed, request the developer to confirm it."
  exit 1
fi
