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
// ไฟล์กฎไม่ถูกแก้จนกว่า resolve · ตัดสินได้จากไฟล์ (ติ๊ก) หรือ blm conflicts -i / blm resolve <โน้ต> --keep
// หลายข้อเสนอค้างพร้อมกันได้: แต่ละใบถูกต่อเข้าโน้ตอิสระกันตอน resolve (spliceBlock) ไม่มี snapshot ร่วม
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
	// blm.md เป็นสารบัญ: หัวข้อที่แยกเป็นโน้ต `blm-<topic>` แล้ว ข้อเสนอลงที่โน้ตนั้น (ร่าง/resolve/push เป็นรายหัวข้อ) ไม่ใช่ blm.md
	note := RulesNote
	if t, ok := s.topicNoteFor(rules.Content, topic); ok {
		note = t.Name
		if !s.Has(note) {
			if _, err := s.Checkout(note); err != nil {
				return ConflictReport{}, fmt.Errorf("propose: topic note %q: %w", note, err)
			}
		}
		if rules, err = s.Get(note); err != nil {
			return ConflictReport{}, err
		}
	}
	// ฐาน = เนื้อหาปัจจุบันของโน้ตเสมอ (ไม่ใช่ snapshot): ข้อเสนอถูก "ต่อ" เข้าโน้ตตอน resolve ด้วย spliceBlock บนเนื้อหา ณ ตอนนั้น
	// snapshot แบบ marker ใช้เฉพาะการชนกับ cloud — เคยพัง 2026-09-20: blm.md ถูกแยกเป็นสารบัญ แต่ blm.merged.md ยังถือฉบับ 205 บรรทัด
	// ถ้า resolve ตอนนั้นจะเขียนฉบับเก่าทับสารบัญทั้งไฟล์
	base := conflictPrefixRe.ReplaceAllString(conflictTagRe.ReplaceAllString(rules.Content, ""), "")
	cloudAt := rules.Base
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
	_, current := spliceBlock(base, topic, heading, "")
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
- **ประเภท:** ข้อเสนอจาก agent (ไม่ใช่การชนกับ cloud) — ไฟล์กฎยังไม่ถูกแก้จนกว่าจะตัดสิน

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

ติ๊ก **หนึ่งช่อง** เท่านั้น แล้วบอก agent ว่า "resolve" (หรือรัน "blm resolve %s --keep current|incoming") — ไฟล์กฎถูกอัปเดตทันที ป้ายหายไป แล้ว push ขึ้น backend

- [ ] เอา Current — คงกฎเดิม (**B**)
- [ ] เอา Incoming — รับข้อเสนอ (**A**)
`, id, note, note, topic, heading, "## "+heading, now, cloudAt, id, topic, heading, s.rel(filepath.Join(s.Dir, note+".md")), strings.TrimSpace(reason), curShown, incoming, note)
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		return ConflictReport{}, err
	}
	// ไม่ติดป้ายที่นี่ — Mark แยกต่างหาก
	return ConflictReport{ID: id, File: s.rel(file), Status: "wait", Draft: note, Topic: topic, Heading: heading, Created: now, NoteHeading: "## " + heading, Kind: "proposal"}, nil
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

// spliceBlock แทน block `## heading` ใต้ `# topic` ด้วย replacement (ว่าง = แค่หา) · ไม่มีหัวข้อย่อย → แทรกท้าย topic ·
// ไม่มี topic → ต่อท้ายไฟล์พร้อม `# topic` · คืนเนื้อหาใหม่และข้อความ block เดิม ("" = หัวข้อย่อยใหม่)
// โน้ตหัวข้อที่ไม่มีบรรทัด `# topic` เลย (block เริ่มที่ ## ทันที) ถือว่าทั้งไฟล์คือ topic นั้น
func spliceBlock(base, topic, heading, replacement string) (out string, current string) {
	lines := strings.Split(base, "\n")
	hasTopic := false
	for _, l := range lines {
		if strings.HasPrefix(conflictTagRe.ReplaceAllString(l, ""), "# ") {
			hasTopic = true
			break
		}
	}
	var res []string
	inTopic, replaced := !hasTopic, false
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		clean := conflictTagRe.ReplaceAllString(l, "")
		if strings.HasPrefix(clean, "# ") {
			if inTopic && !replaced {
				res = appendBlock(res, replacement)
				replaced = true
			}
			inTopic = strings.TrimSpace(strings.TrimPrefix(clean, "# ")) == strings.TrimSpace(topic)
			res = append(res, l)
			continue
		}
		if inTopic && !replaced && strings.HasPrefix(clean, "## ") && strings.TrimSpace(strings.TrimPrefix(clean, "## ")) == strings.TrimSpace(heading) {
			j := i + 1
			for ; j < len(lines); j++ {
				c := conflictTagRe.ReplaceAllString(lines[j], "")
				if strings.HasPrefix(c, "## ") || strings.HasPrefix(c, "# ") {
					break
				}
			}
			block := lines[i:j]
			for len(block) > 0 && strings.TrimSpace(block[len(block)-1]) == "" {
				block = block[:len(block)-1]
			}
			current = strings.Join(block, "\n")
			res = appendBlock(res, replacement)
			replaced = true
			i = j - 1
			continue
		}
		res = append(res, l)
	}
	if !replaced {
		if !inTopic {
			res = append(res, "", "# "+topic)
		}
		res = appendBlock(res, replacement)
	}
	return strings.Join(res, "\n"), current
}

func appendBlock(res []string, text string) []string {
	if text == "" {
		return res
	}
	if len(res) > 0 && strings.TrimSpace(res[len(res)-1]) != "" {
		res = append(res, "")
	}
	return append(res, strings.TrimRight(text, "\n"), "")
}
