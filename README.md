# blm — business-logic-memory

ไทย · **[English](README-EN.md)**

ไบนารีเดียว: หน่วยความจำชั่วคราวระหว่าง session ของ AI agent + กฎธุรกิจฉบับเดียว (`blm.md`) ที่ agent ใช้ทดสอบความเข้าใจตัวเอง + การค้นโค้ดเชิงความหมายที่บอกได้ว่าแต่ละผลลัพธ์เชื่อได้แค่ไหน

## ปัญหา

ทำงานกับ coding agent บนโปรดักต์จริง ทุก session เจอเรื่องเดิมซ้ำ ๆ:

- **หน่วยความจำช้าและอันตราย** memory กลางของโปรเจ็ค (AgentsRoom) คือที่ที่ควรเก็บการตัดสินใจ แต่การเขียนลงไประหว่างทำงานใช้เวลา 3–10 วินาที (บางครั้งเกินสองนาที) และ agent ที่เขียนโดยไม่อ่านก่อนก็เขียนทับโน้ตทั้งฉบับ ผลคือ agent เลิกเขียน — แล้ว session ก็ถูก compact ทั้งที่สิ่งที่ค้นพบยังอยู่แค่ในแชต — หรือไม่ก็เขียนแล้วทำพัง
- **กฎธุรกิจไม่มีที่อยู่ชัดเจน** กระจายอยู่ในโน้ตหลายสิบฉบับ คอมเมนต์ในโค้ด และในหัวเจ้าของ agent "จำ" กฎข้อหนึ่ง สร้างงานต่อจากมัน แล้วผิด เจ้าของมารู้จากผลลัพธ์ ไม่มีไฟล์เดียวที่บอกว่า*ตอนนี้อะไรจริง* ไม่มีทางให้ agent ทดสอบความเข้าใจตัวเองกับมัน และไม่มีที่ให้เจ้าของตัดสินเมื่อ agent เสนอเปลี่ยน
- **ค้นโค้ดได้แค่สองแบบ: ตาบอด หรือบวม** `grep` หาได้แต่ string ที่รู้อยู่แล้ว ส่วน semantic index (SocratiCode) หาความหมายได้แต่คืนโค้ดเต็ม ๆ สิบก้อนต่อคำถาม ต้องมี LLM + vector database ในเครื่องกิน RAM ราว 1 GB บนเครื่อง dev และบอกไม่ได้ว่าผลลัพธ์ตรงเพราะโค้ดหรือเพราะคอมเมนต์ที่อาจไม่จริงแล้ว
- **agent หลุดคอนเซ็ปต์** ทุก session ใหม่ — และทุกครั้งที่ auto-compact — agent เริ่มโดยไม่มีเป้าหมายและกฎหลักของโปรเจ็คในหัว เลย "ช่วย" ในเรื่องที่ไม่มีใครขอ หรือต่อยอดจากกฎที่จำได้ครึ่งเดียว brief ใหม่ด้วยมือกิน 30 นาทีถึงหนึ่งชั่วโมงทุกครั้ง แล้วก็เลือนไปกลางงานอยู่ดี
- **คอมเมนต์และเอกสารเก่ากว่าโค้ด** คอมเมนต์ สเปก ไฟล์ migration กระทั่ง CLAUDE.md ของโปรเจ็คเอง อาจบรรยายสถานะที่โค้ดทิ้งไปแล้ว การค้นที่เชื่อทุกแหล่งเท่ากันพา agent ไปผิดทางด้วยความมั่นใจเต็มร้อย

## blm ทำอะไร

