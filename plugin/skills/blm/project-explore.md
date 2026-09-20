ไทย · **[English](project-explore-EN.md)**

# สำรวจโปรเจ็ค — scan · graph · grep/cat · tools

- `blm_scan {path?}` ขนาด/token/โปรเจ็คย่อย (candidate หัวข้อหลักตอน /blm_init) และ ignore รวม 3 ไฟล์ (`.gitignore` `.socraticodeignore` `.ignorememory`)
- `blm_graph {query?, rebuild?, html?}` กราฟ import + symbol + call · แหล่งข้อมูลตามลำดับ: SocratiCode ใน Qdrant (`<projectId>_symgraph_file`, `socraticode_metadata`) → ast-grep (tree-sitter) → regex — ดูป้าย `engine` ในผล · markdown pass หา doc hubs · งาน local ขนานทุกคอร์ (`BLM_WORKERS` override) · cache `<store>/graph.json` · `html:true` เขียน `graph.html` เปิดในเบราว์เซอร์
- `blm_grep {terms, file?, path?, maxLine?, maxResult?, ext?, sc?, imports?}` ค้นคำตรง case-insensitive หลายคำ snippet ตัดตามบรรทัดว่าง · บรรทัด import/export ถูกตัดออกเป็นค่าเริ่มต้น (`imports:true` เอาคืน) · `sc:true` ถาม `blm_search` หา candidate files ก่อน · ผลเก็บ `<store>/tmp/grep-result-<id>.json` แล้ว `blm_cat {grepId, resultId?, context?}` เปิดบริบทรอบ hit ด้วยเลขบรรทัดจริง — ใช้เมื่อรู้ **คำ** ที่ต้องหา; ถ้ารู้แค่ **ความหมาย** ใช้ [blm-search.md](blm-search.md)
- `blm_tools {action, tool, …}` เครื่องมือข้างเคียง `socraticode | obsidian | graphify | tree-sitter | embedding`: `status · get · install · update · start · stop · restart · gen-graph` — รายละเอียด socraticode ใน [socraticode-remote.md](socraticode-remote.md) · tree-sitter = ast-grep ให้ `blm_graph` มี AST จริงเมื่อไม่มี socraticode · ถ้า tool คืนวิธี/คำสั่งให้ผู้ใช้รันเอง ให้พิมพ์ตามนั้น ห้ามรันแทนโดยไม่ถาม
- `blm_status` / `blm status` ทุกอย่างในหน้าเดียว: READY/NOT READY บรรทัดแรก, paths, กฎ, โน้ต edited รอ push, conflicts, tools, สถิติ, Workers
