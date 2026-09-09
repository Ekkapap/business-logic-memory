---
name: blm
description: How to record project knowledge during a session without blocking on slow memory backends — the store in .agentsroom/blm/ is the working copy (blm.md = the one business-logic rules file + every note you touched), edited only through blm_create/append/patch, proposals for blm.md go through blm_conflict for the owner to decide, synced once when the owner says "update memory". Use whenever you learn something worth remembering mid-task, are about to decide on business logic, or the owner points at something in blm.md.
---

# blm — local memory ที่ทำงานจริง · กฎธุรกิจอยู่ไฟล์เดียว · เจ้าของตัดสิน

เครื่องมือทั้งหมดมาจาก binary `blm` ตัวเดียว: agent เรียกผ่าน MCP (`blm_*`) เจ้าของเรียกตรงในเทอร์มินัล (`blm status`, ต้องอยู่ในโฟลเดอร์โปรเจ็ค) โค้ดชุดเดียวกัน · `/blm:blm_help` = `blm --help` + สรุปนี้

## สามที่ เรียกให้ตรง (เจ้าของกำหนดชื่อ 2026-09-09)

- **blm.md / โน้ตใน blm/** = `.agentsroom/blm/` ที่ทำงานจริง โน้ตที่แตะครั้งแรกถูกดึงจาก AgentsRoom มาไว้ที่นี่พร้อม base สถานะต่อโน้ต `synced` / `edited` push แล้วไฟล์อยู่ต่อ base เลื่อน
- **mirror** = `.agentsroom/memory/` สำเนาของ AgentsRoom ที่แอปเขียนลงมา ใช้เทียบว่าใครใหม่กว่าเท่านั้น ไม่ใช่ที่แก้
- **AgentsRoom** = cloud ปลายทาง เปลี่ยนได้เฉพาะตอน push
- จำนวนไฟล์สองฝั่งไม่ต้องเท่ากัน (cloud มี knowledge/how-to ที่ไม่ใช่ business logic · blm/ มีร่าง รายงาน กราฟ) สิ่งเดียวที่ต้องมีทั้งสองที่คือ blm.md

## เขียน memory (agent ทำเอง ไม่ต้องรอใคร)

- `blm_create {name, content, description, folder, target, mode}` โน้ตใหม่เท่านั้น ชื่อซ้ำ = ปฏิเสธ
- `blm_append {name, content}` ต่อท้าย · `blm_patch {name, find, replace}` ค้นหา/แทนที่ พบพอดี 1 — ทั้งสองดึงโน้ตจาก mirror มาไว้ใน blm/ ให้เองถ้ายังไม่มี
- `blm_replace {name, content, confirm}` ทับทั้งไฟล์ ต้องมีอยู่แล้ว ต่างมาก (ขนาด >30% หรือบรรทัดเดิมหาย >30%) blm คืน `needsConfirm` พร้อมตัวเลขและบรรทัดที่จะหาย เรียกซ้ำด้วย `confirm:true` เฉพาะเมื่อตั้งใจ ห้ามใช้กับ blm.md
- **ห้าม `memory_save`/`memory_get` ตรง** (เนื้อโน้ตวิ่งผ่าน context และทับทั้งก้อน) · Edit/Write/Bash เขียน blm/ ถูก deny ทุกการเขียนผ่าน blm มี history: `history/<name>-[action]-YYYYMMDD-HHmmss.md` กู้ด้วย `blm_restore {name, history}` (`blm restore <ไฟล์> <โน้ต>`)
- **เจ้าของชี้ปัญหาใน blm.md** (ลิงก์ผิด ข้อความผิด) = อ่านบรรทัดนั้นจริง (`grep -n` ใน `.agentsroom/blm/` อ่านได้) แล้ว `blm_patch` ทันที **ห้ามตอบว่า sandbox ไม่ให้แก้** ข้อจำกัดนั้นมีเฉพาะซอร์ส Go ของ blm

## กฎธุรกิจ — blm.md

- "กฎที่เป็นจริงตอนนี้" เจ้าของเป็นคนตัดสิน · หัวไฟล์มีตาราง `## Main Business` · หัวข้อย่อยละ block พร้อม `memory:` `code:` `verify:` `updated_at/by` · ทุกคำที่อ้างไฟล์เป็นลิงก์ `[ชื่อ](path จาก root)` (linkPath ทำเองตอน checkout และ `blm conflict mark` ไม่ครอบด้วย `<>`)
- `blm {query?, trigger}` อ่านกฎ (ว่าง = ทั้งไฟล์ · คำ = block ที่ตรง) เจ้าของสั่ง `/blm "<หัวข้อ>"` ตอนเริ่มวัน · agent เรียกเองเงียบ ๆ ได้ทุกเมื่อที่ความจำขัดกับโค้ด
- โค้ดขัดกับกฎ = หยุดแล้วรายงาน ห้ามแก้โค้ดให้เข้ากับความจำ ห้ามแก้กฎเอง
- ชั้นของกฎ: blm.md = กฎหลักของโปรเจ็ค · โน้ต `features/<x>` = กฎย่อยของฟีเจอร์ (บรรทัด `memory:` คือทางลงไปอ่าน) · โปรเจ็คย่อยในโฟลเดอร์ (เช่น `wireguard/`) เป็นหัวข้อหลักของตัวเอง ไม่ใช่ noise
- **แก้ blm.md สองทาง**: ความหมายของกฎเปลี่ยนและเจ้าของยังไม่ตัดสิน → ยื่นข้อเสนอ (ด้านล่าง) · เจ้าของสั่งแก้จุดใดชัด ๆ หรือแก้เชิงกล (ลิงก์ ตัวสะกด ชื่อไฟล์) → `blm_patch {name:"blm", …}` ผลอยู่ในเครื่องจนสั่ง push
- เจ้าของเปลี่ยนกฎ = override ความจำเดิมทั้งเรื่อง → `blm_stat {event:"override", topic, note}` แล้วแก้ตามข้างบน

## ข้อเสนอและ conflict — เจ้าของตัดสินที่เดียว (`blm_conflict`)

- `blm_conflict {topic, heading, reason, content}` = **ข้อเสนอแก้ blm.md** (ใช้ตอน /blm_init หรือเมื่อกฎควรเปลี่ยน) Current = block ที่มีอยู่ (ว่าง = หัวข้อย่อยใหม่) Incoming = content ที่เสนอ ยื่นซ้ำเรื่องเดิม = แทนฉบับเก่า
- `blm_conflict {name, topic, heading, reason}` = **การชนจริง** ร่างใน blm/ กับ cloud ซ้อนกัน (`blm_diff` บอก) topic/heading = main/sub topic ของ blm.md ที่โน้ตนั้นเป็นส่วนประกอบ
- ทั้งสองแบบได้รายงาน `conflicts/[wait] <heading>-<เวลา>.md` รูป git: `***<<<<<<< Current …***` / `---` / `***>>>>>>> Incoming …***` เจ้าของแก้ข้อความในฝั่งที่จะเก็บได้ · reason/content เขียนภาษาของเจ้าของ · **รายงานอย่างเดียวไม่แตะไฟล์ใด**
- `blm_conflict {action:"mark"}` (`blm conflict mark`) ติดป้าย `[Conflict](path รายงาน)` ที่บรรทัด `memory:` หน้าลิงก์โน้ตที่ชน (หรือหัวข้อของ blm.md เองถ้าเป็นข้อเสนอ) และที่หัวข้อในโน้ตที่ชน · เรียกซ้ำได้ ผลเท่าเดิม · พ่วง linkPath ทั้ง blm.md ด้วย
- เจ้าของตัดสิน: ติ๊กช่องในไฟล์แล้วบอก "resolve" · หรือ `blm conflicts` → `blm conflicts <id>` → `blm resolve <โน้ต> --keep incoming|current` (`-i` = โหมดโต้ตอบในเทอร์มินัลจริง) · agent เรียก `blm_resolve {name, keep}` เมื่อเจ้าของบอกแล้วเท่านั้น
- resolve สำเร็จ = เขียนผลลงโน้ต ป้ายหาย รายงานเป็น `[done]` base เลื่อน และ **push โน้ตนั้นขึ้น AgentsRoom ทันที** (ปิดด้วย `push:false`)
- `blm_conflicts` / `blm conflicts` ดูที่ค้าง (`all:true` รวม done) `blm status` ก็มีส่วน Conflicts · **อย่าทวง** เจ้าของตัดสินเมื่อพร้อม

## sync (backend agentsroom/custom เท่านั้น)

- เจ้าของสั่ง "update memory" → `blm_sync {apply:true, author, role}` blm spawn AgentsRoom MCP เอง ยิง `memory_save` ทีละตัว ตรวจด้วย `memory_list` ครั้งเดียว โน้ตที่ตรง base ไม่ถูกส่ง ไม่ retry เอง เนื้อโน้ตไม่ผ่าน context · cloud ถูกแก้หลัง base → ไม่ push ทับ คืน `conflict:` ให้ไป `blm_diff` → `blm_merge {keep}` หรือยื่น `blm_conflict`
- `blm_sync {direction:"pull", apply:true}` เมื่อสงสัยว่า cloud เปลี่ยน: mirror สด แล้วโน้ตที่ไม่ได้แก้และ cloud ใหม่กว่า → ดึงทับ · แก้ค้าง+cloud เปลี่ยน → รายงานเป็น conflict ไม่ทับ
- ห้ามยิง `memory_save` เองแทนขั้นตอนนี้ เว้นแต่ `blm_sync` ตอบว่าไม่พบ AgentsRoom MCP ใน `.mcp.json`
- backend none/obsidian: ไม่มี sync ไฟล์ใน blm/ คือของจริง

## สำรวจโปรเจ็ค

- `blm_scan` ขนาด/token/โปรเจ็คย่อย · `blm_graph {query, rebuild, html}` กราฟ import+symbol+call แหล่งข้อมูล: SocratiCode ใน Qdrant → ast-grep → regex (ดูป้าย engine) + markdown pass · งาน local ขนานทุกคอร์ · cache `graph.json`
- /blm_init กับ blm.md ที่มีอยู่ = โหมดปรับปรุง: สำรวจ อ่านโน้ต แล้วยื่นทีละ block ผ่าน `blm_conflict` ไม่เขียนทับ · ร่างที่ยาวให้แยกเป็นโน้ตส่วนประกอบ (`blm_create` เช่น `custom-vpn-rules`) แล้ว ref จาก blm.md

## สถิติ

- `blm` ลง read เอง (`trigger:"user"` เมื่อเจ้าของสั่ง) · `blm_report {rows…}` ลง check เอง · NOT PASSED ที่แก้ความเข้าใจได้จากไฟล์กฎ → `blm_stat {event:"fixed", count}` · เจ้าของถามแล้วคุณค้นกฎมาตอบ → `blm_stat {event:"lookup", query}`

## กติกาการทำงานกับเจ้าของ (2026-09-09)

- ก่อนเพิ่ม tool / flag / ไฟล์ใหม่ใน blm บอกหนึ่งบรรทัดว่าจะเพิ่มอะไรและของเดิมขยายแทนได้ไหม แล้วรอคำตอบ
- ตอบสั้น จบเทิร์นเร็ว งานยาวรัน background · อ่านไฟล์จริงก่อนอธิบาย · ไม่ตั้งชื่อเรียกใหม่เอง · ทำงานได้ไม่ต้องรอใครให้ทำเลย ติดอะไรที่แก้เองไม่ได้ให้หยุดแล้วคุย ไม่วนลอง
