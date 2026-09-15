$ErrorActionPreference = 'Stop'
$repo = 'Real-kia/XrayProbe'
$version = if ($env:XRAYPROBE_VERSION) { $env:XRAYPROBE_VERSION } else { 'latest' }

function Find-WritablePathDir {
  foreach ($dir in ($env:Path -split ';')) {
    if ([string]::IsNullOrWhiteSpace($dir)) { continue }
    if (-not (Test-Path -LiteralPath $dir -PathType Container)) { continue }
    $probe = Join-Path $dir ("." + [guid]::NewGuid().ToString() + ".tmp")
    try {
      [IO.File]::WriteAllText($probe, "")
      Remove-Item $probe -ErrorAction SilentlyContinue
      return $dir
    } catch { continue }
  }
  return $null
}

if ($env:XRAYPROBE_INSTALL_DIR) {
  $installDir = $env:XRAYPROBE_INSTALL_DIR
} else {
  $installDir = Find-WritablePathDir
  if (-not $installDir) { $installDir = Join-Path $HOME 'bin' }
}
$arch = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq 'Arm64') { 'arm64' } else { 'amd64' }
$archive = "xrayprobe_windows_$arch.tar.gz"
$base = if ($version -eq 'latest') { "https://github.com/$repo/releases/latest/download" } else { "https://github.com/$repo/releases/download/$version" }
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
  $exePath = Join-Path $installDir 'xrayprobe.exe'
  Write-Host "installed xrayprobe to $exePath"

  $pathDirs = $env:Path -split ';'
  if (-not ($pathDirs -contains $installDir)) {
    # This script runs in-process (irm | iex), so updating $env:Path here
    # takes effect immediately in the caller's own session, not just future ones.
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    [Environment]::SetEnvironmentVariable('Path', "$userPath;$installDir", 'User')
    $env:Path = "$env:Path;$installDir"
    Write-Host "added $installDir to your user PATH"
  }

  Write-Host "downloading Xray-core..."
  & $exePath core install latest | Out-Null
  if ($LASTEXITCODE -eq 0) {
    Write-Host "Xray-core is ready"
  } else {
    Write-Warning "could not download Xray-core now; it will download automatically on the first 'xrayprobe test'"
  }
} finally {
  Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
