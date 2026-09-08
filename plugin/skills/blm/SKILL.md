---
name: blm
description: How to record project knowledge during a session without blocking on slow memory backends — jot into blm_* temp notes, read the business-logic rules file (blm.md) whenever memory conflicts with code, sync once at the end. Use whenever you learn something worth remembering mid-task or are about to decide on business logic.
---

# blm — จดก่อน sync ทีเดียว · กฎธุรกิจอยู่ไฟล์เดียว

เจ้าของกำหนด 2026-09-08: `memory_save` ของ AgentsRoom กลาง session ช้า (3–10 วิ ปกติ เคยเกิน 120 วิ) **ระหว่าง session ห้ามเรียก `memory_save`** ให้จดลง temp แทน
เครื่องมือทั้งหมดมาจาก binary `blm` ตัวเดียว: agent เรียกผ่าน MCP (`blm_*`) ผู้ใช้เรียกตรงใน terminal (`blm status`) โค้ดชุดเดียวกัน

## ระหว่าง session

- เรียนรู้อะไรที่ควรอยู่ใน project memory → `blm_save { name, target, content }`
  - `name` = `<target>-<YYYY-MM-DD>` เมื่อเป็น section ใหม่ของโน้ตเดิม · `target` = ชื่อโน้ตปลายทาง (agentsroom: ดู `.agentsroom/memory/INDEX.md`) · mode ค่าเริ่ม `append`
  - โน้ตปลายทางยังไม่มี → ใส่ `description` ("Contains …") และ `folder`
- เพิ่มเรื่องเข้าโน้ต temp เดิม → `blm_update` · แก้ประโยค → `blm_patch` (find ต้องพบพอดี 1 ครั้ง) · ดู/ลบ → `blm_get` · `blm_delete` · ภาพรวม → `blm_status`
- ไฟล์อยู่ใน store (`blm status` บอก path) อ่านตรงได้ แต่แก้ผ่านเครื่องมือเท่านั้น — Edit/Write/Bash ถูก deny · ทุกการแก้/ลบสำเนาเดิมไป `history/<name>-[update|append|patch|delete]-YYYYMMDD-HHmmss.md` อัตโนมัติ

## กฎธุรกิจ — `blm.md` (source of truth เดียว)

- `blm.md` คือ "กฎที่เป็นจริงตอนนี้" เจ้าของเป็นคนเปลี่ยนเท่านั้น โน้ต memory อื่นคือที่มา/ประวัติ · หัวไฟล์มีตาราง `## Main Business` = หัวข้อหลักทั้งหมด
- `blm` (ไม่ใส่ query = ทั้งไฟล์ · ใส่คำ = ทั้ง block ของหัวข้อย่อยที่ตรง) — เจ้าของสั่งผ่าน `/blm "<หัวข้อ>"` ตอนเริ่มวันและเมื่อ context บวม ตอบตามลำดับในคำสั่งนั้น
- **agent เรียก `blm "<เรื่อง>"` เองได้ทุกเมื่อ** ที่ความจำขัดกับโค้ด/โน้ต หรือกำลังจะตัดสินใจเรื่อง business logic — อ่านเงียบ ๆ ไม่ต้องทำรายงาน รายงานเต็มมีเฉพาะตอนเจ้าของสั่ง `/blm`
- โค้ดขัดกับกฎ = หยุดแล้วรายงาน ห้ามแก้โค้ดให้เข้ากับความจำ ห้ามแก้กฎเอง — เสนอผ่าน `blm_save target=blm` (ร่างรอ confirm) พร้อมคำสั่งของเจ้าของ
- เรื่องใหม่หรือหัวข้อย่อยที่โตเกินไป → เสนอยกเป็นหัวข้อหลัก (เจ้าของตัดสิน)

## สถิติ (ดูใน `blm_status` / `blm status`)

