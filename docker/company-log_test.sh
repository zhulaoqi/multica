#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
. ./docker/company-log.sh

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
export LOG_HOST_ROOT="$tmp/host"
export DIANYI_ROOT="$tmp/dianyi"
export HOSTIP="10.0.0.8"
unset LOG_LEVEL
prepare_company_log
write_startup_banner agent-api "port: 8080"
test -L "$tmp/dianyi/log"
test -f "$tmp/dianyi/log/agent-api.banner"
grep -q "profile:" "$tmp/dianyi/log/agent-api.banner"
grep -q "port: 8080" "$tmp/dianyi/log/agent-api.banner"
test "$LOG_LEVEL" = "info"
echo "company log ok"
