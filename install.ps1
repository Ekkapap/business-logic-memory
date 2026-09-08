# blm one-line installer (Windows PowerShell):
#   irm https://raw.githubusercontent.com/Ekkapap/business-logic-memory/main/install.ps1 | iex
#   ($env:BLM_INIT_ARGS = "--agentsroom" ก่อนรัน เพื่อส่ง option ให้ blm init)
$ErrorActionPreference = "Stop"
$repo = "Ekkapap/business-logic-memory"
Write-Host "blm: run this inside the project you want blm to remember (current: $PWD)"
$arch = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else { "386" }
$binDir = if ($env:BLM_BIN_DIR) { $env:BLM_BIN_DIR } else { Join-Path $env:LOCALAPPDATA "blm" }
New-Item -ItemType Directory -Force -Path $binDir | Out-Null
$zip = Join-Path $env:TEMP "blm.zip"
$url = "https://github.com/$repo/releases/latest/download/blm_windows_$arch.zip"
Write-Host "blm: downloading $url"
try {
  Invoke-WebRequest -Uri $url -OutFile $zip
  Expand-Archive -Path $zip -DestinationPath $binDir -Force
} catch {
  if (Get-Command go -ErrorAction SilentlyContinue) {
    Write-Host "blm: no release asset yet — building from source with go"
    $env:GOBIN = $binDir; go install "github.com/$repo/cmd/blm@latest"
  } else { throw "download failed and go not found — install Go (https://go.dev/dl) or grab a binary from https://github.com/$repo/releases" }
}
$blm = Join-Path $binDir "blm.exe"
& $blm path
$initArgs = if ($env:BLM_INIT_ARGS) { $env:BLM_INIT_ARGS -split " " } else { @() }
& $blm init @initArgs
