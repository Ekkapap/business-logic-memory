#!/bin/sh
# สร้าง binary ทุกแพลตฟอร์มลง dist/ แล้ว (ถ้ามี gh) สร้าง GitHub Release:  scripts/release.sh v2.0.0
set -e
VERSION="${1:?usage: scripts/release.sh vX.Y.Z}"
case "$VERSION" in v[0-9]*.[0-9]*.[0-9]*) ;; *) echo "error: version must look like v2.0.6 (got '$VERSION')"; exit 1;; esac
LD="-s -w -X github.com/Ekkapap/business-logic-memory/internal/blm.Version=${VERSION#v}"
rm -rf dist && mkdir -p dist
for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64; do
  os=${t%/*}; arch=${t#*/}; out="dist/blm_${os}_${arch}"; mkdir -p "$out"
  if [ "$os" = windows ]; then
    GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags "$LD" -o "$out/blm.exe" ./cmd/blm
    (cd "$out" && zip -q "../blm_${os}_${arch}.zip" blm.exe)
  else
    GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags "$LD" -o "$out/blm" ./cmd/blm
    tar -czf "dist/blm_${os}_${arch}.tar.gz" -C "$out" blm
  fi
  rm -rf "$out"; echo "built $t"
done
if command -v gh >/dev/null 2>&1; then
  gh release create "$VERSION" dist/* --title "blm $VERSION" --generate-notes
else
  echo "gh ไม่พบ — อัปโหลด dist/* ที่ https://github.com/Ekkapap/business-logic-memory/releases/new (tag $VERSION)"
fi
