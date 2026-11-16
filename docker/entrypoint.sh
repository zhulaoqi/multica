#!/bin/sh
set -eu

# Schema changes are applied by a DBA from deploy/dba. This process only
# starts the API. The timestamp bounds how long the server retries a
# database that is temporarily unreachable.
export MULTICA_INTERNAL_DATABASE_STARTUP_STARTED_AT_UNIX="$(date +%s)"

echo "Starting server..."
exec ./server
