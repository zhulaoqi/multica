#!/bin/sh
set -eu
APP_HOME=/dianyi/app/agent-web
. "$APP_HOME/runtime-limits.sh"
. "$APP_HOME/company-log.sh"
prepare_company_log

export NODE_ENV=production
export PORT=80
export HOSTNAME=0.0.0.0

# V8's old space is not the whole process. 70% matches the company JVM cap
# and leaves the rest for buffers, code, and the HTTP stack.
if [ -z "${NODE_OPTIONS:-}" ]; then
  if mem=$(runtime_memory_bytes); then
    percent=${NODE_HEAP_PERCENT:-70}
    mb=$(( mem * percent / 100 / 1024 / 1024 ))
    if [ "$mb" -lt 64 ]; then
      mb=64
    fi
    export NODE_OPTIONS="--max-old-space-size=${mb}"
  fi
fi
if [ -z "${UV_THREADPOOL_SIZE:-}" ]; then
  pool=$(runtime_cpu_count)
  if [ "$pool" -lt 4 ]; then
    pool=4
  fi
  if [ "$pool" -gt 8 ]; then
    pool=8
  fi
  export UV_THREADPOOL_SIZE=$pool
fi

write_startup_banner agent-web \
  "node_options: ${NODE_OPTIONS:-unset}" \
  "uv_threadpool: ${UV_THREADPOOL_SIZE}" \
  "port: ${PORT}"
cd "$APP_HOME/apps/web"
exec node server.js
