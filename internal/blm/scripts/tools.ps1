# blm tools — Windows (embedded in the blm binary, run via `blm tools …`)
#   tools.ps1 <install|start|stop> <socraticode|obsidian|graphify> [--docker]
# Native Qdrant/Ollama on Windows is not supported by blm — SocratiCode runs in Docker there.
param([string]$Action, [string]$Tool, [string]$Mode = "")
$ErrorActionPreference = "Stop"
function Has($cmd) { return [bool](Get-Command $cmd -ErrorAction SilentlyContinue) }

function Ensure-Docker {
  if (-not (Has docker)) {
    Write-Host "docker: not found — installing Docker Desktop (winget)"
    winget install -e --id Docker.DockerDesktop --accept-source-agreements --accept-package-agreements
    Write-Host "docker: installed — start Docker Desktop once, then rerun"
    exit 1
  }
  docker info *> $null
  if ($LASTEXITCODE -ne 0) { Write-Host "docker: daemon not running — start Docker Desktop, then rerun"; exit 1 }
  Write-Host "docker: $(docker --version)"
}
function Install-Plugin {
  if (Has claude) {
    claude plugin marketplace add giancarloerra/socraticode *> $null
    claude plugin install socraticode@socraticode *> $null
    Write-Host "socraticode: Claude plugin installed (or already present)"
  } else {
    Write-Host "socraticode: 'claude' not on PATH — later: claude plugin marketplace add giancarloerra/socraticode; claude plugin install socraticode@socraticode"
  }
}

switch ("$Action`:$Tool") {
  "install:socraticode" {
    if ($Mode -ne "--docker") { Write-Host "socraticode: native Qdrant/Ollama is not supported on Windows — use: blm tools install --docker socraticode"; exit 1 }
    Ensure-Docker
    docker pull qdrant/qdrant:v1.17.0
    docker pull ollama/ollama:latest
    Install-Plugin
    Write-Host "ENV -QDRANT_MODE -QDRANT_URL -OLLAMA_MODE -OLLAMA_URL"
    Write-Host "socraticode: docker mode — SocratiCode manages its own containers on first use"
  }
  "start:socraticode" { Ensure-Docker; docker start socraticode-qdrant socraticode-ollama }
  "stop:socraticode"  { docker stop socraticode-qdrant socraticode-ollama }
  "install:obsidian"  { if (Has obsidian) { Write-Host "obsidian: already installed" } else { winget install -e --id Obsidian.Obsidian --accept-source-agreements --accept-package-agreements } }
  "start:obsidian"    { Start-Process "obsidian://open?path=$PWD" }
  "stop:obsidian"     { Stop-Process -Name Obsidian -ErrorAction SilentlyContinue }
  "install:graphify"  {
    if (Has graphify) { Write-Host "graphify: already installed" }
    elseif (Has pipx) { pipx install graphifyy }
    elseif (Has pip)  { pip install --user graphifyy }
    else { Write-Host "pipx/pip not found — install Python 3 first"; exit 1 }
    graphify install --platform claude
  }
  default { Write-Host "usage: tools.ps1 <install|start|stop> <socraticode|obsidian|graphify> [--docker]"; exit 1 }
}