- **local ก่อน sync ครั้งเดียว** โน้ตถูกแก้บนดิสก์ในหลักมิลลิวินาที (`blm_create` / `blm_append` / `blm_patch` — checkout จาก backend พร้อม base snapshot มี history ทุกการเปลี่ยน) แล้ว push ขึ้น AgentsRoom ในขั้นเดียวเมื่อเจ้าของสั่ง "update memory" เนื้อโน้ตไม่ผ่าน context ของ agent การชนกับเวอร์ชันบน cloud ที่ใหม่กว่าจะถูกรายงาน ไม่ถูกเขียนทับ ใช้ได้กับ AgentsRoom (แนะนำ), Obsidian, CLI ของคุณเอง หรือไม่มี backend เลย
- **กฎไฟล์เดียว เจ้าของตัดสิน** `blm.md` เก็บกฎธุรกิจ*ตามที่เป็นอยู่ตอนนี้* พร้อมลิงก์ `memory:` / `code:` / `verify:` agent อ่านมัน รายงาน self-test ผ่าน `/blm` (สิ่งที่เชื่อ*ก่อน*อ่าน เทียบกับที่กฎบอก) และเสนอการเปลี่ยนเป็นรายงาน conflict สไตล์ git ที่เจ้าของเป็นคน resolve — ไฟล์ไม่เปลี่ยนลับหลังเจ้าของ สถิติบอกว่ากฎดึง agent กลับมาบ่อยแค่ไหน
- **คอนเซ็ปต์กลับมาเอง** hook กลางตัวเดียว (`blm hook <event>`) ฉีด*แก่น*ของ `blm.md` — ตาราง Main Business, รายชื่อ rule block และกติกา 3 ข้อ — ทุกครั้งที่เริ่ม session รวมทั้งทันทีหลัง auto-compact และซ้ำก่อน prompt หรือหลังแก้ไฟล์**เฉพาะเมื่อพ้น debounce** (ค่าเริ่มต้น 20 นาที หรือเมื่อ `blm.md` เปลี่ยน) agent ที่แก้ห้าสิบไฟล์ติดกันจึงไม่ถูก brief ห้าสิบรอบ `/blm` ให้เจ้าของเช็คได้ทุกเมื่อว่า agent กับกฎยังตรงกัน
- **ค้นโค้ดที่ให้คะแนนผลลัพธ์ตัวเอง** `blm search` ใช้ index ของ SocratiCode (query hybrid dense + BM25 เดียวกัน คะแนนเดียวกัน) แต่ตอบเป็นสารบัญสองบรรทัดต่อผลแทนโค้ดสิบก้อน เปิดเฉพาะก้อนที่เลือกด้วยบรรทัดจริงจากไฟล์ ตัด `.md` ของโน้ต/memory ออกจากผล และเพิ่มคอลัมน์ **origin**: คำถามตรงกับ*โค้ด*หรือแค่*คอมเมนต์* และคอมเมนต์นั้นตรงกับโค้ดของมันไหม (`code` · `both ✓` · `comment ⚠`) ถามได้ทั้งไทยและอังกฤษ ไม่ต้องรู้ชื่อ symbol
- **สถานะปัจจุบัน ไม่ใช่ประวัติ** สำหรับ SQL index เฉพาะ snapshot schema รายตาราง (`db/schema/<table>.sql` ที่ gen ใหม่หลัง migrate บน dev ทุกครั้ง) — migration และ dump ไม่ index — "ตารางไหนเก็บ X" จึงตกที่คำตอบจริงคำตอบเดียว
- **stack อยู่ที่ที่มันควรอยู่** Ollama + Qdrant รันบนเครื่อง GPU (`blm tools install socraticode --remote <host>`) เครื่อง dev ไม่มีอะไรค้าง และคำสั่งเดียวกันต่อสาย hook ของ Claude Code (สถานะ index ตอนเริ่ม session, pointer top-3 ต่อ prompt, เตือนเมื่อ agent grep แทนที่จะถาม) กับ statusline (`socraticode : online · 4213 nodes / 24326 edges · ✓ synced`)
- **plugin เดียว อัปเดตตัวเอง** `blm init` ติดตั้งไบนารี plugin ของ Claude Code และ hook; `blm self-update` อัปเดตทั้งคู่ CLI กับ MCP ใช้โค้ดทางเดียวกัน สิ่งที่เจ้าของเห็นในเทอร์มินัลคือสิ่งที่ agent เห็น

