---
description: เพิ่มหัวข้อหลักใหม่ใน blm.md หลัง /blm_init ผ่านไปสักพัก — ไม่ scan ทั้งโปรเจ็ค · ระบุหัวข้อ = สกัดเรื่องนั้น · ไม่ระบุ = เสนอผู้สมัครจาก blm_update (กราฟ + git) ให้เจ้าของเลือกก่อน
argument-hint: ["ชื่อหัวข้อหลักใหม่" | draft ["ชื่อ"] — ว่าง = เปิดหน้า review ให้เจ้าของเลือกผู้สมัคร]
---

เป้าหมาย: หัวข้อหลักใหม่หนึ่งหัวข้อ = โน้ต `blm-<slug>` + แถวใหม่ในตาราง Main Business ของ blm.md ที่เจ้าของยอมรับ **หยุดรอคำตอบทุกขั้น** ถามเป็นประโยคธรรมดา ไม่ใช้ option list ไม่แตะหัวข้อที่มีอยู่ (กฎที่ควรเข้าหัวข้อเดิม = `blm_conflict {topic, heading, reason, content}`)

**หน้า review (ทางหลัก — เจ้าของกดปุ่น agent อ่านไฟล์เดียวกัน)**
- เริ่ม: `blm_update {html:true}` (หรือ `{topic, html:true}`) → บอก URL ให้เจ้าของ แล้ว**หยุดรอ** ไม่ต้องทำอะไรต่อ — เจ้าของติ๊ก ref, กด New topic (หลายหัวข้อได้) แล้วกด "blm topic create" (submit รวม) · ทุกคลิกลงไฟล์ทันทีแต่คุณถูกเรียกเมื่อ submit เท่านั้น: hook จะฉีด `[blm] review submitted … blm_update {from}` ในข้อความถัดไปของเจ้าของ (หรือเจ้าของบอกเอง)
- อ่าน: `blm_update {from:"<file>"}` → `review.topics` + `todo` · แต่ละหัวข้อมี **`refs`** (หลักฐาน → ดู `items` ที่ id ตรง แล้วเจาะด้วย `blm_search`/`blm_graph` ไม่ grep ไล่ไฟล์) · **`related`** (หัวข้อที่มีอยู่ที่เกี่ยวข้อง → `blm {query}` อ่านโน้ตพวกนั้น**ก่อน**เสนอ และในความหมายของหัวข้อย่อยบอกว่าแตะหัวข้อไหน เช่น "is_vpn → Portal & Service Access") · **`note`** = สิ่งที่เจ้าของบอกคุณตอนสร้าง ถือเป็นข้อเท็จจริง · status `new` = หัวข้อใหม่ · **`extend` + `existing`** = เอาไปรวมกับหัวข้อที่มีอยู่: อ่านโน้ตนั้น แล้วเสนอ**เฉพาะหัวข้อย่อยที่เพิ่ม** (ชื่อ/ความหมายเดิมส่งกลับตามเดิม) ตอนเขียนจริงใช้ `blm_append` เข้าโน้ต `blm-<existing>` ไม่สร้างโน้ตใหม่ และเพิ่มบรรทัด `memory:` ชี้กลับใน block ของหัวข้อ related ผ่าน `blm_conflict` · แถวที่เจ้าของติ๊ก **ignore** ถูกจำใน `reviews/ignore.json` และไม่โผล่ใน `blm update` อีก
- เสนอ: `blm_update {from, proposal:{topics:[{id, name, desc (cue ≤150), subs:[{id?, name, desc, refs}]}]}}` — **ส่งเฉพาะที่เปลี่ยน**: id เดิม + ฟิลด์/หัวข้อย่อยที่แตะ (name/desc ว่าง = คงเดิม · subs ที่ไม่ส่งยังอยู่ · ตัด = `drop:[subId]`) ไม่ต้องส่งทั้งหัวข้อซ้ำ → HTML ถูก gen ใหม่ บอกเจ้าของว่า "รอบ N พร้อมที่ URL" แล้วหยุดรอ · จุดที่ AGREE แล้วส่งข้อความเดิมกลับไป (คง AGREE) · จุดที่ COMMENT แก้ตาม comment · DRAFT = เจ้าของยังไม่ตัดสิน ส่งข้อความเดิมได้
- วนจน `next` บอกว่า "every point is AGREE" → เขียนจริง (ข้อ 4–5 ด้านล่าง) แล้วส่ง proposal เดิมพร้อม `note:"created blm-<slug>"` · `blm_update {draft:true}` = จุดที่ยัง DRAFT ทุกรีวิว ("$ARGUMENTS" = `draft` หรือ `draft "<topic>"` → เปิดจากตรงนี้)

