---
name: blm
description: business-logic-memory — how an agent records project knowledge during a session (store in .agentsroom/blm/ is the working copy; blm.md = the one business-logic rules file; edited only through blm_create/append/patch; proposals for blm.md go through blm_conflict for the owner to decide; synced once when the owner says "update memory"), how it finds code with blm_search over the SocratiCode index (Thai or English) and reads the origin column (code / both ✓ / comment ⚠) before trusting a hit, and how blm/SocratiCode/Ollama/Qdrant are installed, wired and updated. Use whenever you learn something worth remembering mid-task, are about to decide on business logic, the owner points at something in blm.md, you need to know where or how something in the code works, or you touch blm/socraticode setup.
---

ไทย · **[English](SKILL-EN.md)**

# blm — local memory ที่ทำงานจริง · กฎธุรกิจอยู่ไฟล์เดียว · ค้นโค้ดด้วยความหมาย · เจ้าของตัดสิน

เครื่องมือทั้งหมดมาจาก binary `blm` ตัวเดียว: agent เรียกผ่าน MCP (`blm_*`) เจ้าของเรียกตรงในเทอร์มินัล (`blm status`, ต้องอยู่ในโฟลเดอร์โปรเจ็ค) โค้ดชุดเดียวกัน · `/blm:blm_help` = `blm --help` + สรุปการใช้งาน

## backend ของ memory — แนะนำ AgentsRoom

