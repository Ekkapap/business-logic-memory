package blm

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// blm.md เป็นสารบัญ (เจ้าของ 2026-09-20): ตาราง `## Main Business` แต่ละแถว = หัวข้อหลัก + cue ≤150 ตัวอักษร + **ลิงก์ไปโน้ตหัวข้อ**
// ที่ถือ block กฎของหัวข้อนั้นทั้งหมด (ชื่อ `blm-<topic>` folder `global/conventions/blm` — ชื่อโน้ตไม่ชนโน้ตอื่น และ `blm grep blm-` เห็นครบ)
// ไฟล์เดียว 226 บรรทัดที่ทุกอย่างรวมกันจึงกลายเป็น index + ไฟล์ละหัวข้อ: core brief = blm.md ทั้งไฟล์พอดี, `blm {query}` โหลดเฉพาะ
// หัวข้อที่ถาม, conflict/diff เป็นรายหัวข้อ · โน้ตที่ยังไม่แยก (กฎอยู่ใต้ ## ใน blm.md เอง) ยังทำงานเหมือนเดิม — สองแบบอยู่ร่วมกันได้
//
// แถวตาราง: `| [Authentication](global/conventions/blm/blm-authentication.md) | cue |` — path นับจาก memory root (Config.MemoryRoot:
// mirror ของ agentsroom · `.claude/memory` ของ backend อื่น) โฟลเดอร์ = Config.TopicFolder (ตั้งได้ใน blm.json / env BLM_TOPICS_FOLDER)
// ที่อ่านจริง: store ก่อน (`<store>/<name>.md` = ร่างที่แก้ล่าสุด) แล้ว memory root ตามลิงก์ แล้ว mirror ด้วยชื่อ — เหมือน RulesPath ของ blm.md เอง

// ป้าย [Conflict](…) ที่ conflict mark วางหน้าลิงก์ (ข้อเสนอค้างในหัวข้อนั้น) ต้องไม่ทำให้แถวหาย
var topicRowRe = regexp.MustCompile(`(?m)^\|\s*(?:\[Conflict\]\([^)]*\)\s*)?\[([^\]]+)\]\(([^)]+)\)\s*\|`)

// TopicNote หัวข้อหลักที่มีโน้ตแยก
type TopicNote struct {
	Topic string // ชื่อที่แสดง (label ของลิงก์)
	Name  string // ชื่อโน้ต = basename ของลิงก์ตัด .md
	Path  string // ไฟล์ที่อ่านจริง (store หรือ mirror) · "" = ยังไม่มีทั้งสองที่ (ลิงก์ค้าง)
}

// topicNotes ดึงลิงก์จากตาราง Main Business ใน body ของ blm.md
func (s *Store) topicNotes(body string) []TopicNote {
	var out []TopicNote
	seen := map[string]bool{}
	for _, m := range topicRowRe.FindAllStringSubmatch(body, -1) {
		label, link := strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
		if strings.Contains(link, "://") || !strings.HasSuffix(link, ".md") {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(link), ".md")
		if seen[name] {
			continue
		}
		seen[name] = true
		t := TopicNote{Topic: label, Name: name}
		if s.Has(name) {
			t.Path = filepath.Join(s.Dir, name+".md")
		} else if p := filepath.Join(s.Root, filepath.FromSlash(s.Config.MemoryRoot()), filepath.FromSlash(link)); exists(p) {
			t.Path = p
		} else if s.HasMirror() {
			if folder, ok := s.FindTargetFolder(name); ok {
				t.Path = filepath.Join(s.MirrorDir, filepath.FromSlash(folder), name+".md")
			}
		}
		out = append(out, t)
	}
	return out
}

// topicNoteFor — โน้ตหัวข้อของ label ในตาราง Main Business ("" = หัวข้อนี้ยังอยู่ใน blm.md เอง)
func (s *Store) topicNoteFor(body, topic string) (TopicNote, bool) {
	for _, t := range s.topicNotes(body) {
		if strings.TrimSpace(t.Topic) == strings.TrimSpace(topic) && t.Path != "" {
			return t, true
		}
	}
	return TopicNote{}, false
}

// topicBlocks — block กฎจากโน้ตหัวข้อทุกตัวที่ blm.md ลิงก์ถึง (Topic = label ในตาราง) ต่อท้าย block ของ blm.md เอง
// โน้ตที่ลิงก์ค้าง (ยังไม่มีไฟล์) คืนเป็น block เดียวที่บอกว่าหาย ให้ /blm รายงานได้แทนที่จะเงียบ
func (s *Store) topicBlocks(body string) []RuleBlock {
	var blocks []RuleBlock
	for _, t := range s.topicNotes(body) {
		if t.Path == "" {
			blocks = append(blocks, RuleBlock{Topic: t.Topic, Heading: t.Name + " (missing)", Text: "## " + t.Name + " (missing)\nlinked from blm.md Main Business but not found in the store or the mirror — blm_create it or fix the link\n"})
			continue
		}
		raw, err := os.ReadFile(t.Path)
		if err != nil {
			continue
		}
		_, off, tb := splitFront(string(raw))
		src := s.rel(t.Path)
		for _, b := range SplitBlocks(tb, off, src, t.Path) {
			if b.Heading == "" && strings.TrimSpace(b.Text) == "" {
				continue
			}
			if b.Topic == "" { // โน้ตหัวข้อไม่จำเป็นต้องมี `# ` — ใช้ชื่อหัวข้อจากตาราง
				b.Topic = t.Topic
			}
			blocks = append(blocks, b)
		}
	}
	return blocks
}
