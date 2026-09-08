package blm

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Conflict workflow (เจ้าของกำหนด 2026-09-09): agent ตัดสินไม่ได้และเจ้าของยังไม่ตอบ (เช่น สั่งงานยาวแล้วไปนอน)
// → เขียนสถานะ conflict ไว้ในร่าง blm.md ที่หัวข้อย่อย `[Conflict: #1, #2]` และออกรายงานต่อบริเวณที่ชน
//   <store>/conflicts/[wait] <heading สั้น>-<yyyymmddHHMMSS>.md  (สถานะนำหน้าชื่อ อ่านง่ายในไฟล์ทรี · id อยู่ใน frontmatter · เจ้าของกำหนดรูปแบบ 2026-09-09)
//   แต่ละรายงานมี block A (cloud) และ block B (ร่าง) แยกกัน มี checkbox markdown ให้เลือก แก้ข้อความใน block ก่อนติ๊กได้
// → เจ้าของติ๊กแล้วสั่ง resolve: ประกอบร่างจากผล 3-way ที่เก็บไว้ (marker ต่อบริเวณ) ด้วย block ที่เลือก เลื่อน base ตาม cloud
//   เปลี่ยนชื่อรายงานเป็น [done] … ลบป้ายออกจากหัวข้อ

type ConflictReport struct {
	ID       int    `json:"id"`
	File     string `json:"file"`
	Status   string `json:"status"` // wait | done
	Draft    string `json:"draft"`
	Topic    string `json:"topic"`
	Heading  string `json:"heading"`
	Chosen   string `json:"chosen,omitempty"` // A | B | "" (ยังไม่เลือก) | "both" (ติ๊กสองช่อง = ผิด)
	Created  string `json:"created"`
	Resolved string `json:"resolved,omitempty"`
	// NoteHeading หัวข้อในโน้ตที่ชน (บรรทัด # / ## ก่อนบริเวณที่ชน) — ป้ายติดที่นี่ด้วย เพื่อบอกว่าแก้ตรงไหนของโน้ต
	NoteHeading string `json:"noteHeading,omitempty"`
}

func (s *Store) conflictsDir() string { return filepath.Join(s.Dir, "conflicts") }

var (
	conflictFileRe = regexp.MustCompile(`^\[(wait|done)\] (.+)\.md$`)
	// ป้ายเป็นลิงก์คลิกได้ไปที่รายงาน (เจ้าของ 2026-09-09: "#1 คลิกไม่ได้") · regex ครอบทั้งรูปเก่า [Conflict: #1, #2] และรูปลิงก์ ติดกันหลายอัน
	// รูปที่ต้องจับ: `[Conflict: #1, #2]` (เก่า) · `[Conflict: #1](url)` (เก่า) · `[Conflict: [#1](<url>), [#2](<url>)]` (ปัจจุบัน: เลขแต่ละตัวเป็นลิงก์ของตัวเอง วงเล็บนอกเห็นบนหน้าจอ)
	conflictTagRe = regexp.MustCompile(`(\s*\[Conflict(?:: (?:\[#\d+\](?:\(<[^>]*>\)|\([^)]*\))(?:, )?|[^\]])*)?\](?:\(<[^>]*>\)|\([^)]*\))?)+`)
	checkedRe     = regexp.MustCompile(`(?m)^- \[[xX]\] `)
)