ต่อไป: โฮสต์ indexer ของ SocratiCode เป็นโปรเซสลูกให้ blm เป็น plugin เดียว, จูน threshold ของ origin จากเคสที่วัดจริง, ติดตั้ง stack บนเซิร์ฟเวอร์ GPU ด้วยคำสั่งเดียว และ `--check` ใน CI สำหรับ schema snapshot

- **store คือ working copy** — `.agentsroom/blm/` เก็บ `blm.md` และทุกโน้ตที่ agent แตะ checkout จาก backend พร้อม base snapshot; mirror ของ backend มีไว้แค่บอกว่าใครใหม่กว่า โน้ตเป็น `synced` หรือ `edited`; push แล้วไฟล์อยู่ที่เดิมและเลื่อน base
- **จดเร็วระหว่าง session** — `blm_create` (โน้ตใหม่เท่านั้น) · `blm_append` · `blm_patch` (find/replace, checkout โน้ตให้ก่อน) · `blm_replace` (ทั้งโน้ต ขอ `confirm:true` เมื่อเนื้อหาใหม่ต่างมาก) ไฟล์ local หลักมิลลิวินาที; backend ที่ช้า (AgentsRoom, wiki, CLI) sync ครั้งเดียวเมื่อเจ้าของสั่ง
- **กฎไฟล์เดียว `blm.md`** — อะไรจริง*ตอนนี้* มนุษย์เป็นเจ้าของ แต่ละหัวข้อย่อยมี `memory:` `code:` `verify:` `updated_at:` และทุกคำที่เป็นชื่อไฟล์กลายเป็นลิงก์ (`[db.ts](src/lib/db.ts)`, โน้ตจาก store หรือ mirror) — `linkPath` ทำงานตอน checkout และตอน `blm conflict mark`
- **`blm.md` เป็นสารบัญได้** แถวในตาราง Main Business ลิงก์ไปโน้ตหัวข้อได้ — `| [Authentication](global/conventions/blm/blm-authentication.md) | cue บรรทัดเดียว |` — และโน้ตนั้นถือ rule block ทั้งหมดของหัวข้อ `blm {query}` ตามลิงก์ไป (ref ชี้ไฟล์หัวข้อ) ข้อเสนอและ resolve ลงที่โน้ตหัวข้อ ลิงก์ที่หายโชว์เป็น `(missing)` และ core brief ที่ hook ฉีดคือตารางนี้เป๊ะ โฟลเดอร์หัวข้อ: `global/conventions/blm` บน AgentsRoom, `blm` บนสุดของ `<ai-dir>/memory` (`.claude/memory/blm/`) ที่อื่น — หรือ `topicsFolder` ใน `.claude/blm.json` / env `BLM_TOPICS_FOLDER` (`BLM_MEMORY_DIR` ย้าย root ของ memory)
- **conflict และข้อเสนอ เจ้าของตัดสิน** — โน้ต local ชนกับ backend หรือ agent เสนอเปลี่ยน `blm.md` กลายเป็นรายงานสไตล์ git `conflicts/[wait] <heading>-<time>.md` (Current = local / ที่มีอยู่, Incoming = cloud / ที่เสนอ) เจ้าของติ๊กช่อง หรือรัน `blm conflicts -i` / `blm resolve <note> --keep incoming|current`; blm เขียนโน้ตใหม่ เปลี่ยนชื่อรายงานเป็น `[done]` ถอดเครื่องหมาย `[Conflict](…)` แล้ว push `blm conflict mark` เป็นคนวางเครื่องหมาย; รายงานอย่างเดียวไม่เปลี่ยนอะไร
- **รายงาน self-test** — `/blm ["topic"]`: agent เขียนสิ่งที่เชื่อ*ก่อน*อ่าน แล้วรายงาน PASSED / NOT PASSED / UNKNOWN ต่อหัวข้อย่อยพร้อม ref `.agentsroom/blm/blm.md:<line>` ที่คลิกได้ คอลัมน์จัดตามความกว้าง monospace จริง (สระไทยลอย = 0, emoji = 2)
- **สถิติแบบ `rtk gain`** — agent ผิดบ่อยแค่ไหน กฎดึงกลับบ่อยแค่ไหน มนุษย์ต้องเปลี่ยนกฎบ่อยแค่ไหน
- **history** — ทุกการเปลี่ยน/ลบ snapshot ไฟล์เดิมไป `history/<name>-[action]-YYYYMMDD-HHmmss.md`; `blm restore <history-file> <note>` เอากลับมา (`blm restore <note>` แสดงรายการ)
- **ล็อกการเขียน** — Edit/Write ถูกห้าม, hook `blm guard` ที่ PreToolUse, sandbox ระดับ OS `denyWrite` (ตัวเลือก) มีแต่ `blm` ที่เขียน store ได้
- **ข้ามแพลตฟอร์ม** — ไบนารี Go สำหรับ macOS / Linux / Windows ผู้ใช้เรียก `blm status` ตรง ๆ; agent เรียกโค้ดเดียวกันผ่าน MCP (`blm mcp`)

