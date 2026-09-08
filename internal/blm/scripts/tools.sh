#!/bin/sh
# blm tools — install/start/stop neighbour tools on macOS / Linux (embedded in the blm binary, run via `blm tools …`)
#   tools.sh <install|start|stop> <socraticode|obsidian|graphify> [--docker]
# Rules: check before installing (idempotent) · macOS = Homebrew · Linux = curl/tar + official installers · never sudo silently
set -e
ACTION="$1"; TOOL="$2"; MODE="native"
case "$3" in --docker) MODE="docker";; esac
OS=$(uname -s); ARCH=$(uname -m)
has() { command -v "$1" >/dev/null 2>&1; }
say() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

need_brew() {
  [ "$OS" = Darwin ] || return 0
  has brew || die "Homebrew not found — install it first: /bin/bash -c \"\$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)\""
}

# ---- docker -------------------------------------------------------------------
ensure_docker() {
  if ! has docker; then
    say "docker: not found — installing"
    case "$OS" in
      Darwin) need_brew; brew install --cask docker; say "docker: Docker Desktop installed — open it once (open -a Docker) so the daemon starts";;
      Linux)  curl -fsSL https://get.docker.com | sh; say "docker: installed — you may need: sudo usermod -aG docker \$USER && newgrp docker";;
      *) die "unsupported OS $OS";;
    esac
  else
    say "docker: $(docker --version)"
  fi
  if ! docker info >/dev/null 2>&1; then
    [ "$OS" = Darwin ] && has open && open -a Docker 2>/dev/null || true
    say "docker: daemon not running — start Docker, then rerun"
    return 1
  fi
}

# ---- socraticode --------------------------------------------------------------
install_socraticode_plugin() {
  if has claude; then
    claude plugin marketplace add giancarloerra/socraticode >/dev/null 2>&1 || true
    claude plugin install socraticode@socraticode >/dev/null 2>&1 && say "socraticode: Claude plugin installed" || say "socraticode: plugin already installed or install failed — check: claude plugin list"
  else
    say "socraticode: \`claude\` not on PATH — install the plugin later: claude plugin marketplace add giancarloerra/socraticode && claude plugin install socraticode@socraticode"
  fi
}

# Qdrant ไม่มี Homebrew formula (เจ้าของเจอ 2026-09-09) → ใช้ release binary จาก GitHub ทั้ง mac และ linux
install_qdrant_binary() {
  has qdrant && { say "qdrant: already installed ($(command -v qdrant))"; return 0; }
  case "$OS:$ARCH" in
    Darwin:arm64)                T=aarch64-apple-darwin;;
    Darwin:x86_64)               T=x86_64-apple-darwin;;
    Linux:x86_64|Linux:amd64)    T=x86_64-unknown-linux-gnu;;
    Linux:aarch64|Linux:arm64)   T=aarch64-unknown-linux-gnu;;
    *) die "no qdrant build for $OS/$ARCH — use --docker";;
  esac
  say "qdrant: downloading release binary ($T)"
  mkdir -p "$HOME/.local/bin"
  URL=$(curl -fsSL https://api.github.com/repos/qdrant/qdrant/releases/latest | grep browser_download_url | grep "$T" | grep -v musl | head -1 | cut -d'"' -f4)
  [ -n "$URL" ] || die "could not resolve qdrant download URL — use --docker"
  curl -fsSL "$URL" | tar -xz -C "$HOME/.local/bin" qdrant
  chmod +x "$HOME/.local/bin/qdrant"
  say "qdrant: installed to $HOME/.local/bin/qdrant"
}