// ListConflicts อ่านรายงานทั้งหมด (wait ก่อน done) — ใช้ใน blm_conflicts และแนบใน blm (Rules)
func (s *Store) ListConflicts() []ConflictReport {
	entries, _ := os.ReadDir(s.conflictsDir())
	var out []ConflictReport
	for _, e := range entries {
		m := conflictFileRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.conflictsDir(), e.Name()))
		if err != nil {
			continue
		}
		meta, _, body := splitFront(string(raw))
		id, _ := strconv.Atoi(meta["id"])
		r := ConflictReport{ID: id, File: s.rel(filepath.Join(s.conflictsDir(), e.Name())), Status: m[1], Draft: meta["draft"], Topic: meta["topic"], Heading: meta["subtopic"], Created: meta["created"], Resolved: meta["resolved"], NoteHeading: meta["noteHeading"]}
		r.Chosen = chosenBlock(body)
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Status != out[j].Status {
			return out[i].Status == "wait"
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// chosenBlock ดูว่า block ไหนถูกติ๊ก (A/B) — ติ๊กทั้งคู่หรือไม่ติ๊ก = ""
func chosenBlock(body string) string {
	// รูปแบบใหม่: สองช่องติ๊กอยู่ใต้ "## Decide" (keep A / keep B) · รูปแบบเก่า: ช่องติ๊กอยู่ในหัว "## Block A/B"
	var ca, cb bool
	if dec := blockSection(body, "ตัดสิน"); dec != "" {
		for _, l := range strings.Split(dec, "\n") {
			if !checkedRe.MatchString(l) {
				continue
			}
			switch {
			case strings.Contains(l, "**A**"):
				ca = true
			case strings.Contains(l, "**B**"):
				cb = true
			}
		}
	} else {
		a, b := blockSection(body, "A"), blockSection(body, "B")
		ca, cb = checkedRe.MatchString(a), checkedRe.MatchString(b)
	}
	switch {
	case ca && cb:
		return "both"
	case ca:
		return "A"
	case cb:
		return "B"
	}
	return ""
}

// blockSection ตัดส่วนของหัวข้อ: "### A — cloud"/"### B — draft" (ใหม่), "## Decide", หรือ "## Block A/B" (เก่า) ถึงหัวข้อระดับเดียวกันถัดไป
func blockSection(body, which string) string {
	// รูปปัจจุบัน (เจ้าของออกแบบ 2026-09-09): บรรทัดหัว `***<<<<<<< Current …***` แล้วเนื้อหา B จนถึง `---` · `***>>>>>>> Incoming …***` แล้วเนื้อหา A จนถึง `---`
	// รองรับรูปก่อนหน้า (marker ต้นบรรทัดไม่มี *** · `=======` คั่น · `\>\>…` escape) และไฟล์ที่เจ้าของแก้มือโดยไม่มีบรรทัดว่างหลังหัว
	if which == "A" || which == "B" {
		if txt, ok := gitBlock(body, which); ok {
			return "```text\n" + txt + "\n```"
		}
	}
	for _, h := range []string{"### " + which + " —", "## " + which, "## Block " + which} {
		start := strings.Index(body, h)
		if start < 0 {
			continue
		}
		rest := body[start+len(h):]
		level := "\n## "
		if strings.HasPrefix(h, "### ") {
			level = "\n### "
		}
		if end := strings.Index(rest, level); end >= 0 {
			rest = rest[:end]
		}
		if level == "\n### " {
			if end := strings.Index(rest, "\n## "); end >= 0 {
				rest = rest[:end]
			}
		}
		return rest
	}
	return ""
}

func isMarker(l, which string) bool {
	l = strings.Trim(strings.TrimSpace(l), "*")
	l = strings.ReplaceAll(l, "\\>", ">")
	if which == "B" {
		return strings.HasPrefix(l, "<<<<<<< Current")
	}
	return strings.HasPrefix(l, ">>>>>>> Incoming")
}

// gitBlock เนื้อหาของฝั่ง which (B = Current, A = Incoming) จากส่วนเทียบแบบ git · จบที่ `---`, marker ถัดไป, `=======` หรือหัวข้อ `## `
func gitBlock(body, which string) (string, bool) {
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		if !isMarker(l, which) {
			continue
		}
		if which == "A" {
			// รูปก่อนหน้า: A อยู่ระหว่าง `=======` กับ marker Incoming (ก่อน marker) ไม่ใช่หลัง
			for j := i - 1; j >= 0; j-- {
				if isMarker(lines[j], "B") {
					break
				}
				if strings.TrimSpace(lines[j]) == "=======" {
					return strings.TrimSpace(strings.Join(lines[j+1:i], "\n")), true
				}
			}
		}
		var out []string
		for _, m := range lines[i+1:] {
			t := strings.TrimSpace(m)
			if t == "---" || t == "=======" || strings.HasPrefix(t, "## ") || isMarker(m, "A") || isMarker(m, "B") {
				break
			}
			out = append(out, m)
		}
		return strings.TrimSpace(strings.Join(out, "\n")), true
	}
	return "", false
}

