$ErrorActionPreference = 'Stop'
$repo = 'KiaTheRandomGuy/XrayProbe'
$version = if ($env:XRAYPROBE_VERSION) { $env:XRAYPROBE_VERSION } else { 'latest' }
$installDir = if ($env:XRAYPROBE_INSTALL_DIR) { $env:XRAYPROBE_INSTALL_DIR } else { Join-Path $HOME 'bin' }
$arch = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq 'Arm64') { 'arm64' } else { 'amd64' }
$archive = "xrayprobe_windows_$arch.tar.gz"
$base = "https://github.com/$repo/releases/download/$version"
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("xrayprobe-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
  $archivePath = Join-Path $tmp $archive
  $checksumsPath = Join-Path $tmp 'checksums.txt'
  $userAgent = "xrayprobe-installer/$version"
  Invoke-WebRequest "$base/$archive" -UserAgent $userAgent -OutFile $archivePath
  Invoke-WebRequest "$base/checksums.txt" -UserAgent $userAgent -OutFile $checksumsPath
  $expected = ((Get-Content $checksumsPath | Where-Object { $_ -match [regex]::Escape($archive) }) -split '\s+')[0]
  $actual = (Get-FileHash $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($actual -ne $expected.ToLowerInvariant()) { throw 'checksum mismatch' }
  tar -xzf $archivePath -C $tmp
  New-Item -ItemType Directory -Force -Path $installDir | Out-Null
  Copy-Item (Join-Path $tmp 'xrayprobe.exe') (Join-Path $installDir 'xrayprobe.exe') -Force
  Write-Host "installed xrayprobe to $installDir\xrayprobe.exe"
} finally {
  Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