# รัน qdrant เป็น service: mac = launchd agent (ขึ้นเองตอน login) · linux = nohup (แนะนำ systemd user unit ทีหลัง) · ข้อมูลที่ ~/.qdrant/storage
wait_port() { # wait_port <url> <name> — รอจน service ตอบจริง (สูงสุด 15 วิ) ไม่ตอบ = ล้มเหลวชัด ๆ ไม่ใช่ "loaded"
  i=0; while [ $i -lt 30 ]; do curl -fs "$1" >/dev/null 2>&1 && return 0; sleep 0.5; i=$((i+1)); done
  say "$2: did not answer at $1 within 15s"; return 1
}
start_qdrant() {
  mkdir -p "$HOME/.qdrant"
  if curl -fs http://127.0.0.1:6333/collections >/dev/null 2>&1; then say "qdrant: already running on :6333"; return 0; fi
  Q=$(command -v qdrant)
  if [ "$OS" = Darwin ]; then
    P="$HOME/Library/LaunchAgents/io.qdrant.plist"; mkdir -p "$HOME/Library/LaunchAgents"
    cat > "$P" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>io.qdrant</string>
  <key>ProgramArguments</key><array><string>$Q</string></array>
  <key>WorkingDirectory</key><string>$HOME/.qdrant</string>
  <key>RunAtLoad</key><true/><key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>$HOME/.qdrant/qdrant.log</string>
  <key>StandardErrorPath</key><string>$HOME/.qdrant/qdrant.log</string>
</dict></plist>
PLIST
    # job โหลดอยู่แล้ว (แต่ process ตาย) → kickstart ไม่ bootout ซ้ำ (bootout+bootstrap ติด ๆ กันเคยทำให้ job หายทั้งตัว 2026-09-09)
    if launchctl print "gui/$(id -u)/io.qdrant" >/dev/null 2>&1; then
      launchctl kickstart -k "gui/$(id -u)/io.qdrant"
    else
      launchctl bootstrap "gui/$(id -u)" "$P" 2>/dev/null || launchctl load -w "$P"
    fi
    wait_port http://127.0.0.1:6333/collections qdrant && say "qdrant: running via launchd agent io.qdrant (starts at login, log ~/.qdrant/qdrant.log)" || { tail -5 "$HOME/.qdrant/qdrant.log"; return 1; }
  else
    (cd "$HOME/.qdrant" && nohup "$Q" >qdrant.log 2>&1 &)
    wait_port http://127.0.0.1:6333/collections qdrant && say "qdrant: running in background (log ~/.qdrant/qdrant.log) — add a systemd user unit to start at boot" || { tail -5 "$HOME/.qdrant/qdrant.log"; return 1; }
  fi
}
stop_qdrant() {
  if [ "$OS" = Darwin ] && [ -f "$HOME/Library/LaunchAgents/io.qdrant.plist" ]; then
    launchctl bootout "gui/$(id -u)/io.qdrant" >/dev/null 2>&1 || launchctl unload "$HOME/Library/LaunchAgents/io.qdrant.plist" 2>/dev/null || true
  fi
  pkill -x qdrant 2>/dev/null || true
}

install_ollama() {
  has ollama && { say "ollama: already installed"; return 0; }
  case "$OS" in
    Darwin) need_brew; say "ollama: installing (brew)"; brew install ollama;;
    Linux)  say "ollama: installing (official script)"; curl -fsSL https://ollama.com/install.sh | sh;;
  esac
}
start_ollama() {
  curl -fs http://127.0.0.1:11434/api/tags >/dev/null 2>&1 && { say "ollama: already running on :11434"; return 0; }
  case "$OS" in
    Darwin) brew services start ollama >/dev/null 2>&1 || (nohup ollama serve >/dev/null 2>&1 &);;
    *) pgrep -x ollama >/dev/null || (nohup ollama serve >/dev/null 2>&1 &);;
  esac
  wait_port http://127.0.0.1:11434/api/tags ollama && say "ollama: running on :11434"
}
stop_ollama() {
  [ "$OS" = Darwin ] && brew services stop ollama >/dev/null 2>&1 || true
  pkill -x ollama 2>/dev/null || true
}

