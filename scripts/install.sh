#!/bin/sh
set -eu

repo="Real-kia/XrayProbe"
version="${XRAYPROBE_VERSION:-latest}"
os="$(uname -s)"
arch="$(uname -m)"

# A script run via `curl | sh` executes in a child process and cannot change
# the PATH of the shell that invoked it, so `xrayprobe` only works right away
# if we install into a directory the current PATH already resolves. Scan it
# in its own priority order and use the first existing, writable entry.
find_path_dir() {
  old_ifs=$IFS
  IFS=':'
  for dir in $PATH; do
    IFS=$old_ifs
    if [ -n "$dir" ] && [ -d "$dir" ] && [ -w "$dir" ]; then
      printf '%s' "$dir"
      return 0
    fi
    IFS=':'
  done
  IFS=$old_ifs
  return 1
}

install_dir="${XRAYPROBE_INSTALL_DIR:-}"
if [ -z "$install_dir" ]; then
  install_dir="$(find_path_dir || true)"
fi
if [ -z "$install_dir" ]; then
  install_dir="$HOME/.local/bin"
fi

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
if [ "$version" = "latest" ]; then
  base="https://github.com/${repo}/releases/latest/download"
else
  base="https://github.com/${repo}/releases/download/${version}"
fi

previous_version=""
if [ -x "$install_dir/xrayprobe" ]; then
  previous_version="$("$install_dir/xrayprobe" version 2>/dev/null | awk '{print $2}')"
fi

echo "XrayProbe installer"
echo "  requested version: $version"
echo "  platform:           $os_name/$arch_name"
echo "  install directory:  $install_dir"
if [ -n "$previous_version" ]; then
  echo "  currently installed: $previous_version"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
user_agent="xrayprobe-installer/$version"
echo "downloading $archive..."
curl -fsSL -A "$user_agent" "$base/$archive" -o "$tmp/$archive"
curl -fsSL -A "$user_agent" "$base/checksums.txt" -o "$tmp/checksums.txt"
echo "verifying checksum..."
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
new_version="$("$install_dir/xrayprobe" version 2>/dev/null | awk '{print $2}')"
if [ -z "$previous_version" ]; then
  echo "installed xrayprobe $new_version to $install_dir/xrayprobe"
elif [ "$previous_version" != "$new_version" ]; then
  echo "updated xrayprobe $previous_version -> $new_version at $install_dir/xrayprobe"
else
  echo "xrayprobe $new_version is already the latest version ($install_dir/xrayprobe)"
fi

case ":$PATH:" in
  *":$install_dir:"*) ;;
  *)
    export_line="export PATH=\"$install_dir:\$PATH\""
    added_to=""
    for profile in "$HOME/.bashrc" "$HOME/.zshrc" "$HOME/.profile"; do
      [ -f "$profile" ] || continue
      if ! grep -qF "$export_line" "$profile" 2>/dev/null; then
        printf '\n# added by the XrayProbe installer\n%s\n' "$export_line" >>"$profile"
      fi
      added_to="$added_to $profile"
    done
    echo "" >&2
    echo "warning: $install_dir is not on your PATH yet, so 'xrayprobe' will not be" >&2
    echo "found in this shell session. For right now, run it by its full path:" >&2
    echo "  $install_dir/xrayprobe" >&2
    if [ -n "$added_to" ]; then
      echo "It has been added to:$added_to for future shells; open a new shell (or run 'exec \$SHELL') to pick it up." >&2
    else
      echo "Add this to your shell profile and open a new shell:" >&2
      echo "  $export_line" >&2
    fi
    echo "" >&2
    ;;
esac

echo "downloading Xray-core (this may take a moment)..."
if "$install_dir/xrayprobe" core install latest >/dev/null 2>&1; then
  core_version="$("$install_dir/xrayprobe" core current 2>/dev/null)"
  echo "Xray-core $core_version is ready"
else
  echo "warning: could not download Xray-core now; it will download automatically on the first 'xrayprobe test'" >&2
fi
