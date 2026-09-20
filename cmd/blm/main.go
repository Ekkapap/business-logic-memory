// blm — business-logic-memory: binary เดียวเป็นทั้ง CLI ที่ผู้ใช้เรียกตรง (blm status …), MCP server (blm mcp)
// และ hook guard (blm guard) ข้ามแพลตฟอร์ม เจ้าของกำหนด 2026-09-09: ไม่ต้องมี bun/node/jq บนเครื่องผู้ใช้
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Ekkapap/business-logic-memory/internal/blm"
	"github.com/Ekkapap/business-logic-memory/internal/cli"
	"github.com/Ekkapap/business-logic-memory/internal/mcp"
)

const usage = `blm <command> [args]

  init [path] [--agentsroom | --obsidian | --dir <p> --backend <cli>] [--tools a,b] [--docker] [--no-install] [--no-plugin] [--sandbox] [--marketplace <dir|owner/repo>]
                                  run inside the project root · --tools installs the listed tools when missing (--docker = socraticode via Docker) · --force skips the project-root check
  status [--json]                 readiness, paths, rules, temp notes, tools, stats
  scan [path] [--json]            survey the repo: sizes, token estimate, sub-projects (candidate main topics)
  graph [query] [--rebuild] [--html]   built-in code graph: hubs, folder clusters, who-imports/calls-whom; --html writes <store>/graph.html
  search "<question>" [--limit N=10] [--lang typescript] [--file <path>] [--exclude md] [--full | --brief] [--json]
  search --get <search-id> [--id 1,3] [--context N]
                                  semantic search over the SocratiCode index (Thai or English) without its MCP: embeds with the index's model, hybrid dense+BM25
                                  RRF like codebase_search · answer = one-line table of contents per hit (#id score path:lines preview) + search-id, then
                                  --get opens the chunks you pick (real file lines) · .md under the blm store and .agentsroom/ always dropped, --exclude md drops every .md
  grep "<term1> <term2> ..." [--path <dir> | --file <file>] [--max-line N=20] [--max-result N=10] [--ext .md,.ts,...] [--sc] [--json]
                                  search terms in files: case-insensitive substring match per term · --sc asks blm search first for candidate files · --json for machine output
  cat --grep-id <id> [--result-id <n,m>] [--context N=3] [--json]
                                  read grep result file and display hits with context
  report [name] [--json]          latest report
  tools <action> [tool] [--docker | --local | --remote <host> [--embedding-model m --embedding-dimensions n --embedding-context-length n]]
                                  status|get|install|update|start|stop|restart|gen-graph|help · tools: socraticode|obsidian|graphify|tree-sitter|embedding
                                  socraticode --local = Qdrant+Ollama on this machine (as before) · --remote <host> = use a server that already runs them,
                                  saved to .claude/blm.json + ~/.claude/settings.json env · no flag = local, or stop-and-ask when a remote is saved · "blm tools help" for examples
  sync --push|--pull [--apply] [--parallel] [--author a] [--role r] [--delete a,b]   plan, or with --apply push to AgentsRoom one by one (--parallel = all at once)
  diff <name> · merge <name> mine|cloud|content [file]   see what changed on the cloud vs your draft, then resolve
  conflicts [<id>] [-i] [--all]   waiting conflicts, one per block with its report path · <id> prints the report · -i pick → read → decide (c/i) · --all includes done
  conflict mark                   put [Conflict] marks into blm.md and the notes for every waiting report (reports alone change nothing)
  restore <history-file> <note>   put a history/ snapshot back as the note (blm restore <note> lists snapshots)
  resolve <name> [--keep current|incoming] [--no-push]  apply the decision, then push the note so the backend equals local (--no-push to skip)
  get <name> · create <name> <file|-> [--target t] [--mode m] [--folder f] [--description d] · append <name> <file|-> · patch <name> <find> <replace> · replace <name> <file|-> [--confirm] · delete <name>
  path                            add the blm folder to the user's PATH (prints the command if it cannot)
  update ["<topic>"] [--limit N=10] [--html [--from <json>]] [--json] · update draft ["<topic>"]   what blm.md covers and what it does not: topics + dirs their code: lines cover · graph clusters no rule refers to ·
                                  files changed since the newest rule · with a topic: blm search hits to start reading — the report /blm_update starts from (no LLM)
  self-update [--check] [--binary | --plugin] [--json]   update blm itself (also: blm --self-update): binary (dev checkout → git pull + go build · global → latest GitHub release) + plugin (claude plugin update blm@blm)
  guard                           PreToolUse hook: reads JSON on stdin, denies Bash writes inside the store
  hook session-start|prompt|post-edit|grep-nudge   one Claude Code hook for everything (JSON on stdin → additionalContext): core business logic of blm.md
                                  at session start (incl. after compaction) and, debounced (BLM_CORE_INTERVAL min, default 20), before a prompt / after edits;
                                  top-3 index pointers per prompt; a nudge after Grep/Glob · wired by "blm tools install socraticode" or see "blm hook --help"
  statusline [--top-only]         Claude Code statusline: socraticode : online · N nodes / M edges · ✓ synced|⟳ indexing (+ ctx% / session line unless --top-only)
  mcp                             MCP server (stdio) — the Claude Code plugin runs this
  version`

const updateReportHelp = `blm update ["<topic>"] — หัวข้อหลักใหม่ควรเป็นอะไร (หลัง /blm_init ผ่านไปสักพัก) · MCP: blm_update · slash: /blm:blm_update
ไม่ scan ทั้งโปรเจ็ค ไม่ใช้ LLM — รายงานเดียวกันทั้ง CLI และ MCP

  blm update                   หัวข้อที่มี (โน้ต, block, updated_at เก่าสุด, โฟลเดอร์ที่ code: อ้าง) · cluster ในกราฟที่ไม่มีกฎอ้าง = ผู้สมัคร ·
                               ไฟล์ที่เปลี่ยนหลังกฎล่าสุด (git log, เฉพาะไฟล์ที่ index) แยก "no rule" / "covered — rule may be stale"
  blm update "line webhook"    + ผลค้น blm search ของหัวข้อนั้น (สารบัญ + search-id → blm search --get <id> --id n)
  --limit N                    จำนวนผู้สมัคร/ผลค้น (10) · --json ผลเป็น JSON

  blm update [topic] --html    รายงานเดียวกันเป็นหน้า review: <store>/reviews/review-<id>.json + .html ที่ http://127.0.0.1:<port>/r/<id>
                               ติ๊ก ref → New topic (หลายหัวข้อ) → ปุ่ม "blm topic create" = submit · ทุกคลิก (ติ๊ก, comment+บันทึก, agree, draft)
                               ลงไฟล์ทันที แต่สถานะหลักเปลี่ยนและ agent ถูกเรียกเมื่อ submit รวมเท่านั้น · CLI รอจน submit แล้วจบ (--no-wait ไม่รอ)
  blm update --html --from <json>   เปิดรีวิวเดิม (gen HTML ใหม่จากไฟล์ + server) — ใช้ต่อรอบ 2+ หลัง agent ใส่ข้อเสนอ หรือเมื่อ server ปิดไป
  blm update draft ["topic"]   จุดที่ยัง DRAFT ในทุกรีวิว (ชื่อ/ความหมายของ main/sub topic) เพื่อกลับมาทำต่อ
  blm update --wait [--from <json|id>] [--timeout 240]   รอจนเจ้าของทำอะไรในหน้ารีวิว (แชท/comment/agree/แถวใหม่/submit) หรือกด "จบ live" แล้วพิมพ์ todo
                               agent รันเป็น background ของ Claude Code → ถูกเรียกกลับเมื่อจบ ไม่ต้องวน blm_update wait ใน MCP

ความหมายและหัวข้อย่อยเป็นของคน: เลือกผู้สมัครแล้วสั่ง /blm_update "<ชื่อ>" ใน Claude Code ให้ agent ร่างและรอคุณยืนยัน
(อัปเดตตัว blm เอง = blm self-update)`

