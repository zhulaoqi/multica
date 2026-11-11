#!/bin/sh
set -eu
cd /dianyi/app/agent-api
. ./runtime-limits.sh
. ./company-log.sh
prepare_company_log

case "${PROFILE:-test-ali}" in
  prod|PRO|pro|prod-ali) export PROFILE=prod-ali ;;
  *) export PROFILE=test-ali ;;
esac

./migrate up

# Go's GC guide: keep the soft limit about 10% under the cgroup so the
# process returns memory before the kernel kills it. Operators can pin
# GOMEMLIMIT or GOMAXPROCS; those values are left alone.
if [ -z "${GOMEMLIMIT:-}" ]; then
  if mem=$(runtime_memory_bytes); then
    percent=${GO_MEM_PERCENT:-90}
    export GOMEMLIMIT="$(( mem * percent / 100 ))B"
  fi
fi
if [ -z "${GOMAXPROCS:-}" ]; then
  export GOMAXPROCS="$(runtime_cpu_count)"
fi
if [ -z "${GOGC:-}" ]; then
  export GOGC=100
fi
if [ -z "${GOTRACEBACK:-}" ]; then
  export GOTRACEBACK=crash
fi

write_startup_banner agent-api \
  "gomemlimit: ${GOMEMLIMIT:-unset}" \
  "gomaxprocs: ${GOMAXPROCS}" \
  "gogc: ${GOGC}" \
  "port: 8080"
exec ./server
