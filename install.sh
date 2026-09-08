#!/bin/sh
# blm one-line installer (macOS / Linux):
#   curl -fsSL https://raw.githubusercontent.com/Ekkapap/business-logic-memory/main/install.sh | sh -s -- --agentsroom
# ดาวน์โหลด binary จาก GitHub Releases → ~/.local/bin/blm → เติม PATH → blm init <args ที่ส่งมา>
set -e
REPO="Ekkapap/business-logic-memory"
OS=$(uname -s | tr '[:upper:]' '[:lower:]'); ARCH=$(uname -m)
case "$ARCH" in x86_64|amd64) ARCH=amd64;; arm64|aarch64) ARCH=arm64;; *) echo "unsupported arch $ARCH"; exit 1;; esac
BIN_DIR="${BLM_BIN_DIR:-$HOME/.local/bin}"; mkdir -p "$BIN_DIR"
URL="https://github.com/$REPO/releases/latest/download/blm_${OS}_${ARCH}.tar.gz"
echo "blm: downloading $URL"
if curl -fsSL "$URL" | tar -xz -C "$BIN_DIR" blm 2>/dev/null; then
  chmod +x "$BIN_DIR/blm"
elif command -v go >/dev/null 2>&1; then
  echo "blm: no release asset yet — building from source with go"
  GOBIN="$BIN_DIR" go install "github.com/$REPO/cmd/blm@latest"
else
  echo "blm: download failed and go not found — install Go (https://go.dev/dl) or grab a binary from https://github.com/$REPO/releases"; exit 1
fi
"$BIN_DIR/blm" path
"$BIN_DIR/blm" init "$@"
