package blm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// blm_diff / blm_merge — ปิดช่องโหว่ "ร่าง replace ทับการแก้บน cloud ระหว่างร่างค้าง" (ตกลงกับเจ้าของ 2026-09-08, สร้าง 2026-09-09)
// ฐานเปรียบเทียบคือสำเนา mirror ตอนสร้างร่าง (.base/<name>.md) + updatedAt ของมัน (Note.Base)
// diff/merge เกิดจากคำสั่งเจ้าของเท่านั้น ของเดิมที่ถูกแทนไป history/ เสมอ (snapshot ใน save)

type DiffResult struct {
	Name          string `json:"name"`
	Base          string `json:"base,omitempty"`
	CloudUpdated  string `json:"cloudUpdatedAt,omitempty"`
	CloudChanged  bool   `json:"cloudChanged"`
	DiffCloud     string `json:"diffCloud"`
	DiffMine      string `json:"diffMine"`
	CloudLines    int    `json:"cloudChangedLines"`
	MineLines     int    `json:"mineChangedLines"`
	Overlap       bool   `json:"overlap"`
	Message       string `json:"message,omitempty"`
	BaseAvailable bool   `json:"baseAvailable"`
}

// Diff เทียบร่าง replace กับ base และ cloud (mirror) — เนื้อโน้ตทั้งก้อนไม่ออกไป มีแค่บรรทัดที่ต่าง
func (s *Store) Diff(name string) (DiffResult, error) {
	draft, err := s.Get(name)
	if err != nil {
		return DiffResult{}, err
	}
	res := DiffResult{Name: name, Base: draft.Base}
	if draft.Mode != "replace" {
		res.Message = "append drafts never conflict — nothing to diff"
		return res, nil
	}
	base, err := os.ReadFile(filepath.Join(s.Dir, ".base", draft.Target+".md"))
	if err != nil {
		res.Message = "no base snapshot for this draft (created with blm_save, not blm_edit) — diff against cloud only"
	} else {
		res.BaseAvailable = true
	}
	cloud := ""
	if folder, ok := s.FindTargetFolder(draft.Target); ok {
		raw, _ := os.ReadFile(filepath.Join(s.MirrorDir, folder, draft.Target+".md"))
		n := parseNote(draft.Target, string(raw))
		cloud, res.CloudUpdated = n.Content, n.UpdatedAt
	}
	ref := string(base)
	if !res.BaseAvailable {
		ref = cloud
	}
	res.CloudChanged = res.BaseAvailable && res.CloudUpdated != "" && draft.Base != "" && res.CloudUpdated > draft.Base
	cloudD, cl := LineDiff(ref, cloud)
	mineD, ml := LineDiff(ref, draft.Content)
	res.DiffCloud, res.CloudLines = cloudD, cl
	res.DiffMine, res.MineLines = mineD, ml
	res.Overlap = res.CloudChanged && overlaps(ref, cloud, draft.Content)
	switch {
	case !res.CloudChanged:
		res.Message = "cloud unchanged since the draft was taken — safe to push"
	case res.Overlap:
		res.Message = "cloud AND draft changed the same region — merge by hand: blm_merge {keep:\"content\", content} after reading diffCloud/diffMine"
	default:
		res.Message = "cloud changed elsewhere — blm_merge {keep:\"mine\"} re-applies the draft on top of the cloud version, {keep:\"cloud\"} drops the draft"
	}
	return res, nil
}

// Merge ตัดสินร่าง: keep=mine (ใส่การแก้ของร่างทับ cloud ปัจจุบันแล้วเลื่อน base), cloud (ทิ้งร่าง), content (เนื้อหาที่ agent/เจ้าของรวมเอง)
func (s *Store) Merge(name, keep, content string) (map[string]any, error) {
	draft, err := s.Get(name)
	if err != nil {
		return nil, err
	}
	folder, ok := s.FindTargetFolder(draft.Target)
	if !ok {
		return nil, fmt.Errorf("merge %q: target not in mirror — nothing to merge against", name)
	}
	raw, _ := os.ReadFile(filepath.Join(s.MirrorDir, folder, draft.Target+".md"))
	cloud := parseNote(draft.Target, string(raw))
	switch keep {
	case "cloud":
		if err := s.Delete(name); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "kept": "cloud", "dropped": name}, nil
	case "mine":
		base, _ := os.ReadFile(filepath.Join(s.Dir, ".base", draft.Target+".md"))
		merged, conflicts := ThreeWay(string(base), cloud.Content, draft.Content)
		if conflicts > 0 {
			return nil, fmt.Errorf("merge %q: %d conflicting region(s) — resolve with blm_merge {keep:\"content\", content}", name, conflicts)
		}
		content = merged
	case "content":
		if strings.TrimSpace(content) == "" {
			return nil, fmt.Errorf("merge %q: keep=content needs content", name)
		}
	default:
		return nil, fmt.Errorf("keep must be mine | cloud | content")
	}
	_ = os.WriteFile(filepath.Join(s.Dir, ".base", draft.Target+".md"), []byte(cloud.Content), 0o644)
	n, err := s.save(Input{Name: name, Target: draft.Target, Mode: "replace", Folder: draft.Folder, Description: draft.Description, Tags: draft.Tags, Content: content, HasContent: true, Base: cloud.UpdatedAt}, "merge")
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "kept": keep, "name": n.Name, "base": n.Base, "bytes": len(n.Content)}, nil
}