## ติดตั้ง (บรรทัดเดียว ในโปรเจ็คของคุณ)

> รันคำสั่งติดตั้ง**จาก root ของโปรเจ็ค**ที่ต้องการให้ blm จำ (โฟลเดอร์ที่มี `.git` / `.agentsroom` / `package.json` …) `blm init` ไม่ยอมรันที่อื่นเว้นแต่ใส่ `--force` ไม่ระบุ backend = ตรวจเอง: มี `.claude/blm.json` → ใช้ของเดิม · `.agentsroom/` → agentsroom · `.obsidian/` → obsidian · นอกนั้น none

```sh
# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/Ekkapap/business-logic-memory/main/install.sh | sh -s -- --agentsroom
# Windows (PowerShell)
$env:BLM_INIT_ARGS="--agentsroom"; irm https://raw.githubusercontent.com/Ekkapap/business-logic-memory/main/install.ps1 | iex
# Go toolchain
go install github.com/Ekkapap/business-logic-memory/cmd/blm@latest && blm path && blm init --agentsroom
# Homebrew (tap กำลังมา)
```

ตัวติดตั้งวางไบนารีที่ `~/.blm/bin/blm` (Windows: `~\.blm\bin\blm.exe`) และลิงก์ `~/.local/bin/blm` มาที่นั่น; `blm self-update` แทนไฟล์นี้ด้วย GitHub release ล่าสุด นักพัฒนา: `make install` build ลง `~/.blm/bin` เดียวกัน (repo ยังเป็น checkout สะอาดสำหรับ commit), `make install-dev` ลิงก์ `~/.local/bin/blm` ไป `./bin/blm` ของ repo แทน ซึ่งกรณีนั้น `blm self-update` จะ `git pull` + `go build` ที่นั่น

ตัวเลือกของ `blm init` — backend เป็นตัวกำหนดว่าความจริงอยู่ที่ไหน:

| flag | store | sync |
| --- | --- | --- |
| *(ไม่ใส่)* | `.claude/blm` | ไม่มี — ไฟล์คือความจริง (ไม่เปิด `blm_sync`) |
| `--agentsroom` | `.agentsroom/blm` + mirror `.agentsroom/memory` | push = แผน `memory_save`, pull = refresh mirror |
| `--obsidian` | `blm/` (เห็นใน vault) | ไม่มี |
| `--dir <p> --backend <cli>` | `<p>` | agent เรียนวิธี push/pull จาก `<cli> --help` ตอน `/blm_init` |

