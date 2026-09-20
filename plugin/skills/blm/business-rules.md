# กฎธุรกิจ — blm.md · ข้อเสนอและ conflict · รายงาน /blm · สถิติ

## blm.md คืออะไร

- "กฎที่เป็นจริงตอนนี้" ของโปรเจ็ค เจ้าของเป็นคนตัดสิน · หัวไฟล์มีตาราง `## Main Business` · หัวข้อย่อยละ block พร้อม `memory:` `code:` `verify:` `updated_at/by` · ทุกคำที่อ้างไฟล์เป็นลิงก์ `[ชื่อ](path จาก root)` (linkPath ทำเองตอน checkout และ `blm conflict mark`)
- `blm {query?, trigger}` อ่านกฎ (ว่าง = ทั้งไฟล์ · คำ = block ที่ตรง) คืน `refShort` เป็น `<store>/blm.md:<line>` ที่คลิกได้, `changedSinceLastRead`, `pendingDrafts` · เจ้าของสั่ง `/blm "<หัวข้อ>"` ตอนเริ่มวัน · agent เรียกเองเงียบ ๆ ได้ทุกเมื่อที่ความจำขัดกับโค้ด
- **โค้ดขัดกับกฎ = หยุดแล้วรายงาน** ห้ามแก้โค้ดให้เข้ากับความจำ ห้ามแก้กฎเอง
- **blm.md เป็นสารบัญได้** (เจ้าของ 2026-09-20): แถวในตาราง Main Business เป็นลิงก์ `[Topic](<topicFolder>/blm-<topic>.md)` → block กฎของหัวข้อนั้นทั้งหมดอยู่ในโน้ต `blm-<topic>` (front matter `folder:` = topicFolder) และ `blm {query}` โหลดตามลิงก์ให้เอง (ref ชี้ไฟล์หัวข้อ) · หัวข้อที่ยังไม่แยกอยู่ใต้ `# Topic` ใน blm.md ได้เหมือนเดิม สองแบบอยู่ร่วมกันได้ · ลิงก์ที่หาไฟล์ไม่เจอโผล่เป็น block `(missing)` และใน `blm status` · topicFolder: agentsroom = `global/conventions/blm` · backend อื่น = `blm` บนสุดของ `<ai-dir>/memory` (`.claude/memory/blm/`) · ตั้งเองใน `.claude/blm.json` `topicsFolder` หรือ env `BLM_TOPICS_FOLDER` (memory root: env `BLM_MEMORY_DIR`) · core brief ที่ hook ฉีด = ตารางนี้พอดี
- ชั้นของกฎ: blm.md = กฎหลักของโปรเจ็ค · โน้ต `features/<x>` = กฎย่อยของฟีเจอร์ (บรรทัด `memory:` คือทางลงไปอ่าน) · โปรเจ็คย่อยในโฟลเดอร์ (เช่น `wireguard/`) เป็นหัวข้อหลักของตัวเอง ไม่ใช่ noise
- **แก้ blm.md สองทาง**: ความหมายของกฎเปลี่ยนและเจ้าของยังไม่ตัดสิน → ยื่นข้อเสนอ (ด้านล่าง) · เจ้าของสั่งแก้จุดใดชัด ๆ หรือแก้เชิงกล (ลิงก์ ตัวสะกด ชื่อไฟล์) → `blm_patch {name:"blm", find, replace}` ผลอยู่ในเครื่องจนสั่ง push
- เจ้าของเปลี่ยนกฎ = override ความจำเดิมทั้งเรื่อง → `blm_stat {event:"override", topic, note}` แล้วแก้ตามข้างบน

## ข้อเสนอและ conflict — เจ้าของตัดสินที่เดียว (`blm_conflict`)

