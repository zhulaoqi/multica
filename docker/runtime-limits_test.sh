#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
. ./docker/runtime-limits.sh

fail() {
  echo "FAIL: $1" >&2
  exit 1
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
CGROUP_ROOT=$tmp
export CGROUP_ROOT

echo 4294967296 > "$tmp/memory.max"
echo "150000 100000" > "$tmp/cpu.max"
mem=$(runtime_memory_bytes)
cpu=$(runtime_cpu_count)
[ "$mem" = 4294967296 ] || fail "v2 memory = $mem"
[ "$cpu" = 2 ] || fail "v2 cpu = $cpu"
limit=$(( mem * 90 / 100 ))
[ "$limit" = 3865470566 ] || fail "90 percent = $limit"

rm -f "$tmp/memory.max" "$tmp/cpu.max"
mkdir -p "$tmp/memory" "$tmp/cpu"
echo 1073741824 > "$tmp/memory/memory.limit_in_bytes"
echo 50000 > "$tmp/cpu/cpu.cfs_quota_us"
echo 100000 > "$tmp/cpu/cpu.cfs_period_us"
mem=$(runtime_memory_bytes)
cpu=$(runtime_cpu_count)
[ "$mem" = 1073741824 ] || fail "v1 memory = $mem"
[ "$cpu" = 1 ] || fail "v1 half cpu = $cpu"

echo max > "$tmp/memory.max"
echo 9223372036854771712 > "$tmp/memory/memory.limit_in_bytes"
if runtime_memory_bytes >/dev/null 2>&1; then
  fail "unlimited memory should be absent"
fi

echo "runtime limits ok"