const updateHelp = `blm self-update — อัปเดต blm เอง (binary + plugin) · MCP: blm_selfupdate · slash: /blm:blm_selfupdate
(รับ blm --self-update ด้วย · blm update = รายงานความครอบคลุมของ blm.md ไม่ใช่คำสั่งนี้)

  blm self-update              binary + plugin
  blm self-update --check      ดูว่ามีเวอร์ชันใหม่ไหม ไม่เปลี่ยนอะไร
  blm self-update --binary     เฉพาะ binary · --plugin เฉพาะ plugin · --json ผลเป็น JSON

binary:  dev install (ไฟล์ที่รันอยู่ใน checkout ของ repo) → git pull --ff-only + go build ลงที่เดิม
         global install (install.sh / release) → ดาวน์โหลด blm_<os>_<arch> จาก GitHub Releases ล่าสุดแล้วแทนไฟล์
plugin:  claude plugin marketplace update blm → claude plugin update blm@blm
หลังอัปเดต: 1) /reload-plugins (session ยังถือ plugin เก่า: commands/skills/เวอร์ชัน) 2) /mcp reconnect plugin:blm:blm (MCP ที่รันอยู่ยังใช้ binary เก่า) — ข้อ 1 ถูก copy ลง clipboard ให้บน mac`

const searchHelp = `blm search — ค้นหาความหมายผ่าน index ของ SocratiCode (ไม่ต้องผ่าน MCP ของมัน)

รูปแบบ:
  blm search "<คำถาม ไทยหรืออังกฤษ>" [--limit N=10] [--lang <label>] [--file <path>] [--exclude md] [--full | --brief] [--min-score 0.10] [--json]
  blm search --get <search-id> [--id 1,3] [--context N=0]      เปิด chunk ที่เลือกจากสารบัญ (บรรทัดจริงจากไฟล์ ± context)

ผลลัพธ์ = สารบัญ 1 บรรทัดต่อผล: #id · score · path:Lstart-Lend · [lang] · preview  แล้วปิดท้าย search-id
  → อยากอ่านตัวไหน: blm search --get <search-id> --id 2     (MCP: blm_search {get:"<search-id>", ids:[2]})
  → --full = แนบเนื้อทุก chunk ทีเดียว (แบบ codebase_search เดิม) · --brief = pointer ล้วน ไม่บันทึกไฟล์ผล (hook ใช้)

ทำอะไร:
  1. embed คำถามด้วยโมเดล+prefix เดียวกับที่ index ใช้ (อ่านจาก .claude/blm.json section socraticode → env → ~/.claude/settings.json)
  2. Qdrant hybrid: dense (ความหมาย) + BM25 (คำตรง) รวมด้วย RRF — คะแนน 1.0 = อันดับ 1 ทั้งสองฝั่ง, 0.5 = อันดับ 1 ฝั่งเดียว (สเกลเดียวกับ codebase_search)
  3. กรอง: .md ใต้ store ของ blm และใต้ .agentsroom/ (mirror memory) ตัดออกเสมอ · --exclude md ตัด .md ทั้งโปรเจ็ค

ตัวเลือก:
  --limit N         จำนวนผลสูงสุด (ค่าเริ่มต้น 10)
  --lang <label>    เฉพาะภาษา ตาม label ของ socraticode: typescript (= .ts+.tsx) · go · php · python · markdown · sql · json …
  --file <path>     เฉพาะไฟล์นี้ (relative path ตรงตัว)
  --exclude md      ตัด .md ทุกที่ (ค่าเริ่มต้นตัดเฉพาะของ blm/.agentsroom)
  --full            แนบเนื้อ chunk ทุกผลใน response · --brief pointer ล้วน ไม่บันทึกไฟล์ผล
  --no-trust        ข้ามคอลัมน์ origin (ประหยัด 1 embed request)

คอลัมน์ origin — ความน่าเชื่อถือของแต่ละผล (blm วิเคราะห์เอง socraticode ไม่มี): แยก chunk เป็นโค้ด/คอมเมนต์ แล้ว embed ทั้งสองส่วน
  code        คำถามชนโค้ดโดยตรง → เชื่อได้
  both ✓      ชนทั้งโค้ดและคอมเมนต์ และสองส่วนไปทางเดียวกัน (cosine ≥ 0.60) → เชื่อได้เต็มที่
  both        ชนทั้งคู่ แต่คอมเมนต์กับโค้ดไม่ค่อยตรงกัน
  comment ~   ชนแต่คอมเมนต์ แต่คอมเมนต์เล่าเรื่องเดียวกับโค้ด
  comment ⚠   ชนแต่คอมเมนต์ และโค้ดพูดอีกเรื่อง → คอมเมนต์อาจเก่า อ่านโค้ดก่อนเชื่อ
  ตัวเลขท้ายป้าย = cosine(code, comment) ของก้อนนั้น
  --get <id> --id n,m --context N   เปิด chunk จากผลก่อนหน้า (ไฟล์ <store>/tmp/search-result-<id>.json)
  --min-score X     ตัดผลต่ำกว่า X (ค่าเริ่มต้น 0.10 เท่า SEARCH_MIN_SCORE ของ socraticode)
  --json            ผลลัพธ์ JSON

ตัวอย่าง:
  blm search "เข้าสู่ระบบด้วย LINE ต้องกรอก OTP อีกไหม" --lang typescript
  blm search "cookie consent policy version" --exclude md --limit 5 --brief

ต้องมี: stack ของ SocratiCode ออนไลน์ (blm tools status socraticode) และโปรเจ็คถูก index แล้ว (codebase_index ผ่าน plugin socraticode)`

