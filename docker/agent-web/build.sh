#!/bin/bash
cd "$(dirname "$0")/../.."
echo "[INFO] $(date '+%Y-%m-%d %H:%M:%S') build agent-web"
corepack enable
corepack prepare pnpm@10.28.2 --activate
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