- `blm` ลง `read` ให้เอง (ส่ง `trigger: "user"` เมื่อเจ้าของสั่ง) · `blm_report {rows…}` ลง `check` ให้เอง
- หลังตรวจ: NOT PASSED ที่คุณแก้ความเข้าใจได้เองจากไฟล์กฎ → `blm_stat {event:"fixed", count}` · เจ้าของถามเรื่องกฎแล้วคุณค้นไฟล์กฎมาตอบ → `blm_stat {event:"lookup", query}`
- เจ้าของเปลี่ยนกฎ (ตอบต่างจากไฟล์ หรือสั่งเขียนกฎใหม่) = **override ความจำเดิมทั้งหมดของเรื่องนั้น** ต้องให้เจ้าของ confirm ก่อน แล้ว `blm_stat {event:"override", topic, note}` + อัปเดตร่างใน temp (`blm_save target=blm` ฉบับเต็มที่แก้แล้ว) — **ยังไม่ sync** จนกว่าเจ้าของสั่ง

## แก้บางบรรทัดของโน้ตปลายทางที่มีอยู่แล้ว

- **ห้าม** `memory_get` มาทั้งก้อนแล้ว `memory_save` กลับ (เนื้อโน้ตวิ่งผ่าน context สองรอบ) → ใช้ `blm_edit { target, find, replace }` blm อ่านโน้ตจาก mirror บนดิสก์ แทนข้อความ (ต้องพบพอดี 1) แล้วเก็บเป็นร่าง replace ใน temp คืนมาแค่ 3 บรรทัดรอบจุดแก้ แก้ซ้ำได้ต่อจากร่างเดิม
- section ใหม่ของโน้ตเดิม → `blm_save { name: "<target>-<วันที่>", target, content }` (mode append) ตามเดิม

## ตอนเจ้าของสั่ง "update memory" (มีเฉพาะ backend ที่มีปลายทาง: agentsroom/custom)

1. `blm_sync { apply: true, author: "<ชื่อคุณ>", role: "<role id>" }` — **blm ทำเองทั้งหมด**: spawn AgentsRoom MCP, ยิง `memory_save` ทุกรายการพร้อมกัน (แผนรวมโน้ต target เดียวกันแล้ว ทุกรายการคนละโน้ต), archive ตัวที่สำเร็จไป `.synced/` แล้วคืนแค่ ok/error/ms ต่อโน้ต เนื้อโน้ตไม่ผ่าน context ของคุณเลย (เจ้าของกำหนด 2026-09-09) · ใส่ `delete: ["ชื่อเก่า"]` เมื่อโน้ตถูกเปลี่ยนชื่อ (เช่น `project-business-logic` → `blm`)
2. อ่าน `warnings` และรายการ `failed` ถ้ามี — รายการที่ล้มยังอยู่ใน temp เรียกซ้ำได้ด้วย `names`
3. ห้ามเรียก `memory_save` เองแทนขั้นตอนนี้ เว้นแต่ `blm_sync` ตอบว่าไม่พบ AgentsRoom MCP ใน `.mcp.json` (เปิดโปรเจ็คใน AgentsRoom หนึ่งครั้งให้มันเขียน) ค่อยใช้แผนจาก `blm_sync` (ไม่ใส่ apply) ยิงขนานเอง
- `blm_sync { direction: "pull", apply: true }` ก่อนอ่านกฎเมื่อสงสัยว่า mirror เก่า (blm เรียก `memory_list` ให้ mirror สดเอง)
- backend none/obsidian: ไม่มี `blm_sync` เลย — ไฟล์ใน store คือของจริง

## กฎธุรกิจ — `blm.md` (source of truth เดียว)

