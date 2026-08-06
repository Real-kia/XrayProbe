#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
sh_install="$script_dir/install.sh"
ps_install="$script_dir/install.ps1"

grep -F 'Linux) os_name="linux"' "$sh_install" >/dev/null
grep -F 'Darwin) os_name="darwin"' "$sh_install" >/dev/null
grep -F 'releases/download/${version}' "$sh_install" >/dev/null
grep -F 'xrayprobe_windows_$arch.tar.gz' "$ps_install" >/dev/null
grep -F 'releases/download/$version' "$ps_install" >/dev/null
grep -F -- "-split '\\s+'" "$ps_install" >/dev/null
grep -F 'user_agent="xrayprobe-installer/$version"' "$sh_install" >/dev/null
grep -F '$userAgent = "xrayprobe-installer/$version"' "$ps_install" >/dev/null

if grep -F 'xrayprobe_Linux_' "$sh_install" "$ps_install" >/dev/null 2>&1; then
  echo 'installer still contains a case-sensitive Linux archive name' >&2
  exit 1
fi
if grep -F 'xrayprobe_Darwin_' "$sh_install" "$ps_install" >/dev/null 2>&1; then
  echo 'installer still contains a case-sensitive Darwin archive name' >&2
  exit 1
fi

echo 'installer checks OK'
