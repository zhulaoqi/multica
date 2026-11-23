#!/usr/bin/env bash
# Cross-compiles the CLI and publishes the archives into the web public
# directory that docker/agent-web/Dockerfile copies into the image.
#
#   scripts/build-cli-assets.sh
#
# The installers fetch /releases/latest/download/checksums.txt from the
# deployment origin. That file is not in git; it has to be produced before
# the web image is built. Set MULTICA_CLI_ASSETS to a directory of
# already-built multica-cli archives to skip compilation.
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ -n "${MULTICA_CLI_ASSETS:-}" ]]; then
  exec "${root_dir}/scripts/publish-cli-assets.sh" "${MULTICA_CLI_ASSETS}"
fi

if ! command -v go >/dev/null 2>&1; then
  echo "go is not installed; set MULTICA_CLI_ASSETS to a directory of multica-cli archives" >&2
  exit 1
fi

version="${VERSION:-}"
if [[ -z "$version" ]]; then
  version="$(git -C "$root_dir" describe --tags --always 2>/dev/null || true)"
fi
if [[ -z "$version" ]]; then
  version="dev"
fi
version="${version//\//-}"

commit="${COMMIT:-}"
if [[ -z "$commit" ]]; then
  commit="$(git -C "$root_dir" rev-parse --short HEAD 2>/dev/null || true)"
fi
if [[ -z "$commit" ]]; then
  commit="unknown"
fi

full_commit="${FULL_COMMIT:-}"
if [[ -z "$full_commit" ]]; then
  full_commit="$(git -C "$root_dir" rev-parse HEAD 2>/dev/null || true)"
fi
if [[ -z "$full_commit" ]]; then
  full_commit="unknown"
fi

built_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT

pack() {
  local bin="$1" archive="$2" member="$3"
  case "$archive" in
    *.zip)
      if command -v python3 >/dev/null 2>&1; then
        python3 -c 'import sys, zipfile
z = zipfile.ZipFile(sys.argv[1], "w", compression=zipfile.ZIP_DEFLATED)
z.write(sys.argv[2], arcname=sys.argv[3])
z.close()' "$archive" "$bin" "$member"
      elif command -v zip >/dev/null 2>&1; then
        local dir
        dir="$(mktemp -d)"
        cp "$bin" "$dir/$member"
        (cd "$dir" && zip -q "$archive" "$member")
        rm -rf "$dir"
      else
        echo "need python3 or zip to pack $archive" >&2
        exit 1
      fi
      ;;
    *.tar.gz)
      tar -czf "$archive" -C "$(dirname "$bin")" "$member"
      ;;
    *)
      echo "unsupported archive: $archive" >&2
      exit 1
      ;;
  esac
}

targets=(
  "darwin amd64 tar.gz multica"
  "darwin arm64 tar.gz multica"
  "linux amd64 tar.gz multica"
  "linux arm64 tar.gz multica"
  "windows amd64 zip multica.exe"
  "windows arm64 zip multica.exe"
)

for spec in "${targets[@]}"; do
  read -r goos goarch ext member <<<"$spec"
  echo "building multica-cli-${version}-${goos}-${goarch}.${ext}"
  bin_dir="${stage}/bin-${goos}-${goarch}"
  mkdir -p "$bin_dir"
  (
    cd "${root_dir}/server"
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -buildvcs=false \
      -ldflags "-s -w -X main.version=${version} -X main.commit=${commit} -X main.buildCommit=${full_commit} -X main.date=${built_at}" \
      -o "${bin_dir}/${member}" ./cmd/multica
  )
  pack "${bin_dir}/${member}" "${stage}/multica-cli-${version}-${goos}-${goarch}.${ext}" "$member"
done

"${root_dir}/scripts/publish-cli-assets.sh" "$stage"
