# Container memory and CPU limits. Source this file; it does not start a process.
# CGROUP_ROOT defaults to /sys/fs/cgroup so tests can point at a fixture.
#
# Cloud test and production use the same rule. The only environment split is
# which config file the API loads. The budget is the memory applied for this
# pod: read that cgroup limit, then let the process use part of it.

cgroup_root() {
  printf '%s\n' "${CGROUP_ROOT:-/sys/fs/cgroup}"
}

# Relative path of this process inside the cgroup mount.
# v2: "0::/kubepods/.../container"
# v1: "4:memory:/kubepods/.../container"
# Empty or missing means the mount root.
self_cgroup_rel() {
  cg="${PROC_SELF_CGROUP:-/proc/self/cgroup}"
  if [ ! -r "$cg" ]; then
    return 1
  fi
  rel=$(sed -n 's/^0:://p' "$cg" | head -n 1)
  if [ -z "$rel" ]; then
    rel=$(awk -F: '/memory/ { print $NF; exit }' "$cg")
  fi
  if [ -z "$rel" ]; then
    return 1
  fi
  printf '%s\n' "$rel"
}

# Prints a memory limit file when it is a real byte cap. "max" and the v1
# unlimited sentinel are skipped so a parent cgroup does not hide the pod cap.
read_memory_limit() {
  f="$1"
  if [ ! -f "$f" ]; then
    return 1
  fi
  v=$(cat "$f")
  if [ -z "$v" ] || [ "$v" = "max" ]; then
    return 1
  fi
  if [ "$v" -ge 9223372036854771712 ]; then
    return 1
  fi
  printf '%s\n' "$v"
}

# Prints the cgroup memory limit in bytes. Returns 1 when the limit is unset
# or the platform "unlimited" sentinel. The process cgroup is checked first:
# on the cloud host the mount root is often the node, and the applied pod
# memory sits on this container's own cgroup.
runtime_memory_bytes() {
  root=$(cgroup_root)
  rel=""
  if rel=$(self_cgroup_rel); then
    case "$rel" in
      "" | /) ;;
      *)
        if read_memory_limit "$root$rel/memory.max"; then
          return 0
        fi
        if read_memory_limit "$root/memory$rel/memory.limit_in_bytes"; then
          return 0
        fi
        ;;
    esac
  fi
  if read_memory_limit "$root/memory.max"; then
    return 0
  fi
  if read_memory_limit "$root/memory/memory.limit_in_bytes"; then
    return 0
  fi
  return 1
}

# Prints the whole CPUs the container may use, rounded up. A 1.5 quota is 2.
# An unlimited cgroup falls back to nproc, then to 1.
runtime_cpu_count() {
  root=$(cgroup_root)
  v2="$root"
  v1="$root/cpu"
  rel=""
  if rel=$(self_cgroup_rel); then
    case "$rel" in
      "" | /) ;;
      *)
        if [ -f "$root$rel/cpu.max" ]; then
          v2="$root$rel"
        fi
        if [ -f "$root/cpu$rel/cpu.cfs_quota_us" ]; then
          v1="$root/cpu$rel"
        fi
        ;;
    esac
  fi
  if [ -f "$v2/cpu.max" ]; then
    read -r quota period < "$v2/cpu.max"
    if [ "$quota" != "max" ] && [ "${period:-0}" -gt 0 ]; then
      echo $(( (quota + period - 1) / period ))
      return 0
    fi
  fi
  if [ -f "$v1/cpu.cfs_quota_us" ] && [ -f "$v1/cpu.cfs_period_us" ]; then
    quota=$(cat "$v1/cpu.cfs_quota_us")
    period=$(cat "$v1/cpu.cfs_period_us")
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