- `blm_conflict {topic, heading, reason, content}` = **ข้อเสนอแก้กฎ** (ใช้ตอน /blm_init หรือเมื่อกฎควรเปลี่ยน) Current = block ที่มีอยู่ (ว่าง = หัวข้อย่อยใหม่) Incoming = content ที่เสนอ ยื่นซ้ำเรื่องเดิม = แทนฉบับเก่า · topic ที่แยกเป็นโน้ต `blm-<topic>` แล้ว → รายงาน/resolve/push ลงโน้ตนั้น (`blm resolve blm-<topic>`) ไม่ใช่ blm.md — ข้อเสนอเก่าที่ยื่นก่อนแยกไฟล์ก็ถูกชี้ไปโน้ตหัวข้อให้เอง · ตอน resolve ข้อเสนอถูก "ต่อ" เข้าเนื้อหาปัจจุบันของโน้ต (ไม่มี snapshot ที่อาจเก่ากว่าไฟล์)
- `blm_conflict {name, topic, heading, reason}` = **การชนจริง** ร่างใน blm/ กับ cloud ซ้อนกัน (`blm_diff` บอก) topic/heading = main/sub topic ของ blm.md ที่โน้ตนั้นเป็นส่วนประกอบ
- ทั้งสองแบบได้รายงาน `conflicts/[wait] <heading>-<เวลา>.md` รูป git: `***<<<<<<< Current …***` / `---` / `***>>>>>>> Incoming …***` เจ้าของแก้ข้อความในฝั่งที่จะเก็บได้ · reason/content เขียนภาษาของเจ้าของ · **รายงานอย่างเดียวไม่แตะไฟล์ใด**
- `blm_conflict {action:"mark"}` (`blm conflict mark`) ติดป้าย `[Conflict](path รายงาน)` ที่บรรทัด `memory:` หน้าลิงก์โน้ตที่ชน (หรือหัวข้อของ blm.md เองถ้าเป็นข้อเสนอ · หัวข้อที่แยกไฟล์แล้ว = หน้าลิงก์ในแถวตาราง Main Business + ที่ `## heading` ในโน้ตหัวข้อ) และที่หัวข้อในโน้ตที่ชน · เรียกซ้ำได้ ผลเท่าเดิม · พ่วง linkPath ทั้ง blm.md ด้วย
- เจ้าของตัดสิน: ติ๊กช่องในไฟล์แล้วบอก "resolve" · หรือ `blm conflicts` → `blm conflicts <id>` → `blm resolve <โน้ต> --keep incoming|current` (`-i` = โหมดโต้ตอบในเทอร์มินัลจริง) · agent เรียก `blm_resolve {name, keep}` เมื่อเจ้าของบอกแล้วเท่านั้น
- resolve สำเร็จ = เขียนผลลงโน้ต ป้ายหาย รายงานเป็น `[done]` base เลื่อน และ **push โน้ตนั้นขึ้น AgentsRoom ทันที** (ปิดด้วย `push:false`)
- `blm_conflicts` / `blm conflicts` ดูที่ค้าง (`all:true` รวม done) `blm status` ก็มีส่วน Conflicts · **อย่าทวง** เจ้าของตัดสินเมื่อพร้อม

## รายงาน /blm (self-test)

- `/blm ["หัวข้อ"]`: agent เขียนสิ่งที่ตัวเองเชื่อ **ก่อน** อ่านกฎ (Before) → `blm {query}` → เทียบทีละหัวข้อย่อย PASSED / NOT PASSED / UNKNOWN พร้อม ref `<store>/blm.md:<line>` → After · `blm_report {rows, before, after}` จัดคอลัมน์ด้วย display width จริง (สระไทย 0 ช่อง emoji 2)
- NOT PASSED ที่แก้ความเข้าใจได้จากไฟล์กฎ → `blm_stat {event:"fixed", count}`

## /blm_update — หัวข้อหลักใหม่หลัง /blm_init ผ่านไปนาน (เจ้าของ 2026-09-20)

