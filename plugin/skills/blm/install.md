ไทย · **[English](install-EN.md)**

# ติดตั้ง blm — binary + plugin · อัปเดต · ที่อยู่ไฟล์

blm มีสองส่วนที่ต้องมีคู่กัน: **binary `blm`** บน PATH (CLI + MCP server `blm mcp`) และ **plugin `blm@blm`** ใน Claude Code (skill + slash commands + `.mcp.json` ที่ชี้ `blm mcp`) — plugin ไม่มี binary จะเริ่ม MCP ไม่ได้

## 1. ติดตั้ง binary (ครั้งเดียวต่อเครื่อง) — รันใน root ของโปรเจ็คที่จะให้ blm จำ

```sh
# macOS / Linux — ลง ~/.blm/bin/blm + symlink ~/.local/bin/blm แล้ว blm init
curl -fsSL https://raw.githubusercontent.com/Ekkapap/business-logic-memory/main/install.sh | sh -s -- --agentsroom

# Windows (PowerShell) — ลง ~\.blm\bin\blm.exe + PATH (User)
$env:BLM_INIT_ARGS="--agentsroom"; irm https://raw.githubusercontent.com/Ekkapap/business-logic-memory/main/install.ps1 | iex

# มี Go
go install github.com/Ekkapap/business-logic-memory/cmd/blm@latest && blm path && blm init --agentsroom

# นักพัฒนา (checkout ของ repo): make install = build → ~/.blm/bin/blm · make install-dev = ~/.local/bin/blm → ./bin/blm ของ repo
```

- ที่อยู่มาตรฐาน `~/.blm/bin/blm` (Windows `%USERPROFILE%\.blm\bin\blm.exe`) · `~/.local/bin/blm` เป็น symlink · `BLM_HOME` เปลี่ยนที่ได้
- **backend** (ที่เก็บกลางของ memory) เลือกตอน init และเปลี่ยนทีหลังได้ด้วย init ซ้ำ:

  | flag | store | sync |
  |---|---|---|
  | `--agentsroom` **(แนะนำ)** | `.agentsroom/blm` + mirror `.agentsroom/memory` | push = `memory_save` ผ่าน MCP ของ AgentsRoom (https://agentsroom.dev) · pull = refresh mirror — local ก่อน sync ทีหลัง ตามที่ blm ออกแบบมา |
  | *(ไม่ระบุ)* | ตรวจเอง: มี `.claude/blm.json` → คงเดิม · `.agentsroom/` → agentsroom · `.obsidian/` → obsidian · ไม่งั้น `none` | ตาม backend ที่ตรวจได้ |
  | `none` | `.claude/blm` | **ไม่มี** — ไฟล์คือของจริง `blm_sync` ไม่ถูกประกาศ |
  | `--obsidian` | `blm/` (เห็นใน vault) | ไม่มี |
  | `--dir <p> --backend <cli>` | `<p>` | agent เรียนคำสั่ง push/pull จาก `<cli> --help` ตอน `/blm_init` แล้วเก็บเป็น template ใน `.claude/blm.json` |

- `blm init` ปฏิเสธนอกโฟลเดอร์โปรเจ็ค (ไม่มี `.git`/`.agentsroom`/`package.json` …) `--force` ข้ามได้ · ไม่ระบุ backend: มี `.claude/blm.json` → คงเดิม · `.agentsroom/` → agentsroom · `.obsidian/` → obsidian · ไม่งั้น none
- init เขียน `.claude/blm.json`, store, `.ignorememory`, merge `.claude/settings.json` (deny Edit/Write store, `blm guard` PreToolUse, sandbox denyWrite), เติม `.socraticodeignore`, PATH, ลงทะเบียน plugin · รันซ้ำได้ (idempotent) · `--tools socraticode,tree-sitter` ติดตั้งเครื่องมือข้างเคียงที่ยังไม่มี

## 2. ติดตั้ง plugin ใน Claude Code

`blm init` ทำให้อยู่แล้ว (ผ่าน `claude plugin marketplace add` + `claude plugin install`) — ทำเองเมื่อ init บอกว่าทำไม่ได้ หรือเครื่องที่ไม่มี `claude` ตอนนั้น:

```sh
# marketplace = repo นี้เอง (.claude-plugin/marketplace.json) · plugin ชื่อ blm
claude plugin marketplace add Ekkapap/business-logic-memory
claude plugin install blm@blm
```

หรือใน Claude Code: `/plugin` → Marketplaces → add `Ekkapap/business-logic-memory` → install `blm` · marketplace จาก checkout ในเครื่อง: `blm init --marketplace <dir>` หรือ `claude plugin marketplace add /path/to/business-logic-memory`

แล้ว `/reload-plugins` (หรือเปิด Claude Code ใหม่) → `/blm:blm_help` ต้องตอบ

## 3. ตรวจว่าใช้ได้

```sh
blm version                 # เลข tag เช่น 2.0.8 (หรือ 2.0.8-3-g… = build หลัง tag)
blm status                  # READY / NOT READY <why> บรรทัดแรก
```

ใน Claude Code: tools `blm_*` ต้องโผล่ (`blm`, `blm_create`, `blm_search`, `blm_tools`, `blm_selfupdate` …) · ไม่โผล่ = `/mcp reconnect plugin:blm:blm`

## 4. อัปเดต

```sh
blm self-update             # binary (global: release ล่าสุดจาก GitHub · dev checkout: git pull + build) + plugin (claude plugin update blm@blm)
blm self-update --check     # ดูอย่างเดียว
```

MCP: `blm_selfupdate {check?, binary?, plugin?}` · หลังอัปเดต `/mcp reconnect plugin:blm:blm` (MCP ที่รันอยู่ยังเป็น binary เก่าจนกว่าจะ reconnect) · เลข plugin ตรงกับ binary ตั้งแต่ release ที่ `make release` bump `plugin.json` ให้

## 5. ถอน

```sh
claude plugin uninstall blm@blm
rm -rf ~/.blm ~/.local/bin/blm            # Windows: %USERPROFILE%\.blm
```

ในโปรเจ็ค: `.claude/blm.json`, store (`.agentsroom/blm/` หรือ `.claude/blm/`), บรรทัด `blm guard`/deny ใน `.claude/settings.json` — ลบมือ (init เขียนแบบ merge จึงถอนแบบ merge ไม่มี)
