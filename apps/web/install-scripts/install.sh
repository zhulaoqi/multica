#!/usr/bin/env bash
# Installs the Multica CLI from this deployment.
#
#   curl -fsSL <deployment>/install/install.sh | bash
#
# The web app fills in `__MULTICA_ORIGIN__` when it serves this file, so the
# command above always downloads from the deployment the user is signed in to.
# Set MULTICA_DOWNLOAD_BASE to install from another release mirror, and
# MULTICA_BIN_DIR to choose where the binary lands.
set -euo pipefail

ORIGIN="__MULTICA_ORIGIN__"
BASE="${MULTICA_DOWNLOAD_BASE:-${ORIGIN%/}/releases/latest/download}"

BOLD='' CYAN='' GREEN='' RED='' RESET=''
if [ -t 1 ]; then
  BOLD='\033[1m' CYAN='\033[0;36m' GREEN='\033[0;32m' RED='\033[0;31m' RESET='\033[0m'
fi
info() { printf "${BOLD}${CYAN}==> %s${RESET}\n" "$*"; }
ok() { printf "${BOLD}${GREEN}✓ %s${RESET}\n" "$*"; }
fail() { printf "${BOLD}${RED}✗ %s${RESET}\n" "$*" >&2; exit 1; }
command_exists() { command -v "$1" >/dev/null 2>&1; }

command_exists curl || fail "curl is required."
command_exists tar || fail "tar is required."

case "$(uname -s)" in
  Darwin) OS=darwin ;;
  Linux) OS=linux ;;
  *) fail "Unsupported operating system: $(uname -s). On Windows, use install.ps1." ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) ARCH=amd64 ;;
  arm64 | aarch64) ARCH=arm64 ;;
  *) fail "Unsupported architecture: $(uname -m)" ;;
esac

info "Looking up the latest Multica CLI release"
checksums="$(curl -fsSL "$BASE/checksums.txt")" || fail "Could not download $BASE/checksums.txt"

asset="$(printf '%s\n' "$checksums" | awk '{print $2}' | grep -E "^multica-cli-[^ ]+-${OS}-${ARCH}\.tar\.gz$" | head -n 1)"
[ -n "$asset" ] || fail "No Multica CLI build for ${OS}/${ARCH} in $BASE/checksums.txt"
expected="$(printf '%s\n' "$checksums" | awk -v want="$asset" '$2 == want {print $1}' | head -n 1)"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

info "Downloading $asset"
curl -fsSL "$BASE/$asset" -o "$tmp_dir/$asset" || fail "Failed to download $asset"

actual=''
if command_exists sha256sum; then
  actual="$(sha256sum "$tmp_dir/$asset" | awk '{print $1}')"
elif command_exists shasum; then
  actual="$(shasum -a 256 "$tmp_dir/$asset" | awk '{print $1}')"
fi
if [ -n "$actual" ] && [ "$actual" != "$expected" ]; then
  fail "Checksum mismatch for $asset: expected $expected, got $actual"
fi

tar -xzf "$tmp_dir/$asset" -C "$tmp_dir" multica || fail "Could not extract $asset"

bin_dir="${MULTICA_BIN_DIR:-/usr/local/bin}"
if [ -w "$bin_dir" ] || mkdir -p "$bin_dir" 2>/dev/null; then
  if [ -w "$bin_dir" ]; then
    mv "$tmp_dir/multica" "$bin_dir/multica"
  else
    sudo mv "$tmp_dir/multica" "$bin_dir/multica"
  fi
else
  bin_dir="$HOME/.local/bin"
  mkdir -p "$bin_dir"
  mv "$tmp_dir/multica" "$bin_dir/multica"
  case ":$PATH:" in
    *":$bin_dir:"*) ;;
    *) warn_path="Add $bin_dir to PATH to run multica from a new shell." ;;
  esac
fi
chmod +x "$bin_dir/multica"

ok "Multica CLI installed to $bin_dir/multica"
printf "Next: run %smultica setup%s to sign in and start the runtime.\n" "$BOLD" "$RESET"
if [ -n "${warn_path:-}" ]; then
  printf "%s\n" "$warn_path"
fi
