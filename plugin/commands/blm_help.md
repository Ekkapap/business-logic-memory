---
description: วิธีใช้ blm ในโปรเจ็คนี้ — คำสั่งทั้งหมด (blm --help) และวิธีแก้ memory/blm.md ที่ถูกต้อง
---

แสดงวิธีใช้ blm ให้เจ้าของ (และให้ตัวคุณเองรู้) ทำตามลำดับ ตอบภาษาไทย สั้น

1. รัน `blm --help` และ `blm status` ด้วย Bash ในโฟลเดอร์โปรเจ็ค แล้วแสดงผลตามที่ได้ (ไม่ต้องสรุปซ้ำ)
2. ต่อด้วยสรุปนี้คำต่อคำ:

**ที่เก็บ** `.agentsroom/blm/` = local memory ที่ทำงานจริง (`blm.md` กฎธุรกิจ + โน้ตอื่น) · `.agentsroom/memory/` = mirror ของ AgentsRoom ไว้เทียบ · AgentsRoom = cloud ทุกอย่างแก้ผ่านคำสั่ง blm เท่านั้น

**แก้ memory (agent ทำเอง)**
- โน้ตใหม่ `blm_create` · ต่อท้าย `blm_append` · แก้บางบรรทัด `blm_patch` (ดึงโน้ตจาก mirror มาให้เองถ้ายังไม่มีใน blm/) · ห้าม `memory_save` ตรง ห้าม `blm_replace` กับ blm.md
- ส่งขึ้น AgentsRoom เมื่อเจ้าของสั่ง "update memory" → `blm_sync {apply:true}` · สงสัยว่า cloud เปลี่ยน → `blm_sync {direction:"pull", apply:true}`

**แก้กฎใน blm.md**
- ความหมายของกฎเปลี่ยนและเจ้าของยังไม่ตัดสิน → `blm_conflict {topic, heading, reason, content}` ได้รายงาน `[wait]` ใน `conflicts/`
- เจ้าของสั่งแก้จุดใดชัด ๆ หรือแก้เชิงกล (ลิงก์ ตัวสะกด) → `blm_patch {name:"blm", find, replace}`
- เจ้าของชี้ปัญหาใน blm.md = อ่านบรรทัดนั้น (`grep -n` ใน store อ่านได้) แล้ว patch ทันที ไม่ใช่ตอบว่า sandbox ไม่ให้แก้

**เจ้าของตัดสิน conflict/ข้อเสนอ (เทอร์มินัล ในโฟลเดอร์โปรเจ็ค)**
- `blm conflicts` รายการค้าง · `blm conflicts <id>` อ่านฉบับ · `blm resolve <โน้ต> --keep incoming|current` → แก้ไฟล์ ป้ายหาย push ทันที · `blm conflict mark` ติดป้าย `[Conflict]` ใน blm.md/โน้ตตามรายงานที่ค้าง

**กู้** `blm restore <โน้ต>` ลิสต์ history · `blm restore <ไฟล์ history> <โน้ต>` กู้กลับ

**ลิงก์ในไฟล์** `[ชื่อไฟล์](path จาก root โปรเจ็ค)` ทุกที่ที่อ้างไฟล์ (linkPath ทำให้อัตโนมัติตอน checkout/mark) ไม่ครอบด้วย `<>`

3. ปิดท้ายบรรทัดเดียว: `ตอนนี้: <จำนวนโน้ต edited> โน้ตรอ push · <จำนวน> conflict รอตัดสิน` จาก `blm status`
