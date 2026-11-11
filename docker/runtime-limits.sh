# Container memory and CPU limits. Source this file; it does not start a process.
# CGROUP_ROOT defaults to /sys/fs/cgroup so tests can point at a fixture.

cgroup_root() {
  printf '%s\n' "${CGROUP_ROOT:-/sys/fs/cgroup}"
}

# Prints the cgroup memory limit in bytes. Returns 1 when the limit is unset
# or the platform "unlimited" sentinel.
runtime_memory_bytes() {
  root=$(cgroup_root)
  if [ -f "$root/memory.max" ]; then
    v=$(cat "$root/memory.max")
    if [ -n "$v" ] && [ "$v" != "max" ]; then
      printf '%s\n' "$v"
      return 0
    fi
  fi
  if [ -f "$root/memory/memory.limit_in_bytes" ]; then
    v=$(cat "$root/memory/memory.limit_in_bytes")
    if [ -n "$v" ] && [ "$v" -lt 9223372036854771712 ]; then
      printf '%s\n' "$v"
      return 0
    fi
  fi
  return 1
}

# Prints the whole CPUs the container may use, rounded up. A 1.5 quota is 2.
# An unlimited cgroup falls back to nproc, then to 1.
runtime_cpu_count() {
  root=$(cgroup_root)
  if [ -f "$root/cpu.max" ]; then
    read -r quota period < "$root/cpu.max"
    if [ "$quota" != "max" ] && [ "${period:-0}" -gt 0 ]; then
      echo $(( (quota + period - 1) / period ))
      return 0
    fi
  fi
  if [ -f "$root/cpu/cpu.cfs_quota_us" ] && [ -f "$root/cpu/cpu.cfs_period_us" ]; then
    quota=$(cat "$root/cpu/cpu.cfs_quota_us")
    period=$(cat "$root/cpu/cpu.cfs_period_us")
    if [ "${quota:-0}" -gt 0 ] && [ "${period:-0}" -gt 0 ]; then
      echo $(( (quota + period - 1) / period ))
      return 0
    fi
  fi
  if command -v nproc >/dev/null 2>&1; then
    nproc
    return 0
  fi
  echo 1
}
