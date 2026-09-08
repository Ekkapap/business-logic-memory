package blm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// RuleBlock หนึ่งหัวข้อย่อย (## …) ของ blm.md พร้อมหัวข้อหลัก (# …) ที่สังกัด
type RuleBlock struct {
	Topic   string `json:"topic"`
	Heading string `json:"heading"`
	Text    string `json:"text"`
	// Line บรรทัด (นับจาก 1) ของหัวข้อย่อยในไฟล์จริง — ใช้ทำลิงก์ `path:line` ที่คลิกเปิดได้ใน terminal
	Line   int    `json:"line"`
	Ref    string `json:"ref"`
	RefAbs string `json:"refAbs"`
	// RefShort `blm.md:line` — ผ่าน symlink สั้นข้าง store ใช้ต่อท้าย PASSED/NOT PASSED ในรายงาน
	RefShort string `json:"refShort"`
	// Rule = block ที่เป็น "กฎ" จริง (มีบรรทัด memory:/code:) — ตาราง Main Business, วิธีอ่าน, ข้อสงสัย ไม่ใช่กฎ ไม่ต้องตรวจ
	Rule      bool   `json:"rule"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

type RulesResult struct {
	Source   string      `json:"source"`
	Query    string      `json:"query,omitempty"`
	Blocks   []RuleBlock `json:"blocks"`
	Headings []string    `json:"headings"`
	// ChangedSinceLastRead block ที่ updated_at ใหม่กว่าครั้งก่อนที่อ่าน
	ChangedSinceLastRead []string `json:"changedSinceLastRead"`
	// PendingDrafts โน้ต temp ที่ยังไม่ sync แต่เล็งไฟล์นี้ — ร่างรอ confirm ไม่ใช่กฎจริง
	PendingDrafts []string `json:"pendingDrafts"`
	LastReadAt    string   `json:"lastReadAt,omitempty"`
	Message       string   `json:"message,omitempty"`
	// Conflicts รายงานที่ยังรอเจ้าของตัดสิน (สถานะ wait) — agent ต้องไม่ push ร่างนั้นจนกว่าจะ resolve
	Conflicts []ConflictReport `json:"conflicts"`
}

// RulesPath ไฟล์กฎที่ใช้งาน = สำเนา local ใน store เสมอ (concept เจ้าของ 2026-09-09: แก้ local ก่อน แล้ว sync)
// ยังไม่มีใน store แต่มีใน mirror → checkout มาก่อนครั้งแรก · "" = ยังไม่มีที่ไหนเลย
func (s *Store) RulesPath() string {
	if s.Has(RulesNote) {
		return filepath.Join(s.Dir, RulesNote+".md")
	}
	if _, ok := s.FindTargetFolder(RulesNote); ok {
		if _, err := s.Checkout(RulesNote); err == nil {
			return filepath.Join(s.Dir, RulesNote+".md")
		}
	}
	return ""
}

// Rules อ่านไฟล์กฎธุรกิจ · query ว่าง = ทั้งไฟล์ · ระบุ = grep หัวข้อ+เนื้อหา แล้วคืนทั้ง block ของหัวข้อย่อยที่ตรง
// จำเวลาที่อ่านครั้งล่าสุดไว้ใน store/.rules-read.json เพื่อชี้ block ที่เปลี่ยนตั้งแต่ครั้งก่อน
func (s *Store) Rules(query, trigger string) RulesResult {
	marker := filepath.Join(s.Dir, ".rules-read.json")
	res := RulesResult{Query: query, Blocks: []RuleBlock{}, Headings: []string{}, ChangedSinceLastRead: []string{}, PendingDrafts: []string{}, Conflicts: []ConflictReport{}}
	for _, c := range s.ListConflicts() {
		if c.Status == "wait" {
			res.Conflicts = append(res.Conflicts, c)
		}
	}
	if raw, err := os.ReadFile(marker); err == nil {
		var m struct{ At string }
		_ = json.Unmarshal(raw, &m)
		res.LastReadAt = m.At
	}
	if s.HasMirror() {
		for _, n := range s.List() {
			if n.Target == RulesNote && n.Dirty {
				res.PendingDrafts = append(res.PendingDrafts, n.Name)
			}
		}
	}
	path := s.RulesPath()
	if path == "" {
		res.Message = "no rules file blm.md yet — run /blm_init to analyse the project and draft it, or if a draft exists in temp have the owner review it and blm_sync first"
		return res
	}
	s.ensureShortLink(path)
	raw, err := os.ReadFile(path)
	if err != nil {
		res.Message = err.Error()
		return res
	}
	_, offset, body := splitFront(string(raw))
	res.Source = s.rel(path)
	if s.HasMirror() && len(res.PendingDrafts) > 0 {
		res.Message = "local edits in " + res.Source + " not synced to the backend yet (blm_sync {apply:true} when the owner confirms)"
	}
	all := SplitBlocks(body, offset, res.Source, path)
	q := strings.ToLower(strings.TrimSpace(query))
	for _, b := range all {
		res.Headings = append(res.Headings, b.Topic+" › "+b.Heading)
		if q == "" || strings.Contains(strings.ToLower(b.Heading), q) || strings.Contains(strings.ToLower(b.Text), q) {
			res.Blocks = append(res.Blocks, b)
		}
		if res.LastReadAt != "" && b.UpdatedAt != "" && b.UpdatedAt > res.LastReadAt {
			res.ChangedSinceLastRead = append(res.ChangedSinceLastRead, b.Heading)
		}
	}
	if q != "" && len(res.Blocks) == 0 {
		res.Message = "no heading or text matches " + strings.TrimSpace(query) + " — pick one of headings and search again"
	}
	_ = os.MkdirAll(s.Dir, 0o755)
	m, _ := json.Marshal(map[string]string{"At": time.Now().UTC().Format(time.RFC3339)})
	_ = os.WriteFile(marker, m, 0o644)
	s.RecordStat(map[string]any{"event": "read", "query": query, "blocks": len(res.Blocks), "trigger": trigger})
	return res
}

// ensureShortLink symlink สั้น `<parent ของ store>/blm.md` → ไฟล์กฎจริง ให้รายงานพิมพ์ `blm.md:20`
// แล้วคลิกเปิดได้ใน terminal ของ AgentsRoom โดยไม่โชว์ path ยาว (เจ้าของทดสอบ 2026-09-08 ว่าหาไฟล์จากชื่อสั้นผ่าน symlink ได้)
// ชี้ใหม่ทุกครั้งที่อ่าน จึงไม่มีวันชี้ผิดไฟล์ · ระบบไฟล์ที่ไม่ให้ symlink (Windows ไม่มีสิทธิ์) ข้ามเงียบ ๆ รายงานยังใช้ ref เต็มได้
func (s *Store) ensureShortLink(target string) {
	link := filepath.Join(filepath.Dir(s.Dir), RulesNote+".md")
	if filepath.Clean(link) == filepath.Clean(target) {
		return
	}
	wanted, err := filepath.Rel(filepath.Dir(link), target)
	if err != nil {
		return
	}
	if cur, err := os.Readlink(link); err == nil && cur == wanted {
		return
	}
	if fi, err := os.Lstat(link); err == nil && fi.Mode()&os.ModeSymlink == 0 {
		return // มีไฟล์จริงชื่อนี้อยู่ ไม่ทับ
	}
	_ = os.Remove(link)
	_ = os.Symlink(wanted, link)
}

var (
	updatedRe = regexp.MustCompile(`(?m)^updated_at:\s*(\S+)`)
	ruleRe    = regexp.MustCompile(`(?m)^(memory|code):`)
)

// SplitBlocks แบ่งไฟล์เป็น block ตาม `## ` โดยจำ `# ` ที่ครอบอยู่ — บรรทัดก่อน ## แรกของแต่ละ topic ติดไปกับ topic นั้นเป็น block ชื่อว่าง
// lineOffset = จำนวนบรรทัดที่อยู่ก่อน markdown นี้ในไฟล์จริง (frontmatter)
func SplitBlocks(markdown string, lineOffset int, source, sourceAbs string) []RuleBlock {
	var blocks []RuleBlock
	var cur *RuleBlock
	topic := ""
	line := lineOffset
	ref := func(n int) (string, string, string) {
		return source + ":" + itoa(n), sourceAbs + ":" + itoa(n), RulesNote + ".md:" + itoa(n)
	}
	flush := func() {
		if cur != nil && strings.TrimSpace(cur.Text) != "" {
			cur.Text = strings.TrimRight(cur.Text, "\n") + "\n"
			if m := updatedRe.FindStringSubmatch(cur.Text); m != nil {
				cur.UpdatedAt = m[1]
			}
			cur.Rule = cur.Heading != "" && ruleRe.MatchString(cur.Text)
			blocks = append(blocks, *cur)
		}
		cur = nil
	}
	for _, l := range strings.Split(markdown, "\n") {
		line++
		if strings.HasPrefix(l, "# ") {
			flush()
			topic = strings.TrimSpace(conflictTagRe.ReplaceAllString(l[2:], ""))
			continue
		}
		if strings.HasPrefix(l, "## ") {
			flush()
			r, ra, rs := ref(line)
			cur = &RuleBlock{Topic: topic, Heading: strings.TrimSpace(conflictTagRe.ReplaceAllString(l[3:], "")), Text: l + "\n", Line: line, Ref: r, RefAbs: ra, RefShort: rs}
			continue
		}
		if cur == nil {
			r, ra, rs := ref(line)
			cur = &RuleBlock{Topic: topic, Line: line, Ref: r, RefAbs: ra, RefShort: rs}
		}
		cur.Text += l + "\n"
	}
	flush()
	return blocks
}

func itoa(n int) string { return strconv.Itoa(n) }

// TopicCount นับหัวข้อหลัก (# ) และหัวข้อย่อยที่เป็นกฎ (rule) จากไฟล์กฎปัจจุบัน — สำหรับ blm status
func (s *Store) TopicCount() (topics, rules int) {
	path := s.RulesPath()
	if path == "" {
		return 0, 0
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, 0
	}
	_, off, body := splitFront(string(raw))
	seen := map[string]bool{}
	for _, b := range SplitBlocks(body, off, "", "") {
		if b.Topic != "" && !seen[b.Topic] {
			seen[b.Topic] = true
			topics++
		}
		if b.Rule {
			rules++
		}
	}
	return topics, rules
}