// blockText ดึงข้อความในรั้ว ```text … ``` ของ block (ที่เจ้าของอาจแก้แล้ว)
func blockText(section string) string {
	start := strings.Index(section, "```text\n")
	if start < 0 {
		return ""
	}
	rest := section[start+len("```text\n"):]
	end := strings.LastIndex(rest, "\n```")
	if end < 0 {
		return strings.TrimRight(rest, "\n")
	}
	return rest[:end]
}

func slug(s string) string {
	s = strings.ToLower(s)
	s = regexp.MustCompile(`[^a-z0-9ก-๙]+`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len([]rune(s)) > 24 {
		// ตัดที่ขอบคำ ไม่ให้เหลือเศษอย่าง "graph-rea" (เจ้าของถาม 2026-09-09)
		cut := string([]rune(s)[:24])
		if i := strings.LastIndex(cut, "-"); i > 8 {
			cut = cut[:i]
		}
		s = strings.Trim(cut, "-")
	}
	if s == "" {
		s = "conflict"
	}
	return s
}

// OpenConflict สร้างรายงาน conflict สำหรับร่าง replace ที่ชนกับ cloud: หนึ่งไฟล์ต่อบริเวณที่ชน + เก็บผล 3-way (มี marker) ไว้ประกอบกลับ
// และเติมป้าย `[Conflict: #n, …]` ที่หัวข้อย่อยในร่าง
func (s *Store) OpenConflict(name, topic, heading, reason string) ([]ConflictReport, error) {
	draft, err := s.Get(name)
	if err != nil {
		return nil, err
	}
	if draft.Mode != "replace" {
		return nil, fmt.Errorf("conflict %q: only replace drafts can conflict", name)
	}
	folder, ok := s.FindTargetFolder(draft.Target)
	if !ok {
		return nil, fmt.Errorf("conflict %q: target not in mirror", name)
	}
	raw, _ := os.ReadFile(filepath.Join(s.MirrorDir, folder, draft.Target+".md"))
	cloud := parseNote(draft.Target, string(raw))
	base, _ := os.ReadFile(filepath.Join(s.Dir, ".base", draft.Target+".md"))
	merged, n := ThreeWay(string(base), cloud.Content, draft.Content)
	if n == 0 {
		return nil, fmt.Errorf("conflict %q: no overlapping regions — blm_merge {keep:\"mine\"} can merge this without asking", name)
	}
	if err := os.MkdirAll(s.conflictsDir(), 0o755); err != nil {
		return nil, err
	}
	// เลข #n = ลำดับในรายการที่ค้างอยู่ตอนนี้ ไม่ใช่เลขรันตลอดชีพ (เจ้าของ 2026-09-09) → ใช้เลขว่างต่ำสุดจากรายงานที่ยัง wait · done ไม่จองเลข
	used := map[int]bool{}
	for _, c := range s.ListConflicts() {
		if c.Status != "wait" {
			continue
		}
		if c.Draft == name {
			_ = os.Remove(filepath.Join(s.Root, c.File)) // ยื่นซ้ำสำหรับร่างเดิม = แทนรายงานเก่าที่ยังไม่ได้ติ๊ก
			continue
		}
		used[c.ID] = true
	}
	next := 1
	for used[next] {
		next++
	}
	now := time.Now().UTC().Format(time.RFC3339)
	// แยกบริเวณจาก marker แล้วแทนด้วย marker ที่มีเลข id เพื่อประกอบกลับตอน resolve
	var reports []ConflictReport
	var ids []string
	lines := strings.Split(merged, "\n")
	var out []string
	lastHead := "" // หัวข้อล่าสุดในโน้ตก่อนถึงบริเวณที่ชน → ป้ายในโน้ตติดที่บรรทัดนี้
	noteHeads := map[string][]string{}
	var headOrder []string
	for i := 0; i < len(lines); i++ {
		if lines[i] != "<<<<<<< cloud" {
			out = append(out, lines[i])
			if strings.HasPrefix(lines[i], "# ") || strings.HasPrefix(lines[i], "## ") {
				lastHead = conflictTagRe.ReplaceAllString(lines[i], "")
			}
			continue
		}
		var a, b []string
		i++
		for ; i < len(lines) && lines[i] != "======="; i++ {
			a = append(a, lines[i])
		}
		i++
		for ; i < len(lines) && lines[i] != ">>>>>>> mine"; i++ {
			b = append(b, lines[i])
		}
		id := next
		used[id] = true
		for used[next] {
			next++
		}
		ids = append(ids, "#"+strconv.Itoa(id))
		out = append(out, fmt.Sprintf("<<<<<<< #%d >>>>>>>", id))
		file := filepath.Join(s.conflictsDir(), fmt.Sprintf("[wait] %s-%s.md", slug(heading), time.Now().Format("20060102150405")))
		if _, ok := noteHeads[lastHead]; !ok {
			headOrder = append(headOrder, lastHead)
		}
		noteHeads[lastHead] = append(noteHeads[lastHead], tagFor(len(noteHeads[lastHead])+1, file))
		body := fmt.Sprintf(`---
id: %d
draft: %s
target: %s
topic: %s
subtopic: %s
noteHeading: %s
status: wait
created: %s
cloudUpdatedAt: %s
---

# ความขัดแย้ง #%d — %s › %s

- **โน้ตที่ชน:** %s (AgentsRoom: %s › %s) ตรงหัวข้อ "%s"
- **เรื่องใน blm.md:** %s › %s

**ทำไม agent ตัดสินเองไม่ได้:** %s

## เทียบสองฝั่ง (แบบ git — แก้ข้อความในฝั่งที่จะเก็บได้เลย)

---

***<<<<<<< Current — ร่างในเครื่อง (B, แก้ใน session นี้)***

%s

---

***>>>>>>> Incoming — cloud (A, AgentsRoom แก้ล่าสุด %s)***

%s

---

## ตัดสิน

ติ๊ก **หนึ่งช่อง** เท่านั้น แล้วบอก agent ว่า "resolve" (หรือรัน "blm resolve %s") — โน้ตถูกอัปเดตทันที ป้าย #%d หายไป

- [ ] เอา Current — ร่างในเครื่อง (**B**)
- [ ] เอา Incoming — cloud (**A**)
`, id, name, draft.Target, topic, heading, lastHead, now, cloud.UpdatedAt, id, topic, heading, s.rel(filepath.Join(s.Dir, name+".md")), folder, draft.Target, strings.TrimPrefix(strings.TrimPrefix(lastHead, "## "), "# "), topic, heading, strings.TrimSpace(reason), strings.Join(b, "\n"), cloud.UpdatedAt, strings.Join(a, "\n"), name, id)
		if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
			return nil, err
		}
		reports = append(reports, ConflictReport{ID: id, File: s.rel(file), Status: "wait", Draft: name, Topic: topic, Heading: heading, Created: now, NoteHeading: lastHead})
	}
	// ผล 3-way ที่มี marker เลข id (ใช้ประกอบกลับ) + base เลื่อนไปที่ cloud ปัจจุบันตอน resolve
	_ = os.WriteFile(filepath.Join(s.conflictsDir(), name+".merged.md"), []byte("cloudUpdatedAt: "+cloud.UpdatedAt+"\n---\n"+strings.Join(out, "\n")), 0o644)
	// ป้ายสองที่ (เจ้าของ 2026-09-09): ในโน้ตที่ชน ตรงหัวข้อของบริเวณนั้น (บอกว่าแก้ตรงไหน) และใน blm.md ตรง main/sub topic (บอกว่าชนเรื่องอะไรของ business logic)
	if name != RulesNote {
		tagged := conflictTagRe.ReplaceAllString(draft.Content, "")
		for _, h := range headOrder {
			tagged = tagLine(tagged, h, noteHeads[h])
		}
		if _, err := s.save(Input{Name: name, Content: tagged, HasContent: true}, "conflict-tag"); err != nil {
			return nil, err
		}
	}
	s.refreshRuleTags()
	return reports, nil
}

