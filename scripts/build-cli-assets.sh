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

# The web image is compiled in a Node container that does not have Go.
# Download the toolchain go.mod asks for, then cross-compile from there.
go_version="$(awk '/^go / { print $2; exit }' "${root_dir}/server/go.mod")"
if [[ -z "$go_version" ]]; then
  echo "could not read the Go version from server/go.mod" >&2
  exit 1
fi

if ! command -v go >/dev/null 2>&1; then
  case "$(uname -s)" in
    Linux) go_os=linux ;;
    Darwin) go_os=darwin ;;
    *)
      echo "unsupported OS for the Go toolchain: $(uname -s)" >&2
      exit 1
      ;;
  esac
  case "$(uname -m)" in
    x86_64 | amd64) go_arch=amd64 ;;
    aarch64 | arm64) go_arch=arm64 ;;
    *)
      echo "unsupported architecture for the Go toolchain: $(uname -m)" >&2
      exit 1
      ;;
  esac
  if ! command -v node >/dev/null 2>&1; then
    echo "node is required to download the Go toolchain" >&2
    exit 1
  fi
  go_sdk="${root_dir}/.cache/go-sdk/${go_version}-${go_os}-${go_arch}"
  if [[ ! -x "${go_sdk}/bin/go" ]]; then
    tarball="go${go_version}.${go_os}-${go_arch}.tar.gz"
    tmp_sdk="$(mktemp -d)"
    echo "downloading Go ${go_version} (${go_os}/${go_arch})"
    if ! node - "$tarball" "$tmp_sdk/go.tgz" <<'NODE'
const fs = require("fs");
const https = require("https");
const http = require("http");

const name = process.argv[2];
const dest = process.argv[3];
const urls = [
  process.env.GO_TARBALL_URL,
  `https://mirrors.aliyun.com/golang/${name}`,
  `https://golang.google.cn/dl/${name}`,
  `https://dl.google.com/go/${name}`,
].filter(Boolean);

function get(url) {
  return new Promise((resolve, reject) => {
    const lib = url.startsWith("https:") ? https : http;
    const req = lib.get(url, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        res.resume();
        resolve(get(new URL(res.headers.location, url).href));
        return;
      }
      if (res.statusCode !== 200) {
        res.resume();
        reject(new Error(`HTTP ${res.statusCode} for ${url}`));
        return;
      }
      const out = fs.createWriteStream(dest);
      res.pipe(out);
      out.on("finish", () => out.close(() => resolve()));
      out.on("error", reject);
    });
    req.on("error", reject);
  });
}

(async () => {
  let last = "no download URL";
  for (const url of urls) {
    try {
      await get(url);
      return;
    } catch (err) {
      last = `${url}: ${err.message}`;
      fs.rmSync(dest, { force: true });
    }
  }
  console.error(last);
  process.exit(1);
})();
NODE
    then
      echo "could not download the Go ${go_version} toolchain" >&2
      rm -rf "$tmp_sdk"
      exit 1
    fi
    tar -xzf "${tmp_sdk}/go.tgz" -C "$tmp_sdk"
    mkdir -p "${root_dir}/.cache/go-sdk"
    rm -rf "$go_sdk"
    mv "${tmp_sdk}/go" "$go_sdk"
    rm -rf "$tmp_sdk"
  fi
  export GOROOT="$go_sdk"
  export PATH="${go_sdk}/bin:${PATH}"
  export GOTOOLCHAIN=local
  export GOCACHE="${root_dir}/.cache/go-build"
  export GOMODCACHE="${root_dir}/.cache/go-mod"
  export GOPROXY="${GOPROXY:-https://goproxy.cn,https://proxy.golang.org,direct}"
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
      if command -v node >/dev/null 2>&1; then
        node - "$bin" "$archive" "$member" <<'NODE'
const fs = require("fs");
const zlib = require("zlib");

function crc32(buf) {
  let crc = 0xffffffff;
  for (let i = 0; i < buf.length; i++) {
    crc ^= buf[i];
    for (let j = 0; j < 8; j++) {
      const mask = -(crc & 1);
      crc = (crc >>> 1) ^ (0xedb88320 & mask);
    }
  }
  return (crc ^ 0xffffffff) >>> 0;
}

const data = fs.readFileSync(process.argv[2]);
const name = Buffer.from(process.argv[4]);
const crc = crc32(data);
const comp = zlib.deflateRawSync(data);
const local = Buffer.alloc(30);
local.writeUInt32LE(0x04034b50, 0);
local.writeUInt16LE(20, 4);
local.writeUInt16LE(8, 8);
local.writeUInt32LE(crc, 14);
local.writeUInt32LE(comp.length, 18);
local.writeUInt32LE(data.length, 22);
local.writeUInt16LE(name.length, 26);
const central = Buffer.alloc(46);
central.writeUInt32LE(0x02014b50, 0);
central.writeUInt16LE(20, 4);
central.writeUInt16LE(20, 6);
central.writeUInt16LE(8, 10);
central.writeUInt32LE(crc, 16);
central.writeUInt32LE(comp.length, 20);
central.writeUInt32LE(data.length, 24);
central.writeUInt16LE(name.length, 28);
const localOffset = 0;
central.writeUInt32LE(localOffset, 42);
const eocd = Buffer.alloc(22);
eocd.writeUInt32LE(0x06054b50, 0);
eocd.writeUInt16LE(1, 8);
eocd.writeUInt16LE(1, 10);
eocd.writeUInt32LE(46 + name.length, 12);
eocd.writeUInt32LE(30 + name.length + comp.length, 16);
fs.writeFileSync(
  process.argv[3],
  Buffer.concat([local, name, comp, central, name, eocd]),
);
NODE
      elif command -v python3 >/dev/null 2>&1; then
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
