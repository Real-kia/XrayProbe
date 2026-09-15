#!/bin/sh
set -eu

repo="Real-kia/XrayProbe"
version="${XRAYPROBE_VERSION:-latest}"
install_dir="${XRAYPROBE_INSTALL_DIR:-$HOME/.local/bin}"
os="$(uname -s)"
arch="$(uname -m)"

case "$os" in
  Linux) os_name="linux" ;;
  Darwin) os_name="darwin" ;;
  *) echo "unsupported operating system: $os" >&2; exit 1 ;;
esac
case "$arch" in
  x86_64|amd64) arch_name="amd64" ;;
  arm64|aarch64) arch_name="arm64" ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac

archive="xrayprobe_${os_name}_${arch_name}.tar.gz"
base="https://github.com/${repo}/releases/download/${version}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
user_agent="xrayprobe-installer/$version"
curl -fsSL -A "$user_agent" "$base/$archive" -o "$tmp/$archive"
curl -fsSL -A "$user_agent" "$base/checksums.txt" -o "$tmp/checksums.txt"
expected="$(awk -v name="$archive" '$2 == name {print $1}' "$tmp/checksums.txt")"
[ -n "$expected" ] || { echo "checksum entry not found" >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/$archive" | awk '{print $1}')"
else
  actual="$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')"
fi
[ "$actual" = "$expected" ] || { echo "checksum mismatch" >&2; exit 1; }
mkdir -p "$install_dir"
tar -xzf "$tmp/$archive" -C "$tmp"
install -m 0755 "$tmp/xrayprobe" "$install_dir/xrayprobe"
echo "installed xrayprobe to $install_dir/xrayprobe"