- `blm_update {topic?}` = `blm update` ใน terminal: รายงานความครอบคลุมของ blm.md โดยไม่ scan ซ้ำ ไม่ใช้ LLM — หัวข้อที่มี (โน้ต, block, updated_at เก่าสุด, โฟลเดอร์ที่บรรทัด `code:` อ้าง) · cluster ในกราฟที่ไม่มีกฎอ้าง (ผู้สมัคร) · ไฟล์ที่เปลี่ยนหลังกฎล่าสุด (git log เฉพาะไฟล์ที่ index; `no rule` = ยังไม่มีกฎ / `covered — rule may be stale`) · ระบุ topic = แนบสารบัญ `blm_search` ของเรื่องนั้น
- เจ้าของรันเองก่อนได้เพื่อเลือกหัวข้อ แล้วสั่ง `/blm_update "<ชื่อ>"` — agent อ่าน hit → เสนอความหมาย (cue ≤150 = description ของโน้ต + ช่องในตาราง) → หัวข้อย่อย → ยืนยัน → `blm_create blm-<slug>` (folder = `topicFolder` จาก `blm_status`) + `blm_patch` เพิ่มแถวในตาราง · ไม่ระบุ = agent เสนอผู้สมัคร 3–5 ข้อจากรายงานให้เลือกก่อน
- **หน้า review** (`blm update --html` / `blm_update {html:true}`): รายงานเป็น `<store>/reviews/review-<id>.json` + `.html` เสิร์ฟที่ `http://127.0.0.1:<port>/r/<id>` (พอร์ตคงที่ต่อโปรเจ็ค; ใน `blm mcp` server อยู่กับ process · CLI รอจน submit) — แถวข้อมูลประกอบติ๊กได้หลายคอลัมน์: `ref` (หลักฐาน) · `relate` (หัวข้อที่มีอยู่ที่เกี่ยวข้อง — agent อ่านก่อนเสนอ) · `target` (เอาไปรวมกับหัวข้อนี้ อันเดียว → หัวข้อ status `extend` + `existing`, เขียนจริงด้วย `blm_append`) · `ignore` (จำใน `reviews/ignore.json`, blm update ไม่เสนออีก) + ช่อง "บอก agent" (`note`) → New topic / Add to <topic> → "blm topic create" · รอบต่อไป agent ใส่ข้อเสนอลง**ไฟล์เดียวกัน** (`blm_update {from, proposal}`) ทุกจุด (ชื่อ/ความหมายของ main และ sub topic) มีปุ่ม comment(+บันทึก)/agree/draft · ทุกคลิกลงไฟล์ทันที สถานะหลักเปลี่ยนและ agent ถูกเรียก (hook prompt ฉีด `[blm] review submitted`) เมื่อ submit รวมเท่านั้น · ข้อความที่ agent แก้ = ของเดิมลง `history` พร้อม comment, AGREE ที่ไม่แตะคงอยู่ · ครบ AGREE = สร้างโน้ต · DRAFT ค้าง = `blm update draft ["topic"]` · gen HTML ใหม่จากไฟล์: `blm update --html --from <json>`
- ข้อจำกัดที่รู้: ความครอบคลุมนับเป็นโฟลเดอร์ชั้นสอง (`src/lib` ทั้งก้อน) — ส่วน "changed since" คือตัวชี้ว่ากฎเดิมอาจเก่า ไม่ใช่ตัวตัดสิน

## /blm_init

- โปรเจ็คใหม่: สำรวจ (`blm_scan`, `blm_graph`, `blm_search`) → เสนอหัวข้อหลัก → เจ้าของยืนยัน → ความหมาย → ยืนยัน → หัวข้อย่อยต่อหัวข้อ → blm.md
- มี blm.md อยู่แล้ว = โหมดปรับปรุง: ไม่เขียนทับ ยื่นทีละ block ผ่าน `blm_conflict` (ลงโน้ตหัวข้อเองเมื่อหัวข้อแยกไฟล์แล้ว) · หัวข้อหลักใหม่ = `blm_create {name:"blm-<slug>", folder:<topicFolder>, description:<cue เดียวกับตาราง>}` แล้ว `blm_patch` เพิ่มแถว `| [Topic](<topicFolder>/blm-<slug>.md) | cue |` ในตาราง · ร่างที่ยาวให้แยกเป็นโน้ตส่วนประกอบ (`blm_create` เช่น `custom-vpn-rules`) แล้ว ref จาก block

## สถิติ (แบบ rtk gain — ใน `blm status`)

- `blm` ลง read เอง (`trigger:"user"` เมื่อเจ้าของสั่ง) · `blm_report` ลง check เอง · `blm_stat {event:"lookup", query}` เมื่อเจ้าของถามแล้วคุณค้นกฎมาตอบ · `override` เมื่อเจ้าของเปลี่ยนกฎ · `fixed` เมื่อกฎพากลับมาถูก