install_socraticode_native() {
  case "$OS" in Darwin|Linux) ;; *) die "native install not supported on $OS — use --docker";; esac
  install_qdrant_binary
  install_ollama
  start_qdrant
  start_ollama
  sleep 2
  if ollama list 2>/dev/null | grep -q nomic-embed-text; then say "ollama: model nomic-embed-text present"
  else say "ollama: pulling embedding model nomic-embed-text"; ollama pull nomic-embed-text || say "ollama: pull failed — run later: ollama pull nomic-embed-text"; fi
  install_socraticode_plugin
  say "ENV QDRANT_MODE=external"
  say "ENV QDRANT_URL=http://127.0.0.1:6333"
  say "ENV OLLAMA_MODE=external"
  say "ENV OLLAMA_URL=http://127.0.0.1:11434"
  say "socraticode: native stack ready (qdrant :6333 · ollama :11434)"
}

install_socraticode_docker() {
  ensure_docker || exit 1
  say "docker: pulling images (qdrant, ollama)"
  docker pull qdrant/qdrant:v1.17.0
  docker pull ollama/ollama:latest
  install_socraticode_plugin
  say "ENV -QDRANT_MODE -QDRANT_URL -OLLAMA_MODE -OLLAMA_URL"
  say "socraticode: docker mode — SocratiCode starts and manages its own containers on first use (socraticode-qdrant :16333, socraticode-ollama :11435)"
}

start_socraticode() {
  if [ "$MODE" = docker ] || ! has qdrant; then
    ensure_docker || exit 1
    docker start socraticode-qdrant socraticode-ollama 2>/dev/null || say "socraticode: containers not created yet — they appear on first SocratiCode use"
  else
    start_qdrant; start_ollama
  fi
}
stop_socraticode() {
  if [ "$MODE" = docker ] || ! has qdrant; then
    has docker && docker stop socraticode-qdrant socraticode-ollama 2>/dev/null || true
  else
    stop_qdrant; stop_ollama
  fi
  say "socraticode: stopped"
}

# ---- obsidian -----------------------------------------------------------------
install_obsidian() {
  case "$OS" in
    Darwin) need_brew; [ -d /Applications/Obsidian.app ] && say "obsidian: already installed" || brew install --cask obsidian;;
    Linux)  has obsidian && say "obsidian: already installed" || { has flatpak || die "flatpak not found — install Obsidian from https://obsidian.md/download"; flatpak install -y flathub md.obsidian.Obsidian; };;
    *) die "unsupported OS $OS";;
  esac
}
start_obsidian() { case "$OS" in Darwin) open -a Obsidian "$PWD";; *) (obsidian "obsidian://open?path=$PWD" >/dev/null 2>&1 &) || flatpak run md.obsidian.Obsidian &;; esac; }
stop_obsidian()  { case "$OS" in Darwin) osascript -e 'quit app "Obsidian"';; *) pkill -f -i obsidian || true;; esac; }

# ---- graphify -----------------------------------------------------------------
install_graphify() {
  if ! has graphify; then
    if has pipx; then pipx install graphifyy
    elif has pip3; then pip3 install --user graphifyy
    else die "pipx/pip3 not found — install Python 3 first"; fi
  else say "graphify: already installed ($(command -v graphify))"; fi
  graphify install --platform claude && say "graphify: skill installed for Claude Code (/graphify)"
}

case "$ACTION:$TOOL" in
  install:socraticode) [ "$MODE" = docker ] && install_socraticode_docker || install_socraticode_native;;
  start:socraticode)   start_socraticode;;
  stop:socraticode)    stop_socraticode;;
  install:obsidian)    install_obsidian;;
  start:obsidian)      start_obsidian;;
  stop:obsidian)       stop_obsidian;;
  install:graphify)    install_graphify;;
  *) die "usage: tools.sh <install|start|stop> <socraticode|obsidian|graphify> [--docker]";;
esac