const hookHelp = `blm hook <event> · blm statusline — hook กลางตัวเดียวของ Claude Code: ฉีดแก่น business logic (blm.md) + สถานะ/pointer จาก SocratiCode index

  blm hook session-start    core brief ของ blm.md (ทุก source: startup/resume/clear/compact — หลัง auto-compact บริบทกลับมาที่นี่) + สถานะ index
  blm hook prompt           ก่อนลงมือ: core brief เมื่อพ้น debounce + prompt ≥ 12 ตัวอักษร → pointer 3 บรรทัด (path:Lstart-Lend score) ไม่แนบโค้ด
  blm hook post-edit        หลัง Write/Edit: core brief เมื่อพ้น debounce เท่านั้น (agent แก้ไฟล์รัว ๆ ไม่โดนฉีดทุกครั้ง)
  blm hook grep-nudge       หลัง Grep/Glob → เตือนว่ามี index

core brief = ตาราง Main Business (หัวข้อ+ความหมาย) + รายชื่อ rule block + กติกา 3 ข้อ จาก blm.md ใน store — ไม่ใช่ทั้งไฟล์
debounce: ฉีดซ้ำเมื่อพ้น BLM_CORE_INTERVAL นาที (ค่าเริ่มต้น 20) หรือ blm.md เปลี่ยน · session-start ฉีดเสมอ · สถานะต่อ session ที่ $TMPDIR/blm-hook-<session_id>.json
  blm statusline [--top-only]   socraticode : online · N nodes / M edges · ✓ synced|⟳ indexing  (+ ▸ ctx N% · session: xxxxxxxx)
                            --top-only = บรรทัดบนอย่างเดียว ใช้เมื่อ include จาก statusline หลักที่แสดง ctx อยู่แล้ว

ทุก event รับ JSON ทาง stdin ตามที่ Claude Code ส่ง (session_id, cwd, prompt, tool_name, tool_input, context_window) · โปรเจ็ค = CLAUDE_PROJECT_DIR หรือ cwd
stack ปิด / ไม่มี index / error = เงียบ (exit 0) hook ไม่ทำให้ session พัง · ผล status แคช 10 วิใน temp

ต่อสายใน ~/.claude/settings.json (ระดับเครื่อง ใช้ทุกโปรเจ็ค):
  "hooks": {
    "SessionStart":     [ { "hooks": [ { "type": "command", "command": "blm hook session-start", "timeout": 8000 } ] } ],
    "UserPromptSubmit": [ { "hooks": [ { "type": "command", "command": "blm hook prompt", "timeout": 15000 } ] } ],
    "PostToolUse":      [ { "matcher": "Grep|Glob", "hooks": [ { "type": "command", "command": "blm hook grep-nudge", "timeout": 8000 } ] },
                          { "matcher": "Write|Edit|MultiEdit", "hooks": [ { "type": "command", "command": "blm hook post-edit", "timeout": 8000 } ] } ]
  }
statusline: ต่อท้าย ~/.claude/statusline-command.sh ด้วย
  sc=$(echo "$input" | blm statusline --top-only 2>/dev/null); [ -n "$sc" ] && printf '\n%s' "$sc"
หรือชี้ "statusLine": { "type": "command", "command": "blm statusline" } ตรง ๆ`

const reviewHelp = `blm review — ไฟล์ใน store ของ blm (.agentsroom/blm/**: review json/html ฯลฯ) ผ่านโปรเซส blm — ทางเดียวเมื่อ sandbox ของ Bash ห้ามเขียนที่นั่น

  blm review get <id> [--select <path>]      พิมพ์ json ของ review (id 8 ตัว เช่น 980fe083) · --select = เฉพาะจุด: topics[id=nws6d].subs · items[id=c2].chat · topics.status
  blm review list [<folder>]                 รายชื่อไฟล์ในโฟลเดอร์ของ store (ไม่ใส่ = ตัว store)
  blm review read <path>                     พิมพ์เนื้อหาไฟล์ใด ๆ ใน store
  blm review write <path|id> [--file <src>]  เขียนทั้งไฟล์ (เนื้อหาจาก --file หรือ stdin)
  blm review set <path|id> --select <node> --value <json>   แก้จุดเดียว (value เป็น JSON: "open" · 3 · true · null=ลบ · {...})
  blm review patch <path> --line N [--end M | --insert] [--file <src>]  แทนบรรทัด N–M ด้วยเนื้อหาจาก --file/stdin · --insert = แทรกก่อน N
  blm review delete <path|id> [--confirm <name>] ไม่มี --confirm = ไม่ลบ แค่บอกให้ไปถามเจ้าของก่อน · ลบ = ย้ายไป <store>/.trash/ กู้คืนได้
path = id ของ review · relative ต่อ store · หรือต่อโปรเจ็ค · นอก store = ปฏิเสธ · ทุกครั้งจด <store>/commands.log
`

const grepHelp = `blm grep — ค้นหาคำศัพท์ในไฟล์

รูปแบบ:
  blm grep "<term1> <term2> ..." [--path <dir> | --file <file>] [--max-line N=20] [--max-result N=10] [--ext .ts,.md] [--sc] [--json] [--term "..."]

ตัวเลือก:
  --path <dir>      ค้นหา recursive ในโฟลเดอร์นี้ (ค่าเริ่มต้น = current directory)
  --file <file>     ค้นหาในไฟล์เดียวเท่านั้น (ไม่ใช้กับ --path)
  --max-line N      บรรทัดสูงสุดของ snippet window (ค่าเริ่มต้น 20; clamp ±N/2 รอบ hit)
  --max-result N    hits สูงสุดต่อคำศัพท์ (ค่าเริ่มต้น 10)
  --ext .ts,.md     รายการนามสกุลที่ค้นหา (ค่าเริ่มต้น .md,.txt,.ts,.tsx,.js,.jsx,.php,.sql,.sh,.go)
  --sc              ใช้ SocratiCode semantic search ถ้าพร้อม (fallback = regular scan)
  --imports         รวมบรรทัด import/export/require (ค่าเริ่มต้น = ข้าม)
  --json            ผลลัพธ์ JSON แทนตารางที่อ่านง่าย
  --term "..."      เพิ่มคำศัพท์เพิ่มเติม (ใช้ซ้ำได้)

ผลลัพธ์ (ตารางเริ่มต้น):
  term              ศัพท์ที่ค้นหา
  path:line         ไฟล์และบรรทัดที่ตรงกับ (1-indexed)
  snippet (start-end) ช่วงบรรทัดของ window (start และ end inclusive, 1-indexed)
  lines             จำนวนบรรทัดของ snippet
  text              บรรทัดที่ตรงกัน trimmed

Directory ที่ skip โดยอัตโนมัติ:
  node_modules, .git, .next, dist, build, vendor

ตัวอย่าง:
  blm grep "session login" --path src --max-line 30
  blm grep --term "portal" --term "snapshot" --file src/lib/snapshot.ts --json
  blm grep "config database" --ext .ts,.js --max-result 20 --sc`

