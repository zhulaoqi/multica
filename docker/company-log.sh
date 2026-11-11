# Company log root and startup banner.
# /docker/log/$HOSTIP is the host directory; /dianyi/log is the stable link.
# Process logs stay on stderr so the platform collector still receives them.
# The banner is also written to $LOG_DIR/<name>.banner.

prepare_company_log() {
  host="${HOSTIP:-local}"
  root="${LOG_HOST_ROOT:-/docker/log}"
  dianyi="${DIANYI_ROOT:-/dianyi}"
  logs="$root/$host"
  mkdir -p "$logs" "$dianyi"
  ln -sfn "$logs" "$dianyi/log"
  export LOG_DIR="$dianyi/log"
  if [ -z "${LOG_LEVEL:-}" ]; then
    export LOG_LEVEL=info
  fi
}

write_startup_banner() {
  name="$1"
  shift
  banner="${LOG_DIR}/${name}.banner"
  {
    echo "================================"
    echo " ${name}"
    echo " profile: ${PROFILE:-unset}"
    echo " log_level: ${LOG_LEVEL}"
    echo " log_dir: ${LOG_DIR}"
    for line in "$@"; do
      echo " ${line}"
    done
    echo "================================"
  } | tee "$banner"
}
