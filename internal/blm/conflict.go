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
}

func (s *Store) conflictsDir() string { return filepath.Join(s.Dir, "conflicts") }

var (
	conflictFileRe = regexp.MustCompile(`^\[(wait|done)\] (.+)\.md$`)
	conflictTagRe  = regexp.MustCompile(`\s*\[Conflict: [^\]]*\]`)
	checkedRe      = regexp.MustCompile(`(?m)^- \[[xX]\] `)
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
		r := ConflictReport{ID: id, File: s.rel(filepath.Join(s.conflictsDir(), e.Name())), Status: m[1], Draft: meta["draft"], Topic: meta["topic"], Heading: meta["subtopic"], Created: meta["created"], Resolved: meta["resolved"]}
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

// quote แสดงบรรทัดเป็น blockquote — ใน AgentsRoom รั้ว code ไม่ตัดบรรทัด อ่านย่อหน้ายาวไม่ได้ (เจ้าของ 2026-09-09) blockquote ตัดบรรทัดตามหน้าจอ
func quote(lines []string) string {
	var out []string
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			out = append(out, ">")
			continue
		}
		out = append(out, "> "+l)
	}
	return strings.Join(out, "\n")
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
	next := 1
	for _, c := range s.ListConflicts() {
		if c.Status == "wait" && c.Draft == name {
			_ = os.Remove(filepath.Join(s.Root, c.File)) // ยื่นซ้ำสำหรับร่างเดิม = แทนรายงานเก่าที่ยังไม่ได้ติ๊ก
			continue
		}
		if c.ID >= next {
			next = c.ID + 1
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	// แยกบริเวณจาก marker แล้วแทนด้วย marker ที่มีเลข id เพื่อประกอบกลับตอน resolve
	var reports []ConflictReport
	var ids []string
	lines := strings.Split(merged, "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		if lines[i] != "<<<<<<< cloud" {
			out = append(out, lines[i])
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
		next++
		ids = append(ids, "#"+strconv.Itoa(id))
		out = append(out, fmt.Sprintf("<<<<<<< #%d >>>>>>>", id))
		file := filepath.Join(s.conflictsDir(), fmt.Sprintf("[wait] %s-%s.md", slug(heading), time.Now().Format("20060102150405")))
		// ส่วนต่างล้วน ๆ ระหว่างสอง block (เจ้าของ 2026-09-09: อ่านสอง block เต็มแล้วบอกไม่ได้ว่าอันไหนถูก) — บรรทัดที่เท่ากันไม่ต้องอ่าน
		only, _ := LineDiff(strings.Join(a, "\n"), strings.Join(b, "\n"))
		var onlyA, onlyB []string
		for _, l := range strings.Split(only, "\n") {
			switch {
			case strings.HasPrefix(l, "- "):
				onlyA = append(onlyA, strings.TrimPrefix(l, "- "))
			case strings.HasPrefix(l, "+ "):
				onlyB = append(onlyB, strings.TrimPrefix(l, "+ "))
			}
		}
		if len(onlyA) == 0 {
			onlyA = []string{"(ไม่มี — ฝั่ง A ไม่มีบรรทัดที่ B ขาด)"}
		}
		if len(onlyB) == 0 {
			onlyB = []string{"(ไม่มี — ฝั่ง B ไม่มีบรรทัดที่ A ขาด)"}
		}
		body := fmt.Sprintf(`---
id: %d
draft: %s
target: %s
topic: %s
subtopic: %s
status: wait
created: %s
cloudUpdatedAt: %s
---

# ความขัดแย้ง #%d — %s › %s

**ทำไม agent ตัดสินเองไม่ได้:** %s

## ต่างกันตรงไหน (อ่านแค่ส่วนนี้ก็พอ)

**มีเฉพาะฝั่ง A — cloud** (AgentsRoom แก้ล่าสุด %s):

%s

**มีเฉพาะฝั่ง B — ร่างในเครื่อง** (แก้ใน session นี้):

%s

ส่วนที่เหลือของบริเวณนี้เหมือนกันทั้งสองฝั่ง

## ตัดสิน

ติ๊ก **หนึ่งช่อง** เท่านั้น ถ้าอยากแก้ข้อความก่อน แก้ในรั้วของ block เต็มด้านล่างแล้วค่อยติ๊ก จากนั้นบอก agent ว่า "resolve" (หรือรัน "blm resolve %s") — โน้ตถูกอัปเดตทันที ป้าย #%d หายไป

- [ ] เอาฝั่ง **A** (cloud)
- [ ] เอาฝั่ง **B** (ร่างในเครื่อง)

## Block เต็ม (แก้ได้ก่อนติ๊ก)

### A — cloud
`+"```text\n%s\n```"+`

### B — ร่างในเครื่อง
`+"```text\n%s\n```"+`
`, id, name, draft.Target, topic, heading, now, cloud.UpdatedAt, id, topic, heading, strings.TrimSpace(reason), cloud.UpdatedAt, quote(onlyA), quote(onlyB), name, id, strings.Join(a, "\n"), strings.Join(b, "\n"))
		if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
			return nil, err
		}
		reports = append(reports, ConflictReport{ID: id, File: s.rel(file), Status: "wait", Draft: name, Topic: topic, Heading: heading, Created: now})
	}
	// ผล 3-way ที่มี marker เลข id (ใช้ประกอบกลับ) + base เลื่อนไปที่ cloud ปัจจุบันตอน resolve
	_ = os.WriteFile(filepath.Join(s.conflictsDir(), name+".merged.md"), []byte("cloudUpdatedAt: "+cloud.UpdatedAt+"\n---\n"+strings.Join(out, "\n")), 0o644)
	// ป้ายที่หัวข้อย่อยในร่าง (ตัดป้ายเก่าก่อน)
	tagged := tagHeading(draft.Content, heading, ids)
	if _, err := s.save(Input{Name: name, Target: draft.Target, Mode: "replace", Folder: draft.Folder, Description: draft.Description, Tags: draft.Tags, Content: tagged, HasContent: true, Base: draft.Base}, "conflict"); err != nil {
		return nil, err
	}
	return reports, nil
}

// tagHeading เติม/แทนป้าย [Conflict: …] ที่บรรทัด `## <heading>` (ids ว่าง = ลบป้าย)
func tagHeading(content, heading string, ids []string) string {
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		if !strings.HasPrefix(l, "## ") {
			continue
		}
		clean := conflictTagRe.ReplaceAllString(l, "")
		if strings.TrimSpace(strings.TrimPrefix(clean, "## ")) != strings.TrimSpace(heading) {
			continue
		}
		if len(ids) > 0 {
			clean += " [Conflict: " + strings.Join(ids, ", ") + "]"
		}
		lines[i] = clean
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
	heading := mine[0].Heading
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
	content = tagHeading(content, heading, remaining)
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
	res := map[string]any{"ok": len(remaining) == 0, "name": n.Name, "done": done, "pending": pending, "bothChecked": both, "base": n.Base, "bytes": len(n.Content)}
	if len(remaining) == 0 {
		res["next"] = "blm_sync {apply:true} to push"
	} else {
		res["message"] = "draft updated for the ticked blocks; still waiting on " + strings.Join(remaining, ", ") + " (tick exactly one block each)"
	}
	return res, nil
}
