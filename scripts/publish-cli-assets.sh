#!/usr/bin/env bash
# Publishes CLI release archives into the web image so the internal install
# chain can serve them without reaching GitHub.
#
# Usage:
#   scripts/publish-cli-assets.sh <dir-with-archives> [--dest apps/web/public/releases/latest/download]
#
# The destination is served at /releases/latest/download/<file>, which is where
# apps/web/install-scripts/install.{sh,ps1} resolve the archive and
# checksums.txt. The published directory is gitignored: assets are build
# inputs, refreshed on every deploy, not repository content.
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dest="$root_dir/apps/web/public/releases/latest/download"

if [[ $# -lt 1 || $# -gt 3 ]]; then
  echo "usage: $0 <dir-with-archives> [--dest <dir>]" >&2
  exit 2
fi

src="$1"
if [[ $# -eq 3 ]]; then
  if [[ "$2" != "--dest" ]]; then
    echo "unknown option: $2" >&2
    exit 2
  fi
  dest="$3"
fi

if [[ ! -d "$src" ]]; then
  echo "archive directory not found: $src" >&2
  exit 1
fi

# Desktop accepts the versioned `multica-cli-<v>-<os>-<arch>` naming and the
# legacy `multica_<os>_<arch>` fallback, so publish whichever of the two the
# build produced.
archives=()
while IFS= read -r file; do
  archives+=("$file")
done < <(find "$src" -maxdepth 1 -type f \( -name 'multica-cli-*' -o -name 'multica_*' \) | sort)

if [[ ${#archives[@]} -eq 0 ]]; then
  echo "no multica-cli-* or multica_* archives in $src" >&2
  exit 1
fi

# checksums.txt must list only the files installers can actually fetch, and
# installers match archive names by pattern, so reject anything unexpected
# rather than publishing a partial set that fails halfway through a download.
for file in "${archives[@]}"; do
  name="$(basename "$file")"
  case "$name" in
    *.tar.gz | *.zip) ;;
    *)
      echo "refusing to publish unsupported archive: $name" >&2
      exit 1
      ;;
  esac
done

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$@"
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$@"
  else
    echo "need sha256sum or shasum to publish checksums" >&2
    exit 1
  fi
}

rm -rf "$dest"
mkdir -p "$dest"

for file in "${archives[@]}"; do
  cp "$file" "$dest/"
done

# `<hex>  <filename>` in sorted order: both installers parse this format.
(
  cd "$dest"
  sha256 $(printf '%s\n' "${archives[@]##*/}" | sort) >checksums.txt
)

echo "published ${#archives[@]} archive(s) to ${dest#"$root_dir"/}"
ls -1 "$dest"