// tagFor = `[Conflict](<conflicts/[wait] report.md>)` ลิงก์ไปรายงาน relative จากโน้ตใน store (เจ้าของ 2026-09-09: ไม่ต้องมีเลข)
func tagFor(_ int, reportPath string) string {
	return "[Conflict](<conflicts/" + filepath.Base(reportPath) + ">)"
}

// tagLine ติดป้ายที่บรรทัดหัวข้อที่ตรงกับ head (เทียบหลังตัดป้ายเดิม) — ใช้กับหัวข้อในโน้ตที่ชน
func tagLine(content, head string, ids []string) string {
	if head == "" || len(ids) == 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		if conflictTagRe.ReplaceAllString(l, "") == head {
			lines[i] = conflictTagRe.ReplaceAllString(l, "") + " " + strings.Join(ids, " ")
		}
	}
	return strings.Join(lines, "\n")
}

// refreshRuleTags เขียนป้ายใน blm.md ใหม่ทั้งไฟล์จากรายงานที่ยัง wait: ตัดป้ายเก่าทุกอัน แล้วติด `[Conflict: #a, #b]`
// ที่บรรทัด `# <topic>` และ `## <heading>` ของทุกกลุ่ม → idempotent เรียกซ้ำได้ทั้งตอนยื่นและตอน resolve
func (s *Store) refreshRuleTags() {
	if s.RulesPath() == "" {
		return
	}
	rules, err := s.Get(RulesNote)
	if err != nil {
		return
	}
	// ล้างของเดิมทั้งหมด (ป้ายที่หัวข้อแบบเก่า + [Conflict](…) หน้าลิงก์) แล้วสร้างใหม่จากรายงานที่ยัง wait
	content := conflictTagRe.ReplaceAllString(rules.Content, "")
	content = conflictPrefixRe.ReplaceAllString(content, "")
	content = s.linkMemoryLines(content)
	for _, c := range s.ListConflicts() {
		if c.Status != "wait" {
			continue
		}
		link := "[Conflict](<conflicts/" + filepath.Base(c.File) + ">)"
		if c.Draft == RulesNote {
			content = tagLine(content, c.NoteHeading, []string{link})
			continue
		}
		content = s.markMemory(content, c.Topic, c.Heading, c.Draft, link)
	}
	if content == rules.Content {
		return
	}
	_, _ = s.save(Input{Name: RulesNote, Content: content, HasContent: true}, "conflict-tag")
}

