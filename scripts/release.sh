#!/bin/sh
# สร้าง binary ทุกแพลตฟอร์มลง dist/ แล้ว (ถ้ามี gh) สร้าง GitHub Release
#   scripts/release.sh            → เวอร์ชันถัดไปอัตโนมัติ: tag ล่าสุดบน origin (หรือ local) +1 ที่หลัก patch (v2.0.6 → v2.0.7)
#   scripts/release.sh v2.1.0     → ระบุเอง (ต้องเป็น vX.Y.Z)
set -e
latest_tag() {
  # remote ก่อน (release สร้าง tag บน GitHub โดยไม่มี tag local) ไม่ได้ค่อยดู local
  git ls-remote --tags --refs origin 'v[0-9]*' 2>/dev/null | sed 's#.*/##' | sort -V | tail -1 \
    || true
}
VERSION="${1:-}"
if [ -z "$VERSION" ] || [ "$VERSION" = auto ]; then
  LAST=$(latest_tag)
  [ -n "$LAST" ] || LAST=$(git tag --list 'v[0-9]*' | sort -V | tail -1)
  [ -n "$LAST" ] || LAST=v0.0.0
  MAJOR=${LAST#v}; MAJOR=${MAJOR%%.*}
  MINOR=${LAST#v*.}; MINOR=${MINOR%%.*}
  PATCH=${LAST##*.}
  VERSION="v$MAJOR.$MINOR.$((PATCH + 1))"
  echo "release: last tag $LAST → $VERSION"
fi
case "$VERSION" in v[0-9]*.[0-9]*.[0-9]*) ;; *) echo "error: version must look like v2.0.6 (got '$VERSION')"; exit 1;; esac
# plugin เลขเดียวกับ binary (เจ้าของ 2026-09-20): plugin.json + marketplace.json แล้ว commit ก่อน tag — `claude plugin update blm@blm` ถึงจะเห็นว่าใหม่
PV="${VERSION#v}"
for f in plugin/.claude-plugin/plugin.json .claude-plugin/marketplace.json; do
  [ -f "$f" ] || continue
  if grep -q '"version"' "$f"; then
    sed -i.bak -E "s/\"version\": *\"[^\"]*\"/\"version\": \"$PV\"/" "$f" && rm -f "$f.bak"
  fi
done
if ! git diff --quiet -- plugin/.claude-plugin/plugin.json .claude-plugin/marketplace.json; then
  git add plugin/.claude-plugin/plugin.json .claude-plugin/marketplace.json
  git commit -q -m "chore: plugin version $PV" && echo "plugin: version → $PV (committed)"
fi
# marketplace ของ plugin อ่านจาก main บน GitHub → commit ต้องขึ้นก่อน tag
git push -q origin HEAD
git tag -a "$VERSION" -m "blm $VERSION" 2>/dev/null && git push -q origin "$VERSION" 2>/dev/null || true
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
