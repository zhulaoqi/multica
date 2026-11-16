#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_DIR="$(mktemp -d)"

cleanup() {
  rm -rf "$TEST_DIR"
}
trap cleanup EXIT

cp "$ROOT_DIR/docker/entrypoint.sh" "$TEST_DIR/entrypoint.sh"

cat >"$TEST_DIR/migrate" <<'SCRIPT'
#!/bin/sh
printf 'migrate-called\n' >"$MIGRATE_CALLED_FILE"
exit 1
SCRIPT

cat >"$TEST_DIR/server" <<'SCRIPT'
#!/bin/sh
printf 'started\n' >"$SERVER_STARTED_FILE"
printf '%s\n' "$MULTICA_INTERNAL_DATABASE_STARTUP_STARTED_AT_UNIX" >"$SERVER_STARTED_AT_FILE"
SCRIPT

chmod +x "$TEST_DIR/entrypoint.sh" "$TEST_DIR/migrate" "$TEST_DIR/server"

export MIGRATE_CALLED_FILE="$TEST_DIR/migrate.called"
export SERVER_STARTED_FILE="$TEST_DIR/server.started"
export SERVER_STARTED_AT_FILE="$TEST_DIR/server.started-at"

(
  cd "$TEST_DIR"
  ./entrypoint.sh
)

if [ -e "$MIGRATE_CALLED_FILE" ]; then
  echo "entrypoint ran database migrations"
  exit 1
fi
if [ "$(cat "$SERVER_STARTED_FILE" 2>/dev/null || true)" != "started" ]; then
  echo "entrypoint did not start the server"
  exit 1
fi
started_at="$(cat "$SERVER_STARTED_AT_FILE" 2>/dev/null || true)"
case "$started_at" in
  "" | *[!0-9]*)
    echo "entrypoint database startup timestamp is not a Unix timestamp"
    exit 1
    ;;
esac

echo "entrypoint starts the server without migrating"