// memoryTokenRe ดึงชื่อโน้ตจาก token ในบรรทัด memory: ทั้งรูป `name`, `name ?`, `[name.md](<path>)`
var (
	memoryTokenRe    = regexp.MustCompile(`\[([^\]]+)\.md\]`)
	conflictPrefixRe = regexp.MustCompile(`\[Conflict\]\(<[^>]*>\)\s*`)
)

func memoryNames(line string) []string {
	var names []string
	for _, tok := range strings.Split(strings.TrimPrefix(line, "memory:"), ",") {
		tok = strings.TrimSpace(conflictPrefixRe.ReplaceAllString(tok, ""))
		if tok == "" {
			continue
		}
		if m := memoryTokenRe.FindStringSubmatch(tok); m != nil {
			names = append(names, m[1])
			continue
		}
		names = append(names, strings.Fields(tok)[0])
	}
	return names
}

// noteLink ลิงก์ไปไฟล์โน้ต relative จาก store: สำเนา local ถ้ามี ไม่มีก็ mirror · ไม่พบที่ไหน = ชื่อเปล่า
func (s *Store) noteLink(name string) string {
	if s.Has(name) {
		return "[" + name + ".md](<" + name + ".md>)"
	}
	if folder, ok := s.FindTargetFolder(name); ok {
		if rel, err := filepath.Rel(s.Dir, filepath.Join(s.MirrorDir, folder, name+".md")); err == nil {
			return "[" + name + ".md](<" + filepath.ToSlash(rel) + ">)"
		}
	}
	return name
}

