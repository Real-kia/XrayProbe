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
$exePath = Join-Path $installDir 'xrayprobe.exe'

$previousVersion = $null
if (Test-Path -LiteralPath $exePath -PathType Leaf) {
  $previousVersion = ((& $exePath version 2>$null) -split '\s+')[1]
}

Write-Host "XrayProbe installer"
Write-Host "  requested version:  $version"
Write-Host "  platform:           windows/$arch"
Write-Host "  install directory:  $installDir"
if ($previousVersion) {
  Write-Host "  currently installed: $previousVersion"
}

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("xrayprobe-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
  $archivePath = Join-Path $tmp $archive
  $checksumsPath = Join-Path $tmp 'checksums.txt'
  $userAgent = "xrayprobe-installer/$version"
  Write-Host "downloading $archive..."
  Invoke-WebRequest "$base/$archive" -UserAgent $userAgent -OutFile $archivePath
  Invoke-WebRequest "$base/checksums.txt" -UserAgent $userAgent -OutFile $checksumsPath
  Write-Host "verifying checksum..."
  $expected = ((Get-Content $checksumsPath | Where-Object { $_ -match [regex]::Escape($archive) }) -split '\s+')[0]
  $actual = (Get-FileHash $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($actual -ne $expected.ToLowerInvariant()) { throw 'checksum mismatch' }
  tar -xzf $archivePath -C $tmp
  New-Item -ItemType Directory -Force -Path $installDir | Out-Null
  Copy-Item (Join-Path $tmp 'xrayprobe.exe') $exePath -Force
  $newVersion = ((& $exePath version 2>$null) -split '\s+')[1]
  if (-not $previousVersion) {
    Write-Host "installed xrayprobe $newVersion to $exePath"
  } elseif ($previousVersion -ne $newVersion) {
    Write-Host "updated xrayprobe $previousVersion -> $newVersion at $exePath"
  } else {
    Write-Host "xrayprobe $newVersion is already the latest version ($exePath)"
  }

  $pathDirs = $env:Path -split ';'
  if (-not ($pathDirs -contains $installDir)) {
    # This script runs in-process (irm | iex), so updating $env:Path here
    # takes effect immediately in the caller's own session, not just future ones.
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    [Environment]::SetEnvironmentVariable('Path', "$userPath;$installDir", 'User')
    $env:Path = "$env:Path;$installDir"
    Write-Host "added $installDir to your user PATH"
  }

  Write-Host "downloading Xray-core (this may take a moment)..."
  & $exePath core install latest | Out-Null
  if ($LASTEXITCODE -eq 0) {
    $coreVersion = (& $exePath core current 2>$null)
    Write-Host "Xray-core $coreVersion is ready"
  } else {
    Write-Warning "could not download Xray-core now; it will download automatically on the first 'xrayprobe test'"
  }
} finally {
  Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
