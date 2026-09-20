ไทย · **[English](memory-notes-EN.md)**

# จด memory ระหว่าง session — สามที่ · คำสั่งเขียน · sync

## สามที่ เรียกให้ตรง (เจ้าของกำหนดชื่อ 2026-09-09)

- **blm.md / โน้ตใน blm/** = `<store>/` (agentsroom backend: `.agentsroom/blm/`) ที่ทำงานจริง โน้ตที่แตะครั้งแรกถูกดึงจาก backend มาไว้ที่นี่พร้อม base สถานะต่อโน้ต `synced` / `edited` push แล้วไฟล์อยู่ต่อ base เลื่อน
- **mirror** = `.agentsroom/memory/` สำเนาของ AgentsRoom ที่แอปเขียนลงมา ใช้เทียบว่าใครใหม่กว่าเท่านั้น ไม่ใช่ที่แก้
- **AgentsRoom** = cloud ปลายทาง เปลี่ยนได้เฉพาะตอน push
- จำนวนไฟล์สองฝั่งไม่ต้องเท่ากัน (cloud มี knowledge/how-to ที่ไม่ใช่ business logic · blm/ มีร่าง รายงาน กราฟ) สิ่งเดียวที่ต้องมีทั้งสองที่คือ blm.md
- backend อื่น: `none` = store `.claude/blm` ไฟล์คือของจริง ไม่มี sync · `obsidian` = โฟลเดอร์ `blm/` ในห้อง vault · `custom` = คำสั่ง push/pull ที่เรียนจาก `<cli> --help` ตอน init

## เขียน memory (agent ทำเอง ไม่ต้องรอใคร)

- `blm_create {name, content, description, folder, target, mode}` โน้ตใหม่เท่านั้น ชื่อซ้ำ = ปฏิเสธ · `folder` = `features` | `features/<x>` | `global/architecture|conventions|pitfalls` (ไม่ระบุ = เดาจาก mirror) · `mode` = `append` (ต่อท้ายโน้ต backend ที่มีอยู่) | `replace` (โน้ตใหม่ทั้งใบ ต้องมี description)
- `blm_append {name, content}` ต่อท้าย · `blm_patch {name, find, replace}` ค้นหา/แทนที่ พบพอดี 1 — ทั้งสองดึงโน้ตจาก mirror มาไว้ใน blm/ ให้เองถ้ายังไม่มี
- `blm_replace {name, content, confirm}` ทับทั้งไฟล์ ต้องมีอยู่แล้ว ต่างมาก (ขนาด >30% หรือบรรทัดเดิมหาย >30%) blm คืน `needsConfirm` พร้อมตัวเลขและบรรทัดที่จะหาย เรียกซ้ำด้วย `confirm:true` เฉพาะเมื่อตั้งใจ **ห้ามใช้กับ blm.md**
- **ห้าม `memory_save`/`memory_get` ตรง** (เนื้อโน้ตวิ่งผ่าน context และทับทั้งก้อน) · Edit/Write/Bash เขียน blm/ ถูก deny ทุกการเขียนผ่าน blm มี history: `history/<name>-[action]-YYYYMMDD-HHmmss.md` กู้ด้วย `blm_restore {name, history}` (`blm restore <ไฟล์> <โน้ต>`; `blm restore <โน้ต>` ลิสต์)
- **จดทันทีที่พิสูจน์แล้ว** ไม่ใช่ท้ายงาน — session compact แล้วสิ่งที่อยู่แค่ในแชตหายหมด (บทเรียน 2026-09-09)
- **เจ้าของชี้ปัญหาใน blm.md** (ลิงก์ผิด ข้อความผิด) = อ่านบรรทัดนั้นจริง (`grep -n` ใน store อ่านได้) แล้ว `blm_patch` ทันที **ห้ามตอบว่า sandbox ไม่ให้แก้** ข้อจำกัดนั้นมีเฉพาะซอร์ส Go ของ blm
- ลิงก์ในโน้ต/blm.md: `[ชื่อไฟล์](path จาก root โปรเจ็ค)` ไม่ครอบด้วย `<>` escape `[ ]` ในชื่อ — `linkPath` ทำให้เองตอน checkout และ `blm conflict mark`

## sync (backend agentsroom/custom เท่านั้น)

- เจ้าของสั่ง "update memory" → `blm_sync {apply:true, author, role}` blm spawn AgentsRoom MCP เอง ยิง `memory_save` ทีละตัว ตรวจด้วย `memory_list` ครั้งเดียว โน้ตที่ตรง base ไม่ถูกส่ง ไม่ retry เอง เนื้อโน้ตไม่ผ่าน context · cloud ถูกแก้หลัง base → ไม่ push ทับ คืน `conflict:` ให้ไป `blm_diff` → `blm_merge {keep}` หรือยื่น `blm_conflict`
- `blm_sync {direction:"pull", apply:true}` เมื่อสงสัยว่า cloud เปลี่ยน: mirror สด แล้วโน้ตที่ไม่ได้แก้และ cloud ใหม่กว่า → ดึงทับ · แก้ค้าง+cloud เปลี่ยน → รายงานเป็น conflict ไม่ทับ
- `names:[…]` จำกัดเฉพาะโน้ต · `delete:[…]` ลบโน้ต backend หลัง push (เช่นชื่อเก่าหลัง rename)
- ห้ามยิง `memory_save` เองแทนขั้นตอนนี้ เว้นแต่ `blm_sync` ตอบว่าไม่พบ AgentsRoom MCP ใน `.mcp.json`
- ทางแก้เมื่อร่าง `replace` ชน cloud ที่ใหม่กว่า โดยไม่ต้องส่งทั้งโน้ตผ่าน context: `blm_merge keep=cloud` → `blm_sync pull apply` → `blm_append` ใหม่ → push