const catHelp = `blm cat — แสดงผล grep result ที่บันทึกไว้พร้อม context

รูปแบบ:
  blm cat --grep-id <id> [--result-id <n,m,p>] [--context N=3] [--json]

ตัวเลือก:
  --grep-id <id>     ID ของ grep result ที่ต้องการอ่าน (ได้จากคำสั่ง blm grep)
  --result-id <n,m>  หมายเลข hit ที่ต้องการแสดง คั่นด้วยเครื่องหมายจุลภาค (ค่าเริ่มต้น = ทั้งหมด)
  --context N        บรรทัดเพิ่มเติมก่อนและหลัง hit (ค่าเริ่มต้น 3)
  --json             ผลลัพธ์ JSON แทนตารางที่อ่านง่าย

ผลลัพธ์ (ตารางเริ่มต้น):
  resultId          หมายเลข hit ในผล grep
  path:line         ไฟล์และบรรทัด
  context           จำนวนบรรทัด context
  lines             บรรทัดที่อ่านได้พร้อม highlight บรรทัดที่ตรงกับ

ตัวอย่าง:
  blm cat --grep-id 7f2k9q1x --result-id 2
  blm cat --grep-id 7f2k9q1x --result-id 1,3,5 --context 5
  blm cat --grep-id 7f2k9q1x --json`