// linkMemoryLines ทุกบรรทัด `memory:` ใน blm.md → ลิงก์ไฟล์ทุกโน้ตที่เกี่ยวข้อง (เจ้าของ 2026-09-09: topic สรุปมาจากหลายไฟล์ ควรคลิกไปได้ทุกไฟล์)
func (s *Store) linkMemoryLines(content string) string {
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		if !strings.HasPrefix(l, "memory:") {
			continue
		}
		var out []string
		for _, tok := range strings.Split(strings.TrimPrefix(l, "memory:"), ",") {
			tok = strings.TrimSpace(conflictPrefixRe.ReplaceAllString(tok, ""))
			if tok == "" {
				continue
			}
			// แปลงเฉพาะ token ที่เป็นชื่อโน้ตจริง (มีใน store หรือ mirror) ข้อความอื่นคงเดิม
			if names := memoryNames("memory: " + tok); len(names) == 1 && (s.Has(names[0]) || s.inMirror(names[0])) {
				out = append(out, s.noteLink(names[0]))
				continue
			}
			out = append(out, tok)
		}
		lines[i] = "memory: " + strings.Join(out, ", ")
	}
	return strings.Join(lines, "\n")
}

func (s *Store) inMirror(name string) bool {
	_, ok := s.FindTargetFolder(name)
	return ok
}

// markMemory ใส่ `[Conflict](<report>)` หน้าลิงก์ของโน้ต note ในบรรทัด memory: ของ topic › heading · ไม่มีชื่อนั้นในบรรทัด = ต่อท้ายให้
func (s *Store) markMemory(content, topic, heading, note, link string) string {
	lines := strings.Split(content, "\n")
	inTopic, inHead := false, false
	for i, l := range lines {
		clean := conflictTagRe.ReplaceAllString(l, "")
		switch {
		case strings.HasPrefix(clean, "# "):
			inTopic = strings.TrimSpace(strings.TrimPrefix(clean, "# ")) == strings.TrimSpace(topic)
			inHead = false
		case strings.HasPrefix(clean, "## "):
			inHead = inTopic && strings.TrimSpace(strings.TrimPrefix(clean, "## ")) == strings.TrimSpace(heading)
		case inHead && strings.HasPrefix(l, "memory:"):
			var out []string
			found := false
			for _, tok := range strings.Split(strings.TrimPrefix(l, "memory:"), ",") {
				tok = strings.TrimSpace(tok)
				if tok == "" {
					continue
				}
				if names := memoryNames("memory: " + tok); !found && len(names) == 1 && names[0] == note {
					tok = link + " " + tok
					found = true
				}
				out = append(out, tok)
			}
			if !found {
				out = append(out, link+" "+s.noteLink(note))
			}
			lines[i] = "memory: " + strings.Join(out, ", ")
			return strings.Join(lines, "\n")
		}
	}
	return strings.Join(lines, "\n")
}