เพิ่มเติม: `--tools socraticode,obsidian,graphify` (เครื่องมือข้างเคียงที่จะติดตาม; ค่าเริ่มต้น = ที่ติดตั้งอยู่), `--sandbox` (เปิด OS sandbox ของ Claude Code ใน user settings), `--no-plugin`, `--marketplace <dir|owner/repo>`
`init` เขียน `.claude/blm.json`, store, `.ignorememory`, merge `.claude/settings.json` (กฎ deny, sandbox denyWrite, hook `blm guard`), ต่อสาย hook ระดับเครื่องใน `~/.claude/settings.json` (`blm hook session-start / prompt / grep-nudge / post-edit`), เพิ่ม `blm` ลง PATH (หรือพิมพ์คำสั่งให้ถ้าทำไม่ได้) และลงทะเบียน plugin ของ Claude Code รันซ้ำได้ไม่ซ้ำซ้อน store `.agentsroom/memory-temp` แบบเก่าถูกย้ายให้

จากนั้นใน Claude Code: `/reload-plugins` → `/blm_init` (วิเคราะห์แบบนำทาง: หัวข้อหลัก → ยืนยัน → ความหมาย → ยืนยัน → หัวข้อย่อยต่อหัวข้อ → `blm.md`)

## คำสั่ง