func main() {
	if len(os.Args) < 2 {
		fmt.Println(usage)
		return
	}
	cmd, args := os.Args[1], os.Args[2:]
	if fi, err := os.Stdout.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 && os.Getenv("NO_COLOR") == "" && (runtime.GOOS != "windows" || os.Getenv("WT_SESSION") != "") {
		blm.Color = true // ANSI เฉพาะ terminal จริง (Windows: เฉพาะ Windows Terminal) · MCP/pipe = plain
	}
	err := run(cmd, args)
	// ท้ายผลลัพธ์เมื่อถูก pipe (= agent เรียกจาก Bash) บอก by · related · next เหมือน MCP tool (เจ้าของ 2026-09-21) · terminal จริงไม่พิมพ์
	if !blm.Color && cmd != "mcp" && cmd != "hook" && cmd != "statusline" && cmd != "guard" {
		if cmd == "update" && len(args) > 0 && args[0] != "draft" {
			for _, a := range args {
				if a == "--wait" { // RenderWait มี trailer ของตัวเองแล้ว
					cmd = ""
				}
			}
		}
		if cmd != "" {
			fmt.Println(mcp.Trailer("blm_" + strings.ReplaceAll(cmd, "-", "_"))[1:])
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(cmd string, args []string) error {
	root := mcp.Root()
	switch cmd {
	case "mcp":
		mcp.New(root).Serve(os.Stdin, os.Stdout)
		return nil
	case "guard":
		return cli.Guard(root, os.Stdin, os.Stdout)
	case "sc-daemon": // ภายใน: โปรเซสยาวที่ถือ socraticode ของโปรเจ็ค (เปิดโดย blm tools socraticode <fn> ครั้งแรก)
		if len(args) > 0 {
			root = args[0]
		}
		return blm.RunScDaemon(root)
	case "init":
		o, err := cli.ParseInit(args)
		if err != nil {
			return err
		}
		o.Out = os.Stdout
		lines := cli.Init(o)
		if len(lines) > 0 && strings.HasPrefix(lines[0], "stop") { // ถูกปฏิเสธก่อนเริ่ม: ยังไม่มีอะไรพิมพ์
			fmt.Println(strings.Join(lines, "\n"))
		} else {
			fmt.Println(lines[len(lines)-1]) // บรรทัด next (ที่เหลือพิมพ์สดไปแล้ว)
		}
		return nil
	case "path":
		fmt.Println(cli.EnsurePath())
		return nil
	case "self-update", "--self-update":
		flags, _ := splitFlags(args)
		if flags["help"] != "" || flags["h"] != "" {
			fmt.Println(updateHelp)
			return nil
		}
		r, err := blm.SelfUpdate(blm.SelfUpdateOpts{Check: flags["check"] != "", Binary: flags["binary"] != "", Plugin: flags["plugin"] != ""}, os.Stdout)
		if err != nil {
			return err
		}
		if flags["json"] != "" {
			out, _ := json.MarshalIndent(r, "", "  ")
			fmt.Println(string(out))
			return nil
		}
		fmt.Println(r.Terminal)
		if r.Reconnect {
			_ = exec.Command("sh", "-c", `printf "/reload-plugins" | pbcopy 2>/dev/null`).Run() // ขั้นแรกลง clipboard; reconnect พิมพ์ต่อเอง
		}
		return nil
	case "version", "--version", "-v":
		fmt.Println("blm", blm.Version)
		return nil
	case "help", "-h", "--help":
		fmt.Println(usage)
		return nil
	}
	// ที่เหลือคือ tool ของ MCP เรียกผ่านโค้ดชุดเดียวกัน — ผู้ใช้ได้ผลเหมือน agent โดยไม่ผ่าน AI
	// ไม่มี .claude/blm.json ในโฟลเดอร์นี้ = รันผิดที่ (เจ้าของเจอบ่อย 2026-09-09: "backend none has no sync target" ไม่บอกอะไร)
	// แต่ --help ไม่ต้องมี config เลย
	flags, rest := splitFlags(args)
	if flags["help"] != "" || flags["h"] != "" {
		if cmd == "grep" {
			fmt.Println(grepHelp)
			return nil
		}
		if cmd == "cat" {
			fmt.Println(catHelp)
			return nil
		}
		if cmd == "update" {
			fmt.Println(updateReportHelp)
			return nil
		}
	}
	if _, err := os.Stat(filepath.Join(root, blm.ConfigFile)); err != nil && cmd != "init" && cmd != "version" && cmd != "path" && cmd != "guard" && cmd != "sc-daemon" && cmd != "hook" && cmd != "statusline" && cmd != "mcp" && cmd != "grep" && cmd != "cat" && cmd != "self-update" && cmd != "--self-update" {
		return fmt.Errorf("no %s here (%s) — run blm inside the project folder, e.g. cd <project> && blm %s", blm.ConfigFile, root, cmd)
	}
	srv := mcp.New(root)
	asJSON := flags["json"] != ""
	var (
		res any
		err error
	)
	switch cmd {
	case "status":
		res, err = srv.Call("blm_status", nil2map(nil))
	case "update":
		a := map[string]any{}
		if len(rest) > 0 && rest[0] == "draft" {
			a["draft"] = true
			rest = rest[1:]
		}
		if len(rest) > 0 {
			a["topic"] = strings.Join(rest, " ")
		}
		if n, err := strconv.Atoi(flags["limit"]); err == nil {
			a["limit"] = float64(n)
		}
		if flags["wait"] != "" {
			// background ของ Claude Code: บล็อกจนมีงานใหม่ในรีวิว (แชท/comment/แถวใหม่/submit) หรือเจ้าของกด "จบ live" แล้วจบ → agent ถูกเรียกกลับพร้อม todo
			ref := flags["from"]
			if ref == "" {
				for _, rv := range blm.Open(root, blm.Load(root)).ListReviews() {
					if rv.Status != "done" {
						ref = rv.ID
						break
					}
				}
			}
			if ref == "" {
				return fmt.Errorf("no open review — blm update --html first")
			}
			sec := 240
			if n, err := strconv.Atoi(flags["timeout"]); err == nil && n > 0 {
				sec = n
			}
			w, err := blm.Open(root, blm.Load(root)).WaitViaServer(ref, time.Duration(sec)*time.Second)
			if err != nil {
				return err
			}
			if asJSON {
				out, _ := json.MarshalIndent(w, "", "  ")
				fmt.Println(string(out))
				return nil
			}
			fmt.Print(blm.RenderWait(w))
			return nil
		}
		if from := flags["from"]; from != "" {
			// ไฟล์เดิม → gen HTML ใหม่ + server รอ submit (ทุก event เรียกทางนี้)
			var r *blm.ReviewOpenResult
			if r, err = blm.Open(root, blm.Load(root)).ReopenReview(from); err == nil {
				res = r
			}
		} else if flags["html"] != "" || flags["to"] != "" {
			a["html"] = true
			if flags["open"] != "" {
				a["open"] = true
			}
			res, err = srv.Call("blm_update", a)
		} else {
			res, err = srv.Call("blm_update", a)
		}
		if r, ok := res.(*blm.ReviewOpenResult); ok && err == nil {
			if asJSON {
				break
			}
			fmt.Print(r.Terminal)
			if e := blm.OpenBrowser(r.URL); e != nil {
				fmt.Println(blm.Yellow("could not open a browser: " + e.Error() + " — open " + r.URL + " yourself"))
			}
			if flags["no-wait"] == "" {
				fmt.Println(blm.Dim("waiting for the submit in the browser… (Ctrl-C to stop; every click is already saved to " + r.File + ")"))
				if blm.Open(root, blm.Load(root)).WaitSubmit(r.ID, 0) {
					fmt.Println(blm.Green("submitted") + " → " + r.File + "\n" + blm.Cyan("next: ") + "/blm_update in Claude Code (the prompt hook also points the agent at it)")
				}
			}
			return nil
		}
	case "graph":
		a := map[string]any{"rebuild": flags["rebuild"] != "", "html": flags["html"] != ""}
		if len(rest) > 0 {
			a["query"] = rest[0]
		}
		res, err = srv.Call("blm_graph", a)
	case "scan":
		a := map[string]any{}
		if len(rest) > 0 {
			a["path"] = rest[0]
		}
		res, err = srv.Call("blm_scan", a)
	case "report":
		a := map[string]any{}
		if len(rest) > 0 {
			a["name"] = rest[0]
		}
		res, err = srv.Call("blm_report", a)
	case "tools":
		action, tool := "help", ""
		if len(rest) > 0 {
			action = rest[0]
		}
		if len(rest) > 1 {
			tool = rest[1]
		}
		if flags["help"] != "" || flags["h"] != "" {
			fmt.Println(blm.ToolsHelp)
			return nil
		}
		// เรียกตรง (ไม่ผ่าน MCP) เพื่อ stream output ของ script ออก terminal ทันที
		opts := blm.ToolOpts{Docker: flags["docker"] != "", Local: flags["local"] != "", Remote: flags["remote"] != "", Args: flags["args"]}
		if flags["remote"] != "1" {
			opts.RemoteHost = flags["remote"]
		}
		opts.SC = blm.SocratiCodeConfig{
			OllamaURL: flags["ollama-url"], QdrantURL: flags["qdrant-url"],
			EmbeddingModel: flags["embedding-model"], EmbeddingDimensions: flags["embedding-dimensions"], EmbeddingContextLength: flags["embedding-context-length"],
			EmbeddingQueryPrefix: flags["embedding-query-prefix"], EmbeddingDocumentPrefix: flags["embedding-document-prefix"],
		}
		text, terr := blm.Tools(root, blm.Load(root), action, tool, opts, os.Stdout)
		if terr != nil {
			return terr
		}
		fmt.Println(text)
		return nil
	case "sync":
		a := map[string]any{"direction": "push", "apply": flags["apply"] != "", "parallel": flags["parallel"] != ""}
		if flags["pull"] != "" {
			a["direction"] = "pull"
		}
		if d := flags["done"]; d != "" {
			a["done"] = toAny(strings.Split(d, ","))
		}
		if d := flags["delete"]; d != "" {
			a["delete"] = toAny(strings.Split(d, ","))
		}
		for _, k := range []string{"author", "role"} {
			if v := flags[k]; v != "" {
				a[k] = v
			}
		}
		res, err = srv.Call("blm_sync", a)
	case "get", "delete":
		if len(rest) < 1 {
			return fmt.Errorf("blm %s <name>", cmd)
		}
		res, err = srv.Call("blm_"+cmd, map[string]any{"name": rest[0]})
	case "create", "append", "replace":
		if len(rest) < 2 {
			return fmt.Errorf("blm %s <name> <file|-> [--target t] [--mode m] [--folder f] [--description d]", cmd)
		}
		content, rerr := readContent(rest[1])
		if rerr != nil {
			return rerr
		}
		a := map[string]any{"name": rest[0], "content": content}
		for _, k := range []string{"target", "mode", "folder", "description"} {
			if v := flags[k]; v != "" {
				a[k] = v
			}
		}
		if flags["confirm"] != "" {
			a["confirm"] = true
		}
		res, err = srv.Call("blm_"+cmd, a)
	case "diff":
		if len(rest) < 1 {
			return fmt.Errorf("blm diff <name>")
		}
		res, err = srv.Call("blm_diff", map[string]any{"name": rest[0]})
	case "merge":
		if len(rest) < 2 {
			return fmt.Errorf("blm merge <name> mine|cloud|content [file|-]")
		}
		a := map[string]any{"name": rest[0], "keep": rest[1]}
		if len(rest) > 2 {
			c, rerr := readContent(rest[2])
			if rerr != nil {
				return rerr
			}
			a["content"] = c
		}
		res, err = srv.Call("blm_merge", a)
	case "conflicts", "conflict":
		if len(rest) > 0 && rest[0] == "mark" {
			res, err = srv.Call("blm_conflict", map[string]any{"action": "mark"})
			break
		}
		if flags["i"] != "" || flags["interactive"] != "" {
			return interactiveConflicts(srv)
		}
		a := map[string]any{"all": flags["all"] != ""}
		if len(rest) > 0 {
			id, convErr := strconv.Atoi(strings.TrimPrefix(rest[0], "#"))
			if convErr != nil {
				return fmt.Errorf("blm conflicts [<id>]")
			}
			a["id"] = float64(id)
		}
		res, err = srv.Call("blm_conflicts", a)
	case "restore":
		// blm restore <history-file> <note> · blm restore <note> = list history files (เจ้าของกำหนดรูปคำสั่ง 2026-09-09)
		switch len(rest) {
		case 1:
			res, err = srv.Call("blm_restore", map[string]any{"name": rest[0]})
		case 2:
			res, err = srv.Call("blm_restore", map[string]any{"history": rest[0], "name": rest[1]})
		default:
			return fmt.Errorf("blm restore <history-file> <note>   (blm restore <note> lists its history)")
		}
	case "resolve":
		if len(rest) < 1 {
			return fmt.Errorf("blm resolve <note> [--keep current|incoming]")
		}
		a := map[string]any{"name": rest[0]}
		if k := flags["keep"]; k != "" {
			a["keep"] = k
		}
		if flags["no-push"] != "" {
			a["push"] = false
		}
		res, err = srv.Call("blm_resolve", a)
	case "patch":
		if len(rest) < 3 {
			return fmt.Errorf("blm patch <name> <find> <replace>")
		}
		res, err = srv.Call("blm_patch", map[string]any{"name": rest[0], "find": rest[1], "replace": rest[2]})
	case "search":
		if flags["help"] != "" || flags["h"] != "" {
			fmt.Println(searchHelp)
			return nil
		}
		if g := flags["get"]; g != "" {
			a := map[string]any{"get": g}
			if v := flags["id"]; v != "" {
				a["ids"] = v
			}
			if v := flags["context"]; v != "" {
				a["context"] = toNum(v, 0)
			}
			res, err = srv.Call("blm_search", a)
			break
		}
		if len(rest) < 1 {
			return fmt.Errorf("blm search \"<question>\" [--limit N] [--lang typescript] [--file p] [--exclude md] [--full | --brief] [--json]  ·  blm search --get <search-id> [--id 1,3] [--context N]")
		}
		a := map[string]any{"query": strings.Join(rest, " "), "brief": flags["brief"] != "", "full": flags["full"] != "", "noTrust": flags["no-trust"] != "", "excludeMd": flags["exclude"] == "md" || flags["exclude-md"] != ""}
		if v := flags["limit"]; v != "" {
			a["limit"] = toNum(v, 10)
		}
		if v := flags["lang"]; v != "" {
			a["lang"] = v
		}
		if v := flags["file"]; v != "" {
			a["file"] = v
		}
		if v := flags["min-score"]; v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				a["minScore"] = f
			}
		}
		res, err = srv.Call("blm_search", a)
	case "hook":
		if flags["help"] != "" || flags["h"] != "" || len(rest) < 1 {
			os.Stdout.WriteString(hookHelp + "\n")
			return nil
		}
		hookRoot := os.Getenv("CLAUDE_PROJECT_DIR")
		in := blm.ReadHookInput(os.Stdin)
		if hookRoot == "" {
			hookRoot = in.Cwd
		}
		if hookRoot == "" {
			hookRoot = root
		}
		// hook ห้ามล้ม: error = เงียบ exit 0
		_ = blm.Hook(rest[0], in, hookRoot, os.Stdout)
		return nil
	case "statusline":
		if flags["help"] != "" || flags["h"] != "" {
			os.Stdout.WriteString(hookHelp + "\n")
			return nil
		}
		in := blm.ReadHookInput(os.Stdin)
		slRoot := os.Getenv("CLAUDE_PROJECT_DIR")
		if slRoot == "" {
			slRoot = in.Cwd
		}
		if slRoot == "" {
			slRoot = root
		}
		fmt.Print(blm.Statusline(in, slRoot, flags["top-only"] != ""))
		return nil
	case "grep":
		if flags["help"] != "" || flags["h"] != "" {
			fmt.Println(grepHelp)
			return nil
		}
		if len(rest) < 1 {
			return fmt.Errorf("blm grep \"<term1> <term2> ...\" [--path <dir> | --file <file>] [--max-line N=20] [--max-result N=10] [--ext .md,.ts,...] [--sc] [--json]")
		}
		// Collect all terms: first from the positional arg, then from repeated --term flags
		var allTerms []string
		terms := strings.Fields(rest[0])
		allTerms = append(allTerms, terms...)
		if t := flags["term"]; t != "" {
			allTerms = append(allTerms, t)
		}
		a := map[string]any{"terms": strings.Join(allTerms, " ")}
		if f := flags["file"]; f != "" {
			a["file"] = f
		}
		if p := flags["path"]; p != "" {
			a["path"] = p
		}
		if ml := flags["max-line"]; ml != "" {
			a["maxLine"] = toNum(ml, 20)
		}
		if mr := flags["max-result"]; mr != "" {
			a["maxResult"] = toNum(mr, 10)
		}
		if e := flags["ext"]; e != "" {
			a["ext"] = e
		}
		if flags["sc"] != "" {
			a["sc"] = true
		}
		if flags["imports"] != "" {
			a["imports"] = true
		}
		res, err = srv.Call("blm_grep", a)
	case "review":
		if flags["help"] != "" || flags["h"] != "" || len(rest) < 1 || (rest[0] != "list" && len(rest) < 2) {
			fmt.Print(reviewHelp)
			return nil
		}
		o := blm.CommandOpts{Action: rest[0], Path: ".", Confirm: flags["confirm"], Select: flags["select"]}
		if o.Action == "patch" {
			o.Line, _ = strconv.Atoi(flags["line"])
			o.EndLine, _ = strconv.Atoi(flags["end"])
			o.Insert = flags["insert"] != ""
			if flags["file"] != "" {
				raw, err := os.ReadFile(flags["file"])
				if err != nil {
					return err
				}
				o.Content = string(raw)
			} else {
				raw, _ := io.ReadAll(os.Stdin)
				o.Content = string(raw)
			}
		}
		if o.Action == "set" {
			if v, ok := flags["value"]; !ok {
				return fmt.Errorf("blm review set <path|id> --select <node> --value <json>")
			} else if err := json.Unmarshal([]byte(v), &o.Value); err != nil {
				o.Value = v // ไม่ใช่ JSON = string ตรง ๆ
			}
		}
		if len(rest) > 1 {
			o.Path = rest[1]
		}
		if o.Action == "write" {
			if flags["file"] != "" {
				raw, err := os.ReadFile(flags["file"])
				if err != nil {
					return err
				}
				o.Content = string(raw)
			} else {
				raw, _ := io.ReadAll(os.Stdin)
				o.Content = string(raw)
			}
		}
		r, err := blm.Open(root, blm.Load(root)).Command(o)
		if err != nil {
			return err
		}
		if o.Action == "read" || o.Action == "get" {
			if o.Select != "" {
				raw, _ := json.MarshalIndent(r.Data, "", "  ")
				fmt.Println(string(raw))
				return nil
			}
			fmt.Print(r.Content)
			return nil
		}
		if o.Action == "list" {
			fmt.Println(strings.Join(r.Files, "\n"))
			return nil
		}
		res = r
	case "cat":
		if flags["help"] != "" || flags["h"] != "" {
			fmt.Println(catHelp)
			return nil
		}
		grepID := flags["grep-id"]
		if grepID == "" {
			return fmt.Errorf("blm cat --grep-id <id> [--result-id <n,m>] [--context N=3] [--json]")
		}
		a := map[string]any{"grepId": grepID}
		if rid := flags["result-id"]; rid != "" {
			a["resultId"] = rid
		}
		if ctx := flags["context"]; ctx != "" {
			a["context"] = toNum(ctx, 3)
		}
		res, err = srv.Call("blm_cat", a)
	default:
		return fmt.Errorf("unknown command %q\n%s", cmd, usage)
	}
	if err != nil {
		return err
	}
	if m, ok := res.(map[string]any); ok && !asJSON {
		if t, ok := m["terminal"].(string); ok {
			fmt.Println(t)
			if h, _ := m["html"].(string); h != "" {
				fmt.Println("html:", h, " (open it in a browser)")
			}
			return nil
		}
	}
	if r, ok := res.(*blm.SearchResult); ok && !asJSON {
		fmt.Println(r.Terminal)
		return nil
	}
	if r, ok := res.(*blm.UpdateReport); ok && !asJSON {
		fmt.Print(r.Terminal)
		return nil
	}
	if r, ok := res.(*blm.DraftsResult); ok && !asJSON {
		fmt.Print(r.Terminal)
		return nil
	}
	if r, ok := res.(*blm.Report); ok && !asJSON {
		fmt.Print(r.Content)
		fmt.Println("file:", r.File)
		return nil
	}
	if asJSON {
		out, _ := json.MarshalIndent(res, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	fmt.Print(renderHuman(res))
	return nil
}

// renderHuman พิมพ์ผลลัพธ์ให้คนอ่าน (เจ้าของ 2026-09-09: JSON ใน CLI อ่านยาก) · --json ยังได้ของเดิม
// รู้จักรูปที่ใช้บ่อย: แผน sync, ผล push, โน้ตหนึ่งใบ, blm_grep · ที่เหลือพิมพ์เป็น key: value ย่อหน้าตามชั้น ข้าม response ดิบ
func renderHuman(res any) string {
	var b strings.Builder
	// ผลจาก srv.Call เป็น type ของ Go (struct/slice) — วนผ่าน JSON ให้เป็น map/[]any รูปเดียวเหมือนที่ agent เห็น
	var generic any
	if raw, err := json.Marshal(res); err == nil {
		_ = json.Unmarshal(raw, &generic)
	}
	m, ok := generic.(map[string]any)
	if !ok {
		out, _ := json.MarshalIndent(res, "", "  ")
		return string(out) + "\n"
	}
	// blm_grep: table of hits using existing Table formatter
	if hits, ok := m["hits"].([]any); ok {
		if len(hits) == 0 {
			return "no matches\n"
		}
		grepID := str(m["id"])
		header := []string{"id", "term", "path:line", "snippet", "lines", "chars", "text"}
		var rows [][]string
		for _, h := range hits {
			hit, _ := h.(map[string]any)
			resultID := fmt.Sprint(int(num(hit["resultId"])))
			term, path := str(hit["term"]), str(hit["path"])
			start, end, lines, chars, text := int(num(hit["start"])), int(num(hit["end"])), int(num(hit["lines"])), int(num(hit["chars"])), str(hit["text"])
			pathLine := path + ":" + fmt.Sprint(int(num(hit["line"])))
			snippet := fmt.Sprintf("%d-%d", start, end)
			rows = append(rows, []string{resultID, term, pathLine, snippet, fmt.Sprint(lines), fmt.Sprint(chars), text})
		}
		table := blm.Table(header, rows)
		if grepID != "" {
			if !strings.HasSuffix(table, "\n") {
				table += "\n"
			}
			table += fmt.Sprintf("grep-id: %s\n", grepID)
		}
		return table
	}

	// blm_cat: display cat results with file content
	if results, ok := m["results"].(map[string]any); ok {
		if len(results) == 0 {
			return "no results found\n"
		}
		header := []string{"id", "path:line", "context", "code"}
		type resultItem struct {
			id  int
			res map[string]any
		}
		var items []resultItem
		for _, r := range results {
			res, _ := r.(map[string]any)
			if rid, ok := res["resultId"].(float64); ok {
				items = append(items, resultItem{int(rid), res})
			}
		}
		// Sort by ID
		sort.Slice(items, func(i, j int) bool { return items[i].id < items[j].id })

		var rows [][]string
		for _, item := range items {
			res := item.res
			rid := item.id
			path := str(res["path"])
			line := int(num(res["line"]))
			ctx := int(num(res["context"]))
			lines, _ := res["lines"].([]any)
			hit := int(num(res["hitLine"]))

			pathLine := fmt.Sprintf("%s:%d", path, line)
			contextStr := fmt.Sprintf("±%d", ctx)

			// Format code lines with hit highlighted (use actual file line numbers)
			var codeLines []string
			// Calculate the actual line number of the first line in the context
			firstLineNum := line - hit + 1
			for i, l := range lines {
				actualLineNum := firstLineNum + i
				lineStr := str(l)
				if i+1 == hit {
					codeLines = append(codeLines, fmt.Sprintf(">>> %d: %s", actualLineNum, lineStr))
				} else {
					codeLines = append(codeLines, fmt.Sprintf("    %d: %s", actualLineNum, lineStr))
				}
			}
			code := strings.Join(codeLines, "\n")

			rows = append(rows, []string{fmt.Sprint(rid), pathLine, contextStr, code})
		}
		return blm.Table(header, rows)
	}

	// โน้ตหนึ่งใบ (blm get): หัว + เนื้อหาเต็ม
	if c, ok := m["content"].(string); ok && m["target"] != nil {
		fmt.Fprintf(&b, "%s  (%s → %s · %s · base %s · updated %s)\n\n%s\n", str(m["name"]), str(m["mode"]), str(m["folder"]), map[bool]string{true: "edited", false: "synced"}[m["dirty"] == true], str(m["base"]), str(m["updatedAt"]), c)
		return b.String()
	}
	if plan, ok := m["plan"].([]any); ok {
		if len(plan) == 0 {
			b.WriteString("nothing to push — every note in blm/ matches the backend\n")
		} else {
			fmt.Fprintf(&b, "plan — %d note(s) differ from the backend (preview, nothing sent)\n", len(plan))
			for _, it := range plan {
				x, _ := it.(map[string]any)
				fmt.Fprintf(&b, "  %-22s %-8s → %-22s %6.0f bytes", str(x["name"]), str(x["mode"]), str(x["folder"]), num(x["bytes"]))
				if x["targetExists"] == false {
					b.WriteString("  (new on the backend)")
				}
				b.WriteString("\n")
				if w, ok := x["warnings"].([]any); ok {
					for _, l := range w {
						fmt.Fprintf(&b, "      ! %v\n", l)
					}
				}
			}
			b.WriteString("send: blm sync --push --apply\n")
		}
		return b.String()
	}
	if pushed, ok := m["pushed"].([]any); ok {
		if len(pushed) == 0 {
			b.WriteString("nothing to push\n")
		}
		for _, it := range pushed {
			x, _ := it.(map[string]any)
			mark := "✔"
			if x["verified"] != true {
				mark = "✘"
			}
			fmt.Fprintf(&b, "%s %-22s %-8s %5.0f ms", mark, str(x["name"]), str(x["mode"]), num(x["ms"]))
			if e := str(x["error"]); e != "" {
				fmt.Fprintf(&b, "  %s", e)
			}
			b.WriteString("\n")
		}
		if d, ok := m["deleted"].([]any); ok && len(d) > 0 {
			fmt.Fprintf(&b, "deleted on backend: %d\n", len(d))
		}
		if pr, ok := m["pull"].(map[string]any); ok {
			b.WriteString(renderHuman(pr))
		}
		fmt.Fprintf(&b, "%d pushed · %v failed · %.0f ms\n", len(pushed), m["failed"], num(m["ms"]))
		if n := str(m["next"]); n != "" {
			b.WriteString(n + "\n")
		}
		return b.String()
	}
	if pr, ok := m["pull"].(map[string]any); ok {
		for _, k := range []string{"refreshed", "conflicts", "upToDate"} {
			if l, ok := pr[k].([]any); ok && len(l) > 0 {
				fmt.Fprintf(&b, "%-10s %s\n", k+":", joinAny(l))
			}
		}
		if n := str(m["next"]); n != "" {
			b.WriteString(n + "\n")
		}
		return b.String()
	}
	if name := str(m["name"]); name != "" && m["mode"] != nil && m["ok"] != nil {
		state := "ok"
		if m["ok"] != true {
			state = "failed"
		}
		fmt.Fprintf(&b, "%s  %s  (%s → %s, %.0f bytes)\n", state, name, str(m["mode"]), str(m["folder"]), num(m["bytes"]))
		if chk, ok := m["check"].(map[string]any); ok && chk["needsConfirm"] == true {
			fmt.Fprintf(&b, "needs confirm: %s\n  old %.0f → new %.0f bytes · %.0f existing line(s) disappear\n", str(chk["reason"]), num(chk["oldBytes"]), num(chk["newBytes"]), num(chk["changedLines"]))
			if r := str(chk["removed"]); r != "" {
				b.WriteString("  first lines that would go:\n    " + strings.ReplaceAll(r, "\n", "\n    ") + "\n")
			}
		}
		if n := str(m["next"]); n != "" {
			b.WriteString(n + "\n")
		}
		return b.String()
	}
	writeKV(&b, m, "")
	return b.String()
}

func writeKV(b *strings.Builder, m map[string]any, indent string) {
	keys := make([]string, 0, len(m))
	for k := range m {
		if k == "response" || k == "content" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch v := m[k].(type) {
		case map[string]any:
			fmt.Fprintf(b, "%s%s:\n", indent, k)
			writeKV(b, v, indent+"  ")
		case []any:
			if len(v) == 0 {
				fmt.Fprintf(b, "%s%s: -\n", indent, k)
				continue
			}
			fmt.Fprintf(b, "%s%s:\n", indent, k)
			for _, it := range v {
				if im, ok := it.(map[string]any); ok {
					fmt.Fprintf(b, "%s  -\n", indent)
					writeKV(b, im, indent+"    ")
				} else {
					fmt.Fprintf(b, "%s  - %v\n", indent, it)
				}
			}
		default:
			fmt.Fprintf(b, "%s%s: %v\n", indent, k, v)
		}
	}
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func num(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	}
	return 0
}

func toNum(s string, def int) float64 {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return float64(def)
	}
	return float64(n)
}

func joinAny(l []any) string {
	parts := make([]string, 0, len(l))
	for _, x := range l {
		parts = append(parts, fmt.Sprint(x))
	}
	return strings.Join(parts, ", ")
}

func nil2map(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// splitFlags แยก --k v / --k=v / --flag ออกจาก positional (ไม่ใช้ package flag เพราะ positional ปนกับ flag ได้)
func splitFlags(args []string) (map[string]string, []string) {
	flags := map[string]string{}
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		// -i / -all แบบขีดเดียวก็รับ (เจ้าของพิมพ์ `blm conflicts -i` 2026-09-09) ยกเว้นเลขติดลบ
		if !strings.HasPrefix(a, "-") || len(a) < 2 || (a[1] >= '0' && a[1] <= '9') {
			rest = append(rest, a)
			continue
		}
		k := strings.TrimLeft(a, "-")
		if eq := strings.Index(k, "="); eq >= 0 {
			flags[k[:eq]] = k[eq+1:]
			continue
		}
		switch k {
		case "json", "push", "pull", "docker", "apply", "parallel", "rebuild", "html", "i", "interactive", "all", "no-push", "confirm", "sc", "imports", "help", "h":
			flags[k] = "1"
		case "local", "brief", "top-only", "exclude-md", "check", "binary", "plugin", "full", "no-trust":
			flags[k] = "1"
		default:
			// ค่าถัดไปเป็น flag อีกตัว (เช่น `--remote --embedding-model …`) = flag นี้ไม่มีค่า
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				flags[k] = args[i+1]
				i++
			} else {
				flags[k] = "1"
			}
		}
	}
	return flags, rest
}

func readContent(src string) (string, error) {
	if src == "-" {
		var b strings.Builder
		buf := make([]byte, 32*1024)
		for {
			n, err := os.Stdin.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		return b.String(), nil
	}
	raw, err := os.ReadFile(filepath.Clean(src))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// interactiveConflicts เมนูเทอร์มินัลล้วน (เจ้าของ 2026-09-09: ไม่มี checkbox แบบ TUI ก็ใช้คำสั่งล้วนได้):
// แสดงรายการที่ค้าง → พิมพ์เลข → เห็นรายงานแบบ git → c (current = ร่างในเครื่อง) / i (incoming = cloud) / s (ข้าม) → รายการนั้นหายจาก list
func interactiveConflicts(srv *mcp.Server) error {
	in := bufio.NewReader(os.Stdin)
	for {
		res, err := srv.Call("blm_conflicts", map[string]any{})
		if err != nil {
			return err
		}
		m := res.(map[string]any)
		fmt.Println(m["terminal"])
		open := 0
		for _, c := range m["conflicts"].([]blm.ConflictReport) {
			if c.Status == "wait" {
				open++
			}
		}
		if open == 0 {
			return nil
		}
		fmt.Print("report id to review (Enter = quit): ")
		line, _ := in.ReadString('\n')
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
		if line == "" {
			return nil
		}
		id, convErr := strconv.Atoi(line)
		if convErr != nil {
			fmt.Println("not a number")
			continue
		}
		show, err := srv.Call("blm_conflicts", map[string]any{"id": float64(id)})
		if err != nil {
			fmt.Println(err)
			continue
		}
		sm := show.(map[string]any)
		fmt.Println(sm["terminal"])
		note := sm["conflict"].(blm.ConflictReport).Draft
		fmt.Print("keep [c]urrent (local draft) · [i]ncoming (cloud) · [s]kip: ")
		ans, _ := in.ReadString('\n')
		keep := ""
		switch strings.ToLower(strings.TrimSpace(ans)) {
		case "c", "current":
			keep = "current"
		case "i", "incoming":
			keep = "incoming"
		default:
			continue
		}
		out, err := srv.Call("blm_resolve", map[string]any{"name": note, "keep": keep})
		if err != nil {
			fmt.Println(err)
			continue
		}
		om := out.(map[string]any)
		fmt.Printf("resolved: kept %s for %s · %v · %v\n", keep, note, om["done"], om["next"])
	}
}