// ResolveConflicts ประกอบร่างจาก block ที่เจ้าของเลือก **ทีละรายงานที่ติ๊กแล้ว** (เจ้าของ 2026-09-09: done แล้วอัปเดต blm.md ทันที ป้าย #เลขนั้นหาย)
// บริเวณที่ยังไม่ติ๊กใช้ข้อความฝั่งร่าง (block B) ไว้ชั่วคราวและป้ายยังค้าง · ครบทุกบริเวณ = เลื่อน base ไป cloud และลบ snapshot
func (s *Store) ResolveConflicts(name string) (map[string]any, error) {
	var mine []ConflictReport
	for _, c := range s.ListConflicts() {
		if c.Draft == name && c.Status == "wait" {
			mine = append(mine, c)
		}
	}
	if len(mine) == 0 {
		return nil, fmt.Errorf("resolve %q: no open conflicts", name)
	}
	mergedRaw, err := os.ReadFile(filepath.Join(s.conflictsDir(), name+".merged.md"))
	if err != nil {
		return nil, fmt.Errorf("resolve %q: merged snapshot missing", name)
	}
	parts := strings.SplitN(string(mergedRaw), "\n---\n", 2)
	cloudAt := strings.TrimPrefix(parts[0], "cloudUpdatedAt: ")
	snapshot := parts[len(parts)-1]
	content := snapshot
	var done, pending, both []string
	for _, c := range mine {
		marker := fmt.Sprintf("<<<<<<< #%d >>>>>>>", c.ID)
		raw, _ := os.ReadFile(filepath.Join(s.Root, c.File))
		_, _, body := splitFront(string(raw))
		switch c.Chosen {
		case "A", "B":
			text := blockText(blockSection(body, c.Chosen))
			snapshot = strings.Replace(snapshot, marker, text, 1)
			content = strings.Replace(content, marker, text, 1)
			finished := strings.Replace(string(raw), "status: wait", "status: done\nresolved: "+time.Now().UTC().Format(time.RFC3339)+"\nchosen: "+c.Chosen, 1)
			newName := strings.Replace(filepath.Base(c.File), "[wait] ", "[done] ", 1)
			_ = os.WriteFile(filepath.Join(s.conflictsDir(), newName), []byte(finished), 0o644)
			_ = os.Remove(filepath.Join(s.Root, c.File))
			done = append(done, "#"+strconv.Itoa(c.ID))
		case "both":
			both = append(both, "#"+strconv.Itoa(c.ID))
			content = strings.Replace(content, marker, blockText(blockSection(body, "B")), 1)
		default:
			pending = append(pending, "#"+strconv.Itoa(c.ID))
			content = strings.Replace(content, marker, blockText(blockSection(body, "B")), 1)
		}
	}
	remaining := append(append([]string{}, pending...), both...)
	sort.Strings(remaining)
	content = conflictTagRe.ReplaceAllString(content, "")
	if name != RulesNote { // ป้ายในโน้ตเหลือเฉพาะรายงานที่ยังไม่จบ · ถ้าโน้ตคือ blm.md เอง refreshRuleTags ท้ายสุดจัดการ
		byHead := map[string][]string{}
		var hOrder []string
		for _, c := range mine {
			tag := "#" + strconv.Itoa(c.ID)
			keep := false
			for _, r := range remaining {
				if r == tag {
					keep = true
				}
			}
			if !keep {
				continue
			}
			if _, ok := byHead[c.NoteHeading]; !ok {
				hOrder = append(hOrder, c.NoteHeading)
			}
			byHead[c.NoteHeading] = append(byHead[c.NoteHeading], tagFor(len(byHead[c.NoteHeading])+1, filepath.Join(s.Root, c.File)))
		}
		for _, h := range hOrder {
			content = tagLine(content, h, byHead[h])
		}
	}
	draft, err := s.Get(name)
	if err != nil {
		return nil, err
	}
	base := draft.Base
	if len(remaining) == 0 {
		folder, _ := s.FindTargetFolder(draft.Target)
		if raw, err := os.ReadFile(filepath.Join(s.MirrorDir, folder, draft.Target+".md")); err == nil {
			_ = os.MkdirAll(filepath.Join(s.Dir, ".base"), 0o755)
			_ = os.WriteFile(filepath.Join(s.Dir, ".base", draft.Target+".md"), []byte(parseNote(draft.Target, string(raw)).Content), 0o644)
		}
		base = cloudAt
		_ = os.Remove(filepath.Join(s.conflictsDir(), name+".merged.md"))
	} else {
		_ = os.WriteFile(filepath.Join(s.conflictsDir(), name+".merged.md"), []byte("cloudUpdatedAt: "+cloudAt+"\n---\n"+snapshot), 0o644)
	}
	n, err := s.save(Input{Name: name, Target: draft.Target, Mode: "replace", Folder: draft.Folder, Description: draft.Description, Tags: draft.Tags, Content: content, HasContent: true, Base: base}, "merge")
	if err != nil {
		return nil, err
	}
	s.refreshRuleTags() // รายงานที่ done ถูก rename แล้ว → ป้ายเลขนั้นหายจาก blm.md ทันที ที่ยัง wait คงอยู่
	res := map[string]any{"ok": len(remaining) == 0, "name": n.Name, "done": done, "pending": pending, "bothChecked": both, "base": n.Base, "bytes": len(n.Content)}
	if len(remaining) == 0 {
		res["next"] = "blm_sync {apply:true} to push"
	} else {
		res["message"] = "draft updated for the ticked blocks; still waiting on " + strings.Join(remaining, ", ") + " (tick exactly one block each)"
	}
	return res, nil
}

