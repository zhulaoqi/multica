# Installs the Multica CLI from this deployment.
#
#   irm <deployment>/install/install.ps1 | iex
#
# The web app fills in `__MULTICA_ORIGIN__` when it serves this file, so the
# command above always downloads from the deployment the user is signed in to.
# Set $env:MULTICA_DOWNLOAD_BASE to install from another release mirror.
$ErrorActionPreference = "Stop"

$Origin = "__MULTICA_ORIGIN__"
$Base = if ($env:MULTICA_DOWNLOAD_BASE) { $env:MULTICA_DOWNLOAD_BASE.TrimEnd("/") } else { "$($Origin.TrimEnd('/'))/releases/latest/download" }

if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") {
  $Arch = "arm64"
} elseif ([Environment]::Is64BitOperatingSystem) {
  $Arch = "amd64"
} else {
  throw "Multica CLI requires a 64-bit Windows installation."
}

Write-Host "==> Looking up the latest Multica CLI release"
$Checksums = (Invoke-WebRequest -UseBasicParsing "$Base/checksums.txt").Content
$Asset = ($Checksums -split "`n" |
  ForEach-Object { ($_ -split "\s+")[1] } |
  Where-Object { $_ -match "^multica-cli-.+-windows-$Arch\.zip$" } |
  Select-Object -First 1)
if (-not $Asset) {
  throw "No Multica CLI build for windows/$Arch in $Base/checksums.txt"
}
$Expected = (($Checksums -split "`n") |
  Where-Object { $_ -match "\s+$([regex]::Escape($Asset))$" } |
  ForEach-Object { ($_ -split "\s+")[0] } |
  Select-Object -First 1)

$WorkDir = Join-Path ([System.IO.Path]::GetTempPath()) ("multica-cli-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $WorkDir | Out-Null
try {
  $Archive = Join-Path $WorkDir $Asset
  Write-Host "==> Downloading $Asset"
  Invoke-WebRequest -UseBasicParsing "$Base/$Asset" -OutFile $Archive

  $Actual = (Get-FileHash -Algorithm SHA256 -Path $Archive).Hash.ToLowerInvariant()
  if ($Expected -and $Actual -ne $Expected.ToLowerInvariant()) {
    throw "Checksum mismatch for ${Asset}: expected $Expected, got $Actual"
  }

  Expand-Archive -Path $Archive -DestinationPath $WorkDir -Force

  $BinDir = if ($env:MULTICA_BIN_DIR) { $env:MULTICA_BIN_DIR } else { Join-Path $env:LOCALAPPDATA "Multica\bin" }
  New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
  Copy-Item -Force (Join-Path $WorkDir "multica.exe") (Join-Path $BinDir "multica.exe")

  $UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
  if (($UserPath -split ";") -notcontains $BinDir) {
    [Environment]::SetEnvironmentVariable("Path", "$UserPath;$BinDir", "User")
    Write-Host "Added $BinDir to the user PATH. Open a new terminal to use it."
  }

  Write-Host "Multica CLI installed to $BinDir\multica.exe"
  Write-Host "Next: run 'multica setup' to sign in and start the runtime."
} finally {
  Remove-Item -Recurse -Force $WorkDir -ErrorAction SilentlyContinue
}