**comment = แชต (เจ้าของ 2026-09-21)** — ปุ่ม comment ของทุกจุด (ชื่อ/ความหมาย/หัวข้อย่อย) เปิด modal แชตเดียวกับ ask ของแถว id = `<topic>:name` · `<topic>:desc` · `<topic>:sub:<sid>:name|desc` · ข้อความของเจ้าของ = COMMENT ค้าง → ตอบทันทีด้วย `answers:[{id, answer}]` และถ้าต้องแก้ข้อความจุดนั้นส่ง `topics` ในการเรียกเดียวกัน · agree/draft ไม่ปลุกคุณ รอ submit · คำตอบของ `{from}`/`{wait}` เป็นแบบย่อ (todo มี thread ครบ) — อยากได้จุดไหนของไฟล์ใช้ `blm_review {action:"get", path:<id>, select:"topics[id=…]"}` ไม่ต้องอ่านทั้งไฟล์ (`full:true` = ทั้งก้อน)

**โหมด live (เจ้าของกด ask/comment/agree แล้วคุณตอบทันที)** — หลังเปิดหน้าหรือส่ง proposal เรียก `blm_update {from, wait:true}` ทันที: มันบล็อกจนมีงานใหม่ (แชทในแถว, COMMENT, แถว New/Update topic, submit) แล้วคืน `todo` · ตอบด้วย `blm_update {from, proposal:{answers:[{id, answer}] / topics:[…]}}` (ระหว่างนั้นหน้าโชว์ "agent กำลังพิมพ์") แล้วเรียก `wait` อีก วนไปจน `stopped:true` (เจ้าของกด "จบ live") หรือเจ้าของบอกในแชท · `timeout:true` = ไม่มีอะไร เรียกซ้ำได้ · คำตอบในแชท: อ้างไฟล์เป็น path จริง (`src/lib/x.ts:L10`) หน้าเปิดให้คลิกดูได้ · โค้ดใส่ fenced block

**ไม่ระบุหัวข้อ และเจ้าของไม่ต้องการหน้า review** ("$ARGUMENTS" ว่าง)
1. `blm_update {}` → พิมพ์ `terminal` ตามตัวอักษร (หัวข้อที่มี + โฟลเดอร์ที่คุ้มครอง · cluster ที่ไม่มีกฎอ้าง · ไฟล์ที่เปลี่ยนหลังกฎล่าสุด)
2. จาก Candidates และ Changed "no rule" เสนอผู้สมัครหัวข้อหลัก 3–5 ข้อ พร้อมเหตุผลบรรทัดเดียว (โฟลเดอร์/ไฟล์แกน/เปลี่ยนล่าสุด) — ห้ามเดาความหมายจากชื่อโฟลเดอร์ ให้เปิด hub symbol หรือ `blm_search` เรื่องนั้น 1 ครั้งก่อนเขียนเหตุผล
3. รอเจ้าของเลือกหนึ่งหัวข้อ → ทำต่อแบบ "ระบุหัวข้อ"

**ระบุหัวข้อ**
1. `blm_update {topic: "$ARGUMENTS"}` → ผลค้น (สารบัญ + searchId) · อ่าน chunk ที่ score สูงและ origin `code`/`both ✓` ด้วย `blm_search {get: searchId, ids: […]}` · เจาะเพิ่มด้วย `blm_graph {query}` (ใครเรียกใคร) และ `blm_search` คำถามย่อย — ไม่ grep ไล่ไฟล์
2. **เสนอความหมาย** 1–2 ประโยค (cue ≤150 ตัวอักษร จะเป็นทั้ง description ของโน้ตและช่องความหมายในตาราง) → รอยืนยัน
3. **เสนอหัวข้อย่อย** พร้อม block ตามรูปของ /blm_init (`## หัวข้อย่อย` · `memory:` · `code:` · `verify:` · `updated_at/by` · กฎ 1–5 บรรทัด) → รอยืนยันทีละหัวข้อย่อย
4. เขียน: `blm_status` ดู `topicFolder` → `blm_create {name: "blm-<slug>", mode: "replace", folder: <topicFolder>, description: <cue>, content: "# <หัวข้อ>\n\n<blocks>"}` → `blm_patch {name: "blm", find: <แถวสุดท้ายของตาราง>, replace: <แถวนั้น + "\n| [<หัวข้อ>](<topicFolder>/blm-<slug>.md) | <cue> |">}`
5. ตรวจ: `blm {query: "<คำในกฎใหม่>"}` ต้องคืน block จาก `blm-<slug>.md` · `blm_status` บรรทัด Topic notes ต้องนับเพิ่ม · backend agentsroom/custom: บอกเจ้าของว่าสั่ง "update memory" เมื่อพร้อม (`blm_sync`)

ตอบเป็นภาษาไทย สั้น ตรง