- `blm.md` คือ "กฎที่เป็นจริงตอนนี้" เจ้าของเป็นคนเปลี่ยนเท่านั้น โน้ต memory อื่นคือที่มา/ประวัติ · หัวไฟล์มีตาราง `## Main Business` = หัวข้อหลักทั้งหมด
- `blm` (ไม่ใส่ query = ทั้งไฟล์ · ใส่คำ = ทั้ง block ของหัวข้อย่อยที่ตรง) — เจ้าของสั่งผ่าน `/blm "<หัวข้อ>"` ตอนเริ่มวันและเมื่อ context บวม ตอบตามลำดับในคำสั่งนั้น
- **agent เรียก `blm "<เรื่อง>"` เองได้ทุกเมื่อ** ที่ความจำขัดกับโค้ด/โน้ต หรือกำลังจะตัดสินใจเรื่อง business logic — อ่านเงียบ ๆ ไม่ต้องทำรายงาน รายงานเต็มมีเฉพาะตอนเจ้าของสั่ง `/blm`
- โค้ดขัดกับกฎ = หยุดแล้วรายงาน ห้ามแก้โค้ดให้เข้ากับความจำ ห้ามแก้กฎเอง — เสนอผ่าน `blm_save target=blm` (ร่างรอ confirm) พร้อมคำสั่งของเจ้าของ
- เรื่องใหม่หรือหัวข้อย่อยที่โตเกินไป → เสนอยกเป็นหัวข้อหลัก (เจ้าของตัดสิน)

## สถิติ (ดูใน `blm_status` / `blm status`)

- `blm` ลง `read` ให้เอง (ส่ง `trigger: "user"` เมื่อเจ้าของสั่ง) · `blm_report {rows…}` ลง `check` ให้เอง
- หลังตรวจ: NOT PASSED ที่คุณแก้ความเข้าใจได้เองจากไฟล์กฎ → `blm_stat {event:"fixed", count}` · เจ้าของถามเรื่องกฎแล้วคุณค้นไฟล์กฎมาตอบ → `blm_stat {event:"lookup", query}`
- เจ้าของเปลี่ยนกฎ (ตอบต่างจากไฟล์ หรือสั่งเขียนกฎใหม่) = **override ความจำเดิมทั้งหมดของเรื่องนั้น** ต้องให้เจ้าของ confirm ก่อน แล้ว `blm_stat {event:"override", topic, note}` + อัปเดตร่างใน temp (`blm_save target=blm` ฉบับเต็มที่แก้แล้ว) — **ยังไม่ sync** จนกว่าเจ้าของสั่ง

## ตอนเจ้าของสั่ง "update memory" (มีเฉพาะ backend ที่มีปลายทาง: agentsroom/custom)

1. `blm_sync` → `plan[]` แต่ละรายการคือ argument ของ `memory_save` ที่พร้อมส่ง (custom: `command` ให้รันผ่าน Bash)
2. อ่าน `warnings` แล้วทำตามแผน **ทุกรายการพร้อมกันในข้อความเดียว** (agentsroom: `memory_save` หนึ่ง tool call ต่อรายการ เติม `author`, `role` · custom: Bash เดียวต่อคำสั่งด้วย `&` + `wait`) — แผนรวมโน้ต target เดียวกันไว้แล้ว ทุกรายการจึงคนละโน้ต ยิงขนานได้ ห้ามรอทีละตัวเพราะ AgentsRoom ช้า (เจ้าของสั่ง 2026-09-09)
3. `blm_sync { done: [...plan[].from ที่สำเร็จ] }` → ไฟล์ย้ายไป `.synced/`
- `blm_sync {direction:"pull"}` ก่อนอ่านกฎเมื่อสงสัยว่า mirror เก่า (agentsroom: บอกให้เรียก `memory_list` บังคับ fetch)
- backend none/obsidian: ไม่มี `blm_sync` เลย — ไฟล์ใน store คือของจริง

## กฎ

- temp คือ *ร่างของ section ที่จะเข้าโน้ตปลายทาง* ไม่ใช่ knowledge base ตัวที่สอง ห้ามอ่าน temp แทน memory จริง
- ห้าม `memory_save` เอง นอกขั้นตอน sync — เว้นแต่เจ้าของสั่งชัดเจน
- backend ที่ช้า (AgentsRoom) อาจค้าง 30–120 วิ — ทุกอย่างที่เขียนได้ใน temp ให้เขียนใน temp เท่านั้น