// RenderConflicts รายการสำหรับเทอร์มินัล: เฉพาะที่ค้าง (all=true รวมที่จบแล้ว) บรรทัดละรายการ + path เต็มบรรทัดถัดไป
// (เจ้าของ 2026-09-09: viewer ของ AgentsRoom เปิดลิงก์ .md ซ้อนไม่ได้ → ดู/ตัดสินจากเทอร์มินัล · ที่ resolve แล้วต้องหายจาก list)
func RenderConflicts(list []ConflictReport, all bool) string {
	var b strings.Builder
	wait := 0
	for _, c := range list {
		if c.Status == "wait" {
			wait++
		}
	}
	b.WriteString(Title(fmt.Sprintf("blm conflicts — %d waiting for the owner", wait)) + "\n")
	shown := 0
	for _, c := range list {
		if c.Status != "wait" && !all {
			continue
		}
		shown++
		state := Green("done · kept " + c.Chosen)
		if c.Status == "wait" {
			switch c.Chosen {
			case "":
				state = Yellow("wait · not ticked")
			case "both":
				state = Red("wait · both ticked")
			default:
				state = Yellow("wait · " + c.Chosen + " ticked → blm resolve " + c.Draft)
			}
		}
		fmt.Fprintf(&b, "\n%s  %s\n    %s  ←  note %s\n    %s\n", Cyan("#"+strconv.Itoa(c.ID)), state, c.Topic+" › "+c.Heading, c.Draft, Dim(c.File))
	}
	if shown == 0 {
		b.WriteString("\nnone\n")
	}
	b.WriteString("\n" + Dim("blm conflicts <id>  show a report · blm conflicts -i  pick and decide interactively · blm resolve <note> --keep current|incoming · --all includes done") + "\n")
	return b.String()
}

// RenderConflict รายงานหนึ่งฉบับเป็นข้อความเทอร์มินัล: ตัด frontmatter, *** และรั้ว code · marker แบบ git คงไว้
func (s *Store) RenderConflict(c ConflictReport) (string, error) {
	raw, err := os.ReadFile(filepath.Join(s.Root, c.File))
	if err != nil {
		return "", err
	}
	_, _, body := splitFront(string(raw))
	var out []string
	for _, l := range strings.Split(body, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, "```"):
			continue
		case strings.HasPrefix(t, "# "):
			out = append(out, Title(strings.TrimPrefix(t, "# ")))
		case strings.HasPrefix(t, "## "):
			out = append(out, Section(strings.TrimPrefix(t, "## ")))
		case strings.HasPrefix(t, "***") && strings.HasSuffix(t, "***"):
			out = append(out, Cyan(strings.Trim(t, "*")))
		case t == "---":
			continue
		default:
			out = append(out, strings.ReplaceAll(strings.ReplaceAll(l, "\\>", ">"), "**", ""))
		}
	}
	out = append(out, "", Dim(fmt.Sprintf("decide: blm resolve %s --keep current   (B, local draft)   |   blm resolve %s --keep incoming   (A, cloud)", c.Draft, c.Draft)))
	return strings.Join(out, "\n"), nil
}

// Keep ติ๊กช่องให้ทุกรายงานที่ยัง wait ของร่างนี้จากเทอร์มินัล: keep = current|B (ร่างในเครื่อง) หรือ incoming|A (cloud)
func (s *Store) Keep(name, keep string) error {
	which := ""
	switch strings.ToLower(keep) {
	case "current", "b", "mine", "local":
		which = "B"
	case "incoming", "a", "cloud", "theirs":
		which = "A"
	default:
		return fmt.Errorf("keep %q: use current (local draft) or incoming (cloud)", keep)
	}
	n := 0
	for _, c := range s.ListConflicts() {
		if c.Draft != name || c.Status != "wait" {
			continue
		}
		p := filepath.Join(s.Root, c.File)
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		lines := strings.Split(string(raw), "\n")
		for i, l := range lines {
			if !strings.HasPrefix(strings.TrimSpace(l), "- [") {
				continue
			}
			mark := "- [ ]"
			if strings.Contains(l, "**"+which+"**") {
				mark = "- [x]"
			}
			lines[i] = checkboxRe.ReplaceAllString(l, mark)
		}
		if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
			return err
		}
		n++
	}
	if n == 0 {
		return fmt.Errorf("keep: no open conflicts for %q", name)
	}
	return nil
}

var checkboxRe = regexp.MustCompile(`- \[[ xX]\]`)
