# hooks + statusline ของ Claude Code สำหรับ SocratiCode index (blm ถือให้)

ทั้งหมดเป็นกลไกปกติของ Claude Code: hook รับ JSON ทาง stdin แล้วฉีด context กลับด้วย `{"hookSpecificOutput":{"hookEventName":…,"additionalContext":…}}` · statusline รับ JSON แล้วพิมพ์ข้อความ · blm ทำให้ในคำสั่งเดียว (`blm hook …`, `blm statusline`) ไม่มี script ข้างนอก

## สิ่งที่ฉีดให้ agent

| event | คำสั่ง | ทำอะไร |
|---|---|---|
| `SessionStart` (startup/resume/clear/**compact**) | `blm hook session-start` | **core brief** ของ blm.md เสมอ (หลัง auto-compact บริบทกลับมาที่นี่ มีบรรทัดบอกว่าเพิ่ง compact) + สถานะ index + เตือนให้ถาม `blm_search` ก่อน grep |
| `UserPromptSubmit` | `blm hook prompt` | ก่อนลงมือ: core brief **เมื่อพ้น debounce** + prompt ≥ 12 ตัวอักษร → pointer 3 บรรทัด `path:Lstart-Lend (score)` ไม่แนบโค้ด (prompt เดิมซ้ำไม่ค้นซ้ำ) |
| `PostToolUse` matcher `Write\|Edit\|MultiEdit` | `blm hook post-edit` | core brief **เมื่อพ้น debounce เท่านั้น** — งานยาวที่แก้ไฟล์รัว ๆ ไม่มี prompt ใหม่ ก็ยังได้แก่นกฎกลับมาเป็นระยะ ไม่ใช่ทุกครั้ง |
| `PostToolUse` matcher `Grep\|Glob` | `blm hook grep-nudge` | เตือนว่ามี index — "where is / how does" ควรใช้ `blm_search` |

**core brief** = ตาราง Main Business (หัวข้อ + ความหมาย) + รายชื่อ rule block + กติกา 3 ข้อ (อ่านกฎก่อนแก้ · โค้ดขัดกฎ = หยุด · ทำเฉพาะที่สั่ง เจ้าของตัดสิน scope) — ดึงจาก blm.md ใน store ไม่ใช่ทั้งไฟล์ (~25 บรรทัด) · ทำไม: agent ลืมเป้าหมาย/คอนเซ็ปต์แล้วทำเกินสั่ง, brief ใหม่ทุก session เสีย 30 นาที–1 ชม., หลัง auto-compact บริบทหาย

**debounce** (สถานะต่อ session ที่ `$TMPDIR/blm-hook-<session_id>.json`): ฉีดซ้ำเมื่อพ้น `BLM_CORE_INTERVAL` นาที (ค่าเริ่มต้น 20; `0` = ทุกครั้ง) หรือ blm.md เปลี่ยน (hash) · `session-start` ฉีดเสมอ · PreToolUse ใช้ฉีดไม่ได้ (Claude Code รับแค่ allow/deny) จึงใช้ prompt = ก่อนลงมือ, post-edit = ระหว่างทาง

stack ปิด / ไม่มี index / ไม่มี blm.md / error = เงียบ exit 0 hook ไม่ทำให้ session พัง · โปรเจ็ค = `CLAUDE_PROJECT_DIR` → `cwd` ใน JSON → cwd

## statusline

`socraticode : online · 4213 nodes / 24326 edges · ✓ synced` (+ บรรทัดสอง `▸ ctx 22% · session: 878f733d` เว้นแต่ `--top-only`)

- `online` = Ollama **และ** Qdrant ตอบ (ค้นต้องใช้ทั้งคู่) · nodes/edges จาก point เดียวใน `<projectId>_symgraph_meta` ของ Qdrant
- `✓ synced` = index ทันโค้ด · `⟳ indexing` = มีไฟล์ที่ index มองเห็นถูกแก้หลังรอบล่าสุด watcher ของ socraticode กำลังตาม (ตรวจจาก mtime ของไฟล์ใน `git status` ที่ไม่ถูก `.socraticodeignore` — ให้ git ประเมิน ignore ผ่าน `core.excludesFile`) · `not indexed` = ยังไม่มี graph ของโปรเจ็คนี้ · offline แสดง `indexed dd/mm hh:mm` ล่าสุด
- ผลจาก server แคช 10 วิใน temp (`blm-socraticode-stats-<projectId>.json`) เพราะ statusline ถูกเรียกทุก tick

## ต่อสาย (blm ทำให้ตอน `blm tools install socraticode` ทั้ง --local/--remote — idempotent)

- `~/.claude/settings.json` `hooks`: ใส่ 4 entry ข้างบน · **ถอดเฉพาะของ blm เอง** (`blm hook …` รุ่นก่อน, `socraticode-hooks.cjs`) hook ของคนอื่นอยู่ครบ · เจอ graft จะพิมพ์ `note: graft hooks still installed — remove with: graft uninstall -y`
- `~/.claude/statusline-command.sh`: มีไฟล์ → แทนเฉพาะ block ใน marker `# >>> blm socraticode statusline >>>` … `# <<< … <<<` แล้วต่อท้ายใหม่ · ไม่มี → เขียนสคริปต์ค่าเริ่มต้นที่ฝังใน binary (เวลา · โฟลเดอร์ · git branch/dirty/worktree · โมเดล · ctx เทียบ auto-compact + บรรทัด socraticode)
- `statusLine`/`subagentStatusLine` ใน settings: ตั้งให้เมื่อยังไม่มีหรือชี้ `socraticode-statusline.cjs` เดิม · ชี้อย่างอื่น (graft/ของผู้ใช้) ไม่แตะ แค่ note

ทำมือ (เครื่องที่ไม่ได้ผ่าน install): `blm hook --help` พิมพ์ block ของ settings.json และบรรทัด statusline ให้ก๊อป

## ทดสอบ

```sh
echo '{"cwd":"'$PWD'","prompt":"how does cookie consent work"}' | blm hook prompt
echo '{"cwd":"'$PWD'","session_id":"x","context_window":{"used_percentage":21}}' | blm statusline
```