| ผู้ใช้ (เทอร์มินัล ไม่ใช้ AI) | agent (MCP tool) | slash |
| --- | --- | --- |
| `blm status [--json]` — ความพร้อมก่อน (`READY` / `NOT READY <why>`) แล้ว path, กฎ, โน้ตชั่วคราว, tools, สถิติ; สไตล์ rtk-gain มีสีบน TTY | `blm_status` | `/blm_status` |
| `blm report [name]` | `blm_report` | `/blm_report` |
| `blm tools <action> [tool]` | `blm_tools` | `/blm_tools` |
| `blm sync --push \| --pull [--apply]` | `blm_sync` | — |
| `blm create/append/patch/replace <name> …` · `blm get/delete <name>` | `blm_create` · `blm_append` · `blm_patch` · `blm_replace` · `blm_get` · `blm_delete` | `create` ไม่รับชื่อที่มีอยู่ · `append`/`patch` checkout โน้ตจาก mirror ของ backend ก่อน · `replace` ต้อง `--confirm` เมื่อเปลี่ยนมาก |
| `blm restore <history-file> <note>` | `blm_restore` | `blm restore <note>` แสดงไฟล์ history |
| `blm diff <name>` · `blm merge <name> mine\|cloud\|content` | `blm_diff` · `blm_merge` | — |
| `blm conflicts [<id>] [-i] [--all]` · `blm conflict mark` · `blm resolve <note> [--keep current\|incoming] [--no-push]` | `blm_conflict {action: report\|mark, name \| content, topic, heading, reason}` · `blm_conflicts` · `blm_resolve {name, keep, push}` | ใส่ `content` = ข้อเสนอต่อกฎ (Current = block ตามที่เป็น, Incoming = ข้อเสนอ) — ลงที่โน้ตหัวข้อเมื่อหัวข้อถูกแยกออกจาก blm.md · resolve push โน้ตให้เว้นแต่ `--no-push` |
| `blm scan [path]` · `blm graph [query] [--rebuild]` | `blm_scan` · `blm_graph` | แหล่งกราฟ: กราฟ SocratiCode ใน Qdrant → ast-grep → regex · งาน local ใช้ทุก core (`BLM_WORKERS` กำหนดเอง) |
| `blm search "<question>" [--lang typescript] [--file p] [--exclude md] [--limit N] [--full]` · `blm search --get <search-id> --id 1,3 [--context N]` | `blm_search` | ค้นเชิงความหมายบน index ของ SocratiCode **โดยไม่ผ่าน MCP ของมัน** — คำตอบเป็นสารบัญ (บรรทัดละผล: #id · score · path:lines · preview) + search-id แล้ว `--get` เปิดเฉพาะก้อนที่เลือกด้วยบรรทัดจริง (`--full` แนบทั้งหมดแบบ codebase_search): embed ด้วยโมเดล/prefix เดียวกับ index (`.claude/blm.json` ส่วน socraticode → env), Qdrant hybrid dense+BM25 RRF — คะแนนเดียวกับ `codebase_search` (1.0 = อันดับ 1 ทั้งสองฝั่ง) · `.md` ใต้ store ของ blm และ `.agentsroom/` ถูกตัดเสมอ `--exclude md` ตัด `.md` ทั้งหมด · ไทยหรืออังกฤษ |
| `blm grep "<terms>" [--path \<dir\> \| --file \<file\>] [--max-line N] [--max-result N] [--ext .ts,.md] [--sc]` | `blm_grep` | ค้น substring ไม่สนตัวพิมพ์; snippet window = ขอบบรรทัดว่างหรือ ±maxLine/2 รอบ hit; นามสกุล/โฟลเดอร์ที่ข้ามตั้งได้; pool 4 worker; --sc ถาม `blm search` ก่อนเพื่อหาไฟล์ผู้สมัคร |
| — | `blm` (อ่านกฎ) | `/blm ["topic"]` |
| — | `blm_stat` | — |
| `blm hook session-start\|prompt\|post-edit\|grep-nudge` · `blm statusline [--top-only]` | — | hook กลางตัวเดียวของ Claude Code ระดับเครื่องใน `~/.claude/settings.json`: แก่นของ `blm.md` (ตาราง Main Business + rule block + กติกา 3 ข้อ) ทุกครั้งที่เริ่ม session รวมหลัง compact และ — ตาม debounce (`BLM_CORE_INTERVAL` ค่าเริ่มต้น 20 นาที หรือเมื่อ blm.md เปลี่ยน) — ก่อน prompt / หลัง Write/Edit; สถานะ index ตอนเริ่ม session, pointer `file:line` top-3 ต่อ prompt (ไม่แนบโค้ด), เตือนหลัง Grep/Glob; statusline `socraticode : online · N nodes / M edges · ✓ synced\|⟳ indexing` (+ บรรทัด ctx% / session) — `blm hook --help` พิมพ์ block ของ settings ให้ |
| `blm update ["<topic>"] [--limit N]` | `blm_update {topic?, limit?}` | `blm.md` ครอบคลุมอะไรและยังไม่ครอบคลุมอะไร — จุดเริ่มของ `/blm_update` (เพิ่มหัวข้อหลักหลัง `/blm_init` ไปหลายเดือน) โดยไม่ scan ใหม่และไม่ใช้ LLM: หัวข้อ + โฟลเดอร์ที่บรรทัด `code:` ของมันคุ้มครอง · cluster ของกราฟที่ไม่มีกฎอ้าง · ไฟล์ที่เปลี่ยนหลังกฎล่าสุด (`no rule` / `covered — rule may be stale`) · ใส่หัวข้อ = สารบัญ `blm search` ของเรื่องนั้น รายงานเดียวกันทั้งเทอร์มินัลและ agent · `--html` เปลี่ยนเป็นหน้ารีวิว (`<store>/reviews/review-<id>.json` + `.html` บน `127.0.0.1`): เจ้าของติ๊ก ref กด New topic แล้ว comment / agree / draft ชื่อและความหมายที่เสนอทุกจุด; agent เขียนข้อเสนอลงไฟล์เดียวกัน (`blm_update {from, proposal}`); ทุกคลิกถูกบันทึก agent ถูกเรียกเมื่อ submit หรือทันทีในโหมด live · `--html --from <json>` gen ใหม่ · `blm update draft` แสดงที่ยัง DRAFT |
| `blm self-update [--check] [--binary \| --plugin]` (หรือ `blm --self-update`) | `blm_selfupdate` | อัปเดต blm เอง: ไบนารี (dev checkout → `git pull` + `go build` ในที่ · ติดตั้งแบบ global → GitHub release ล่าสุดแทนไฟล์) + plugin (`claude plugin marketplace update blm` → `claude plugin update blm@blm`) · แล้ว `/mcp reconnect plugin:blm:blm` |
| `blm review get <id> · list [folder] · read <path> · write <path|id> [--file] · set <path|id> --select <node> --value <json> · patch <path> --line N [--end M \| --insert] · delete <path|id> [--confirm name]` | `blm_review {action, path, content?, select?, value?, line?, endLine?, insert?, confirm?}` | ไฟล์ใน store ของ blm (review json/html, ignore.json…) ผ่านโปรเซส blm — ทางเดียวเมื่อ sandbox ของ Bash ฝั่ง agent ห้ามเขียน `.agentsroom/blm/**` · `get --select` ดึงเฉพาะจุดใน JSON (`topics[id=x].subs`, `items[id=c2].chat`, `[+]` ต่อท้าย) · `set` แก้จุดเดียว · `patch` แทนช่วงบรรทัด · `delete` โดยไม่มี `confirm` ไม่ลบ (ถามเจ้าของก่อน); ไฟล์ที่ลบไป `<store>/.trash/`; ทุกครั้งจด `<store>/commands.log` · `/blm_review` = เปิดหน้า update ประจำ (`/r/update`) ในเบราว์เซอร์แล้วเข้าโหมด live |
| `blm guard` (hook) · `blm mcp` (server) · `blm path` · `blm init` | | `/blm_init` · `/blm_update ["topic"]` (หัวข้อหลักใหม่หนึ่งหัวข้อ: ผู้สมัคร → ความหมาย → หัวข้อย่อย → `blm-<slug>` + แถวในตาราง) · `/blm:blm_help` (help + วิธีแก้ memory และ blm.md) |

`blm tools` ดูแลเครื่องมือวิเคราะห์โค้ดข้างเคียงที่ agent พึ่งพาตอน `/blm_init` (ลด token ของ agent): `socraticode`, `obsidian`, `graphify` — `status · get · install · start · stop · restart · gen-graph` logic ติดตั้งอยู่ใน `scripts/tools.sh` (macOS/Linux) และ `scripts/tools.ps1` (Windows) ฝังในไบนารี; ทุกการติดตั้งตรวจก่อนแล้วเพิ่มเฉพาะที่ขาด

```sh
blm init --agentsroom --tools socraticode            # ชุดเดียว: ตั้งค่า + ติดตั้งเครื่องมือที่ขาด
blm tools install socraticode --local                # Qdrant + Ollama + nomic-embed-text บนเครื่องนี้ (mac: brew · linux: release binary + ollama.com)
blm tools install socraticode --docker               # หรือ Docker (ติดตั้ง Docker CLI ให้ถ้าไม่มี; Windows: docker เท่านั้น)
blm tools install socraticode --remote 192.168.1.50 --embedding-model bge-m3 --embedding-dimensions 1024 --embedding-context-length 8192
                                                     # บริการอยู่อีกเครื่อง (เครื่อง GPU ในเครือข่าย): ไม่ติดตั้งอะไรที่นี่
blm tools install socraticode --remote               # ภายหลัง: ใช้ค่าที่ .claude/blm.json จำไว้ (เช่นเครื่อง dev ตัวที่สองหลัง git pull)
blm tools update socraticode                         # plugin SocratiCode ล่าสุดจาก marketplace ของมัน (ตัว MCP รัน socraticode@latest อยู่แล้ว)
blm tools status
```

`--local` เขียน `QDRANT_MODE/QDRANT_URL/OLLAMA_MODE/OLLAMA_URL=external/127.0.0.1` ลง `env` ของ `~/.claude/settings.json` ให้ SocratiCode ใช้บริการของคุณแทนการเปิด container; โหมด docker ถอดออก
`--remote <host>` ไม่ติดตั้งอะไร: blm ตรวจว่า Ollama (`:11434` พร้อมโมเดล) และ Qdrant (`:6333`) ตอบบน host นั้น (`--ollama-url` / `--qdrant-url` สำหรับ port อื่น) บันทึกค่าเป็นส่วน `socraticode` ของ `.claude/blm.json` และเขียน env ทั้งเก้าตัวของ SocratiCode (`OLLAMA_*`, `QDRANT_*`, `EMBEDDING_MODEL/DIMENSIONS/CONTEXT_LENGTH/QUERY_PREFIX/DOCUMENT_PREFIX`) ลง `~/.claude/settings.json` — reconnect plugin socraticode หลังจากนั้น โมเดลที่ไม่ใช่ nomic ได้ prefix ว่างเว้นแต่ใส่ `--embedding-query-prefix` / `--embedding-document-prefix` ทั้งสองโหมดต่อสาย Claude Code ให้ด้วย: hook `blm hook session-start|prompt|grep-nudge` ลง `~/.claude/settings.json` (แทนเฉพาะรายการเดิมของ blm; hook ของเครื่องมืออื่นเช่น graft คงอยู่และบอกวิธีถอดเอง) และบรรทัด socraticode ท้าย `~/.claude/statusline-command.sh` (สร้างจาก statusline ค่าเริ่มต้นของ blm เมื่อไม่มีไฟล์; `statusLine` ใน settings ชี้มาที่นี่เว้นแต่คุณใช้คำสั่งอื่นอยู่) — รันซ้ำได้ ไม่มี flag: `install socraticode` = `--local` เมื่อยังไม่ตั้งค่าอะไร; ถ้าโปรเจ็คมีส่วน remote อยู่แล้ว blm หยุดและให้บอก `--remote` (ใช้ต่อ) หรือ `--local` (ติดตั้งที่นี่ ถอดส่วน remote) กันติดตั้ง local โดยไม่ตั้งใจ; `blm status` / `blm graph` / `blm grep --sc` อ่านส่วนเดียวกัน จึงตามเซิร์ฟเวอร์ไปด้วย ใช้ได้บน Windows (ไม่มีสคริปต์เกี่ยว) เครื่องมือที่ติดตั้งทีหลังถูกเพิ่มใน `tools` ของ `.claude/blm.json` ให้เอง

## โครงสร้าง

- `cmd/blm` — entrypoint · `internal/blm` — store (checkout/base/dirty, restore), rules, linkpath, conflict/propose/resolve, diff/merge, graph (socraticode/ast-grep/regex, ขนาน), layout, stats, status, tools, update/review, hook · `internal/mcp` — stdio JSON-RPC · `internal/cli` — init, guard, PATH
- `plugin/` — plugin ของ Claude Code (manifest ชี้ `blm mcp`, skill, commands) · `.claude-plugin/marketplace.json` — repo นี้คือ marketplace · skill เป็นสารบัญ (`plugin/skills/blm/SKILL.md`) ที่ลิงก์ไปไฟล์ละหัวข้อ: [memory-notes](plugin/skills/blm/memory-notes.md) · [business-rules](plugin/skills/blm/business-rules.md) · [blm-search](plugin/skills/blm/blm-search.md) (ค้นหา, คะแนน, origin) · [project-explore](plugin/skills/blm/project-explore.md) · [socraticode-remote](plugin/skills/blm/socraticode-remote.md) (stack local/remote, ตั้งเซิร์ฟเวอร์) · [hooks-statusline](plugin/skills/blm/hooks-statusline.md) · [install](plugin/skills/blm/install.md) · [contributing](plugin/skills/blm/contributing.md) (fork → PR → release) · [working-with-owner](plugin/skills/blm/working-with-owner.md)
- `make test` · `make install` (build → `~/.blm/bin/blm`, `~/.local/bin/blm` symlink มาที่นั่น — layout เดียวกับ install.sh) หรือ `make install-dev` (`~/.local/bin/blm` → `./bin/blm` ของ repo นี้) (เวอร์ชัน = git tag ล่าสุด เช่น `2.0.6` หรือ `2.0.6-2-g33bc0b6` หลัง tag) · `make release` (เพิ่มเลข patch จาก tag ล่าสุดบน origin อัตโนมัติ; `RELEASE_VERSION=v2.1.0` เพื่อกำหนดเอง) — 5 target + GitHub Release ผ่าน `gh`
