ไทย · **[English](contributing-EN.md)**

# พัฒนา blm ต่อ — fork · build · test · ส่งกลับ (pull request)

repo: https://github.com/Ekkapap/business-logic-memory (public, Go ล้วน ไม่มี dependency นอก stdlib)

## 1. เตรียมเครื่อง

- Go ≥ 1.25 (`go.mod`) · git · `make` · Claude Code CLI (`claude`) ถ้าจะทดสอบ plugin · `gh` ถ้าจะออก release
- ไม่ต้องมี Ollama/Qdrant สำหรับ build/test (test จำลองด้วย `httptest`) — ต้องมีเฉพาะตอนลอง `blm search` กับ index จริง

## 2. fork + clone

```sh
gh repo fork Ekkapap/business-logic-memory --clone      # หรือกด Fork บน GitHub แล้ว
git clone git@github.com:<you>/business-logic-memory.git
cd business-logic-memory
git remote add upstream https://github.com/Ekkapap/business-logic-memory.git
```

## 3. build ให้ใช้กับเครื่องตัวเองระหว่าง dev

```sh
make install-dev      # build ./bin/blm แล้ว ~/.local/bin/blm → ./bin/blm ของ checkout นี้ (blm self-update จะ git pull + build ที่นี่)
make install          # build → ~/.blm/bin/blm เหมือนผู้ใช้ทั่วไป (ใช้ทดสอบตัวติดตั้ง/self-update แบบ release)
```

หลัง build ทุกครั้งใน Claude Code: `/mcp reconnect plugin:blm:blm` (MCP ที่รันอยู่ยังเป็น binary เก่า) · `make install-dev` copy คำสั่งนี้ลง clipboard บน mac ให้

ทดสอบ plugin จาก checkout โดยไม่ต้อง release: `claude plugin marketplace add /path/to/business-logic-memory` แล้ว `claude plugin install blm@blm` (marketplace = `.claude-plugin/marketplace.json` ของ repo)

## 4. โครงโค้ด

- `cmd/blm/main.go` — CLI: usage, parse flag (`splitFlags`), dispatch ไป `mcp.Server.Call` (CLI กับ MCP ใช้โค้ดเดียวกัน) + help ต่อคำสั่ง (`searchHelp`, `hookHelp`, `updateHelp` …)
- `internal/mcp/server.go` — ประกาศ tool (`obj/str/enum` schema) + `Call` switch
- `internal/blm/` — logic: store/checkout/history (`store.go`), rules + linkpath, conflict/propose/resolve, diff/merge, sync กับ AgentsRoom (`agentsroom.go`), graph (socraticode/ast-grep/regex), grep/cat, **search + trust** (`search.go`, `trust.go`), hooks/statusline (`hook.go`), tools install/wire (`tools.go`, `wire.go`, `scripts/tools.sh|ps1|statusline-command.sh` ฝังใน binary), self-update (`selfupdate.go`), layout/สี (`layout.go`), config (`config.go`)
- `internal/cli/` — `init`, `guard` (PreToolUse hook), PATH
- `plugin/` — Claude Code plugin: `.claude-plugin/plugin.json`, `skills/blm/` (SKILL.md + ไฟล์ย่อยที่คุณกำลังอ่าน), `commands/*.md` (slash)
- test อยู่ข้างไฟล์ (`*_test.go`) — network จำลองด้วย `httptest`, HOME/TMPDIR/BLM_CACHE_DIR ชี้ temp เสมอ ห้ามแตะ `~/.claude` จริง

## 5. กติกาเวลาแก้

- **ก่อนเพิ่ม tool / flag / ไฟล์ใหม่ บอกเจ้าของหนึ่งบรรทัด** ว่าจะเพิ่มอะไรและของเดิมขยายแทนได้ไหม (บทเรียน 2026-09-09: `blm_propose` ซ้ำกับ `blm_conflict` ที่รับ `content` ได้)
- คอมเมนต์ในโค้ดอธิบาย **ทำไม** (ภาษาไทยได้) และวันที่/ผู้ตัดสินเมื่อเป็นกติกาจากเจ้าของ · ไม่มี `any`-style shortcut, ไม่กลืน error เงียบ ยกเว้น hook/statusline ที่ต้องเงียบโดยตั้งใจ (คอมเมนต์บอก)
- CLI และ MCP ต้องได้ผลเดียวกัน (CLI เรียก `srv.Call` แล้ว render `terminal`) · เพิ่ม tool = แก้ทั้ง `server.go` (schema+Call), `main.go` (usage+case+help), `plugin/commands/*.md`, README แถวตาราง, ไฟล์ skill ที่เกี่ยว
- `gofmt` ทุกไฟล์ที่แตะ · `make test` (= `go vet ./... && go test ./...`) ต้องผ่าน
- ใน sandbox ของ agent: `GOCACHE=$TMPDIR/gocache GOMODCACHE=$TMPDIR/gomod go test ./...` (cache ของจริงเขียนไม่ได้) · `blm guard` จับข้อความที่มี path ของ store ในคำสั่ง Bash — เขียนสคริปต์ patch ลงไฟล์แล้วรัน แทน heredoc

## 6. commit · push · pull request

```sh
git checkout -b feat/<เรื่อง>
# … แก้ · gofmt · make test
git add <ไฟล์ที่แก้>            # ไม่ add .agentsroom/ .claude/ bin/
git commit -m "feat: <สิ่งที่ทำ> — <ทำไม/ผล>"
git push -u origin feat/<เรื่อง>
gh pr create --base main --repo Ekkapap/business-logic-memory --title "feat: …" --body "…"   # หรือกด Compare & pull request บน GitHub
```

- รูปแบบ commit: `feat:` / `fix:` / `chore:` / `docs:` subject บรรทัดเดียว บอกทั้ง "ทำอะไร" และ "ทำไม" · body เป็น bullet ต่อเรื่องได้
- PR body: ปัญหา → สิ่งที่เปลี่ยน → ทดสอบอย่างไร (คำสั่งที่รัน + ผลจริง) · ถ้าเปลี่ยน CLI/MCP ให้แปะ output ตัวอย่าง
- sync กับ upstream ก่อนส่ง: `git fetch upstream && git rebase upstream/main`
- อย่า push ขึ้น `main` ของ upstream ตรง (เฉพาะเจ้าของ) · release (`make release`) เจ้าของทำ: bump `plugin.json` → commit+push → tag `vX.Y.Z` → build 5 แพลตฟอร์ม → GitHub Release

## 7. release (เจ้าของ)

```sh
make release                       # patch ถัดไปจาก tag ล่าสุดบน origin
make release RELEASE_VERSION=v2.1.0
```

`scripts/release.sh`: เขียน `version` ใน `plugin/.claude-plugin/plugin.json` = tag → commit `chore: plugin version` → `git push origin HEAD` → tag + push → build `dist/blm_<os>_<arch>.tar.gz|zip` (darwin/linux/windows) → `gh release create` · ผู้ใช้ได้ทั้ง binary และ plugin เลขเดียวกันผ่าน `blm self-update`
