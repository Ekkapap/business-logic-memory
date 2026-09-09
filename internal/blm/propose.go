package blm

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Propose = ข้อเสนอแก้ blm.md จาก agent (เช่นตอน /blm_init) ที่เจ้าของต้องตัดสินก่อน — ออกเป็นรายงาน conflict รูปเดียวกับของจริง
// (เจ้าของ 2026-09-09: "หรือทำรายงาน conflict" · วิธีเก็บกวาดข้อ 4: สรุปไม่ได้ → สองฝั่งแบบ git ให้เจ้าของตัดสิน)
//
//	Current (B)  = block ที่มีอยู่ใน blm.md ตอนนี้ (ว่าง = หัวข้อย่อยใหม่)
//	Incoming (A) = block ที่เสนอ
//
// blm.md ไม่ถูกแก้จนกว่า resolve · ตัดสินได้จากไฟล์ (ติ๊ก) หรือ blm conflicts -i / blm resolve blm --keep
// หลายข้อเสนอค้างพร้อมกันได้: ทุกใบใช้ snapshot เดียว conflicts/blm.merged.md (marker <<<<<<< #id >>>>>>> ต่อบริเวณ)
func (s *Store) Propose(topic, heading, content, reason string) (ConflictReport, error) {
	if topic == "" || heading == "" || strings.TrimSpace(content) == "" {
		return ConflictReport{}, fmt.Errorf("propose: topic, heading and content are required")
	}
	if s.RulesPath() == "" {
		return ConflictReport{}, fmt.Errorf("propose: no blm.md yet — blm_create it first")
	}
	rules, err := s.Get(RulesNote)
	if err != nil {
		return ConflictReport{}, err
	}
	// ฐานที่จะแทรก marker: snapshot ที่มีอยู่ (ข้อเสนอ/conflict อื่นค้าง) หรือเนื้อหาปัจจุบัน
	base := conflictPrefixRe.ReplaceAllString(conflictTagRe.ReplaceAllString(rules.Content, ""), "")
	cloudAt := rules.Base
	if raw, err := os.ReadFile(filepath.Join(s.conflictsDir(), RulesNote+".merged.md")); err == nil {
		parts := strings.SplitN(string(raw), "\n---\n", 2)
		if len(parts) == 2 {
			cloudAt = strings.TrimPrefix(parts[0], "cloudUpdatedAt: ")
			base = parts[1]
		}
	}
	incoming := strings.TrimSpace(content)
	if !strings.HasPrefix(incoming, "## ") {
		incoming = "## " + heading + "\n" + incoming
	}
	// ยื่นซ้ำเรื่องเดิม (topic+heading เดียวกัน) = แทนรายงานเก่าที่ยังไม่ตัดสิน ไม่งอกเป็นเลขใหม่
	skip := map[string]bool{}
	for _, c := range s.ListConflicts() {
		if c.Status == "wait" && c.Kind == "proposal" && c.Topic == topic && c.Heading == heading {
			_ = os.Remove(filepath.Join(s.Root, c.File))
			skip[c.File] = true
		}
	}
	id := s.nextConflictID(skip)
	marker := fmt.Sprintf("<<<<<<< #%d >>>>>>>", id)
	lines := strings.Split(base, "\n")
	var out []string
	current := ""
	inTopic, replaced, topicSeen := false, false, false
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		clean := conflictTagRe.ReplaceAllString(l, "")
		if strings.HasPrefix(clean, "# ") {
			if inTopic && !replaced {
				// จบ topic โดยไม่พบหัวข้อย่อย → หัวข้อย่อยใหม่ แทรกท้าย topic
				out = append(out, marker, "")
				replaced = true
			}
			inTopic = strings.TrimSpace(strings.TrimPrefix(clean, "# ")) == strings.TrimSpace(topic)
			if inTopic {
				topicSeen = true
			}
			out = append(out, l)
			continue
		}
		if inTopic && !replaced && strings.HasPrefix(clean, "## ") && strings.TrimSpace(strings.TrimPrefix(clean, "## ")) == strings.TrimSpace(heading) {
			j := i + 1
			for ; j < len(lines); j++ {
				c := conflictTagRe.ReplaceAllString(lines[j], "")
				if strings.HasPrefix(c, "## ") || strings.HasPrefix(c, "# ") || strings.HasPrefix(lines[j], "<<<<<<< #") {
					break
				}
			}
			block := lines[i:j]
			for len(block) > 0 && strings.TrimSpace(block[len(block)-1]) == "" {
				block = block[:len(block)-1]
			}
			current = strings.Join(block, "\n")
			out = append(out, marker, "")
			replaced = true
			i = j - 1
			continue
		}
		out = append(out, l)
	}
	if !replaced {
		if !topicSeen {
			out = append(out, "", "# "+topic, "")
		}
		out = append(out, marker, "")
	}
	if err := os.MkdirAll(s.conflictsDir(), 0o755); err != nil {
		return ConflictReport{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	file := filepath.Join(s.conflictsDir(), fmt.Sprintf("[wait] %s-%s.md", slug(heading), time.Now().Format("20060102150405")))
	curShown := current
	if curShown == "" {
		curShown = "(ยังไม่มีหัวข้อย่อยนี้ใน blm.md — เลือก Current = ไม่เพิ่ม)"
	}
	body := fmt.Sprintf(`---
id: %d
kind: proposal
draft: %s
target: %s
topic: %s
subtopic: %s
noteHeading: %s
status: wait
created: %s
cloudUpdatedAt: %s
---

# ข้อเสนอ #%d — %s › %s

- **ไฟล์:** %s
- **ประเภท:** ข้อเสนอจาก agent (ไม่ใช่การชนกับ cloud) — blm.md ยังไม่ถูกแก้จนกว่าจะตัดสิน

**เหตุผลของข้อเสนอ:** %s

## เทียบสองฝั่ง (แบบ git — แก้ข้อความในฝั่งที่จะเก็บได้เลย)

---

***<<<<<<< Current — กฎที่มีอยู่ตอนนี้ (B)***

%s

---

***>>>>>>> Incoming — ข้อเสนอ (A)***

%s

---

## ตัดสิน

ติ๊ก **หนึ่งช่อง** เท่านั้น แล้วบอก agent ว่า "resolve" (หรือรัน "blm resolve %s --keep current|incoming") — blm.md ถูกอัปเดตทันที ป้ายหายไป แล้ว push ขึ้น backend

- [ ] เอา Current — คงกฎเดิม (**B**)
- [ ] เอา Incoming — รับข้อเสนอ (**A**)
`, id, RulesNote, RulesNote, topic, heading, "## "+heading, now, cloudAt, id, topic, heading, s.rel(filepath.Join(s.Dir, RulesNote+".md")), strings.TrimSpace(reason), curShown, incoming, RulesNote)
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		return ConflictReport{}, err
	}
	if err := os.WriteFile(filepath.Join(s.conflictsDir(), RulesNote+".merged.md"), []byte("cloudUpdatedAt: "+cloudAt+"\n---\n"+strings.Join(out, "\n")), 0o644); err != nil {
		return ConflictReport{}, err
	}
	// ไม่ติดป้ายที่นี่ — Mark แยกต่างหาก
	return ConflictReport{ID: id, File: s.rel(file), Status: "wait", Draft: RulesNote, Topic: topic, Heading: heading, Created: now, NoteHeading: "## " + heading, Kind: "proposal"}, nil
}

// nextConflictID เลขว่างต่ำสุดจากรายงานที่ยัง wait (skip = รายงานที่กำลังจะถูกแทน)
func (s *Store) nextConflictID(skip map[string]bool) int {
	used := map[int]bool{}
	for _, c := range s.ListConflicts() {
		if c.Status == "wait" && !skip[c.File] {
			used[c.ID] = true
		}
	}
	n := 1
	for used[n] {
		n++
	}
	return n
}

var _ = strconv.Itoa