// ---- line diff (LCS แบบ DP — โน้ตไม่เกินพันบรรทัด พอ · ponytail: ไม่ทำ Myers) ----

type op struct {
	kind byte // ' ' = same, '-' = removed from a, '+' = added in b
	text string
}

func diffOps(a, b string) []op {
	x := strings.Split(strings.TrimRight(a, "\n"), "\n")
	y := strings.Split(strings.TrimRight(b, "\n"), "\n")
	n, m := len(x), len(y)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if x[i] == y[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var out []op
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case x[i] == y[j]:
			out = append(out, op{' ', x[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			out = append(out, op{'-', x[i]})
			i++
		default:
			out = append(out, op{'+', y[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, op{'-', x[i]})
	}
	for ; j < m; j++ {
		out = append(out, op{'+', y[j]})
	}
	return out
}

// LineDiff คืน diff แบบ unified ย่อ (เฉพาะบรรทัดที่ต่าง + บริบท 1 บรรทัด) และจำนวนบรรทัดที่เปลี่ยน
func LineDiff(a, b string) (string, int) {
	ops := diffOps(a, b)
	var out []string
	changed := 0
	for i, o := range ops {
		if o.kind != ' ' {
			changed++
			out = append(out, string(o.kind)+" "+o.text)
			continue
		}
		near := (i > 0 && ops[i-1].kind != ' ') || (i+1 < len(ops) && ops[i+1].kind != ' ')
		if near {
			out = append(out, "  "+o.text)
		} else if len(out) > 0 && out[len(out)-1] != "  …" {
			out = append(out, "  …")
		}
	}
	if changed == 0 {
		return "", 0
	}
	return strings.Join(out, "\n"), changed
}

// changedLines เซตของเลขบรรทัดใน base ที่ถูกแตะ (ลบ หรือมีการแทรกติดกัน)
func changedLines(base, other string) map[int]bool {
	set := map[int]bool{}
	line := 0
	for _, o := range diffOps(base, other) {
		switch o.kind {
		case ' ':
			line++
		case '-':
			set[line] = true
			line++
		case '+':
			set[line] = true
		}
	}
	return set
}

func overlaps(base, cloud, mine string) bool {
	c := changedLines(base, cloud)
	for l := range changedLines(base, mine) {
		if c[l] || c[l-1] || c[l+1] {
			return true
		}
	}
	return false
}

// ThreeWay รวม base→cloud และ base→mine แบบบรรทัด: ใช้ฝั่งที่เปลี่ยน ถ้าทั้งคู่เปลี่ยนที่เดียวกันเป็น conflict (คืนจำนวน)
func ThreeWay(base, cloud, mine string) (string, int) {
	bl := strings.Split(strings.TrimRight(base, "\n"), "\n")
	cOps := alignToBase(base, cloud, len(bl))
	mOps := alignToBase(base, mine, len(bl))
	var out []string
	conflicts := 0
	for i := 0; i <= len(bl); i++ {
		c, m := cOps[i], mOps[i]
		switch {
		case c.changed && m.changed && strings.Join(c.lines, "\n") != strings.Join(m.lines, "\n"):
			conflicts++
			out = append(out, "<<<<<<< cloud")
			out = append(out, c.lines...)
			out = append(out, "=======")
			out = append(out, m.lines...)
			out = append(out, ">>>>>>> mine")
		case c.changed:
			out = append(out, c.lines...)
		case m.changed:
			out = append(out, m.lines...)
		default:
			out = append(out, c.lines...)
		}
	}
	return strings.Join(out, "\n") + "\n", conflicts
}

type slot struct {
	changed bool
	lines   []string
}

// alignToBase แปลง diff ให้เป็น "แต่ละบรรทัดของ base กลายเป็นอะไร" (slot i = สิ่งที่มาแทนบรรทัด i · slot n = ส่วนต่อท้าย)
func alignToBase(base, other string, n int) []slot {
	slots := make([]slot, n+1)
	line := 0
	for _, o := range diffOps(base, other) {
		switch o.kind {
		case ' ':
			slots[line].lines = append(slots[line].lines, o.text)
			line++
		case '-':
			slots[line].changed = true
			line++
		case '+':
			idx := line
			if idx > n {
				idx = n
			}
			slots[idx].changed = true
			slots[idx].lines = append(slots[idx].lines, o.text)
		}
	}
	return slots
}
