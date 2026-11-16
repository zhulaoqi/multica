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
# The company Nexus sometimes drops a tarball mid-download (Socket timeout).
# Retry the whole install; a later attempt reuses packages already in the store.
install_ok=0
for attempt in 1 2 3; do
  echo "[INFO] $(date '+%Y-%m-%d %H:%M:%S') pnpm install attempt ${attempt}"
  if pnpm install --frozen-lockfile \
    --fetch-timeout 180000 \
    --fetch-retries 5 \
    --fetch-retry-mintimeout 10000 \
    --fetch-retry-maxtimeout 120000 \
    --network-concurrency 8; then
    install_ok=1
    break
  fi
  echo "[WARN] $(date '+%Y-%m-%d %H:%M:%S') pnpm install failed, retrying"
  sleep 10
done
if [ "$install_ok" -ne 1 ]; then
  echo "[ERROR] $(date '+%Y-%m-%d %H:%M:%S') Compilation failed, request the developer to confirm it."
  exit 1
fi
# Node 22.6 cannot require() an ES module. fumadocs-mdx's loader is ESM, and
# Next loads it with require(). Node 22.12+ does this by default.
export NODE_OPTIONS="${NODE_OPTIONS:+$NODE_OPTIONS }--experimental-require-module"
NODE_ENV=production STANDALONE=true pnpm --filter @multica/web build
if [ $? -ne 0 ]; then
  echo "[ERROR] $(date '+%Y-%m-%d %H:%M:%S') Compilation failed, request the developer to confirm it."
  exit 1
fi