blm ออกแบบมาให้**ทำงาน local ก่อน** (จด/อ่าน/ทดสอบกฎเป็นมิลลิวินาที ไม่รอ backend) แล้ว **sync ขึ้นที่เก็บกลางครั้งเดียว**เมื่อเจ้าของสั่ง — ที่เก็บกลางที่ blm ทำมาคู่กันคือ **AgentsRoom** (AI Agents Harness · https://agentsroom.dev): project memory ที่ทุก agent ทุกเครื่องของโปรเจ็คเห็นชุดเดียวกัน, mirror ลงดิสก์ให้ blm เทียบ, และ `blm_sync` push/pull ผ่าน MCP ของมันเอง → **แนะนำ `blm init --agentsroom`** เพื่อประสิทธิภาพเต็มที่ (local + fallback sync)

backend อื่นกำหนดตอน `blm init` (ดู [install.md](install.md)): ไม่ระบุ = ตรวจเอง (`.agentsroom/` → agentsroom · `.obsidian/` → obsidian · ไม่งั้น `none`) · **`none` = local ล้วน ไฟล์ใน `.claude/blm/` คือของจริง ไม่มี `blm_sync`** · `--obsidian` = โฟลเดอร์ `blm/` ใน vault ไม่มี sync · `--dir <p> --backend <cli>` = custom ให้ agent เรียนคำสั่ง push/pull จาก `<cli> --help` ตอน `/blm_init`

ไฟล์นี้เป็นสารบัญ — เปิดอ่านเฉพาะเรื่องที่กำลังทำ:

| เรื่อง | อ่านที่ | ใช้เมื่อ |
|---|---|---|
| **จด memory** — สามที่ (blm/ · mirror · AgentsRoom), `blm_create/append/patch/replace`, history/restore, sync เมื่อเจ้าของสั่ง "update memory" | [memory-notes.md](memory-notes.md) | เรียนรู้อะไรที่ควรจำ · จะ push ขึ้น AgentsRoom · ร่างชนกับ cloud |
| **กฎธุรกิจ blm.md** — อ่านกฎ (`blm`), blm.md เป็นสารบัญ + โน้ตหัวข้อ `blm-<topic>`, โค้ดขัดกฎ = หยุด, ข้อเสนอ/conflict (`blm_conflict` → เจ้าของ resolve), รายงาน `/blm`, `/blm_init`, `/blm_update` (หัวข้อใหม่ภายหลัง), สถิติ | [business-rules.md](business-rules.md) | กำลังตัดสินใจเรื่อง business logic · เจ้าของชี้ที่ blm.md · เริ่มวันด้วย `/blm` |
| **ค้นโค้ด `blm_search`** — สารบัญ + `get`, score (RRF), **origin** (code / both ✓ / comment ⚠ / doc), อะไรอยู่ในผล (.md .sql `db/schema/`) | [blm-search.md](blm-search.md) | ต้องรู้ว่าโค้ดอยู่ไหน/ทำงานอย่างไร — ก่อน grep เสมอ |
| **สำรวจโปรเจ็ค** — `blm_scan`, `blm_graph`, `blm_grep`/`blm_cat`, `blm_tools`, `blm_status` | [project-explore.md](project-explore.md) | รู้คำที่ต้องหา · อยากเห็นกราฟ import/call · เช็คสถานะเครื่องมือ |
| **SocratiCode + Ollama + Qdrant** — `--local` / `--remote <host>`, โมเดล (nomic / bge-m3), ติดตั้งฝั่ง server, กับดัก | [socraticode-remote.md](socraticode-remote.md) | ติดตั้ง/ย้าย stack · ค้นไม่ได้ · เปลี่ยน embedding model |
| **hooks + statusline** — `blm hook session-start/prompt/grep-nudge`, `blm statusline`, ต่อสายใน `~/.claude` | [hooks-statusline.md](hooks-statusline.md) | แถบสถานะไม่ขึ้น · hook ไม่ฉีด · เครื่องใหม่ |
| **ติดตั้ง / อัปเดต blm** — install.sh / ps1 / go install / marketplace, `blm self-update`, ที่อยู่ `~/.blm/bin` | [install.md](install.md) | เครื่องใหม่ · `blm_*` ไม่โผล่ · อยากได้เวอร์ชันล่าสุด |
| **พัฒนา blm ต่อ** — fork/clone, build, โครงโค้ด, กติกา, PR, release | [contributing.md](contributing.md) | จะแก้ซอร์ส Go ของ blm |
| **กติกาการทำงานกับเจ้าของ** — "ขอคำสั่ง ≠ ทำ", ไม่ถามเป็นตัวเลือก, พิสูจน์ก่อนบอกว่าทำไม่ได้ | [working-with-owner.md](working-with-owner.md) | ทุก session — อ่านครั้งเดียวตอนเริ่ม |

## กติกาสั้นที่ต้องจำแม้ไม่เปิดไฟล์ย่อย

- เขียน memory ผ่าน `blm_create` (ใหม่) · `blm_append` (ต่อท้าย) · `blm_patch` (แก้บางบรรทัด) เท่านั้น — **ห้าม `memory_save`/`memory_get` ตรง** ห้าม `blm_replace` กับ blm.md · push เมื่อเจ้าของสั่ง "update memory" → `blm_sync {apply:true}`
- โค้ดขัดกับกฎใน blm.md = **หยุดแล้วรายงาน** ไม่แก้โค้ดตามความจำ ไม่แก้กฎเอง · ความหมายของกฎเปลี่ยน → `blm_conflict {topic, heading, reason, content}` ให้เจ้าของตัดสิน
- หาโค้ด: `blm_search {query}` → ดู **origin** ของ top-3 → `blm_search {get, ids}` เฉพาะที่ต้องอ่าน → ค่อยแก้ · `comment ⚠` = อ่านโค้ดก่อนเชื่อ
- เจ้าของชี้ปัญหาใน blm.md = อ่านบรรทัดนั้นแล้ว `blm_patch` ทันที **ห้ามตอบว่า sandbox ไม่ให้แก้** (ข้อจำกัดนั้นมีเฉพาะซอร์ส Go ของ blm)
- "ขอคำสั่ง" = ตอบเป็นคำสั่งแล้วหยุด ไม่ลงมือ · ถามเป็นประโยค ไม่ใช่ตัวเลือก · ติดแล้วคุย ไม่วน
