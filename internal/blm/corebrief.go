package blm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Core brief — บริบท business logic ที่ agent ต้องถืออยู่เสมอ ฉีดผ่าน hook (เจ้าของ 2026-09-20)
//
// ปัญหา: agent ลืมเป้าหมาย/คอนเซ็ปต์หลักของโปรเจ็คแล้วทำเกินสิ่งที่กำหนด · brief ใหม่ทุก session กิน 30 นาที–1 ชม. ·
// หลัง auto-compact บริบทหาย ⇒ ฉีด "แก่น" ของ blm.md ให้เอง: ตาราง Main Business (หัวข้อ + ความหมาย) + รายชื่อหัวข้อย่อย
// + กติกา 3 ข้อ — ไม่ใช่ทั้งไฟล์ (226 บรรทัด) แค่พอให้รู้ว่ากฎมีเรื่องอะไรบ้างและต้อง `blm {query}` ก่อนลงมือ
//
// จังหวะที่ฉีด (เฉพาะ event ที่ Claude Code รับ additionalContext: SessionStart / UserPromptSubmit / PostToolUse —
// PreToolUse รับแค่ allow/deny จึงใช้ "ก่อนแก้" ไม่ได้ ใช้ prompt = ก่อนลงมือ และ post-edit = ระหว่างทำงานยาว):
//   session-start  ทุกครั้ง (startup / resume / clear / **compact**) — บริบทหลัง compact กลับมาที่นี่
//   prompt, post-edit  ฉีดซ้ำเมื่อพ้น debounce เท่านั้น (BLM_CORE_INTERVAL นาที ค่าเริ่มต้น 20) หรือ blm.md เปลี่ยน (hash)
// สถานะต่อ session อยู่ `os.TempDir()/blm-hook-<session_id>.json` — hook รันเป็นโปรเซสใหม่ทุกครั้ง จึงต้องจำลงดิสก์
// ponytail: debounce ต่อ session ไม่ใช่ต่อ agent ย่อย; subagent ที่ session_id เดียวกันแชร์นาฬิกาเดียว

const coreIntervalDefault = 20 * time.Minute

type hookState struct {
	CoreAt    int64  `json:"coreAt"`   // ms ครั้งล่าสุดที่ฉีด core brief
	CoreHash  string `json:"coreHash"` // sha256 ของ blm.md ตอนฉีด
	LastQuery string `json:"lastQuery"`
}

func hookStatePath(sessionID string) string {
	if sessionID == "" {
		sessionID = "default"
	}
	return filepath.Join(os.TempDir(), "blm-hook-"+sessionID+".json")
}

func readHookState(sessionID string) hookState {
	var st hookState
	if raw, err := os.ReadFile(hookStatePath(sessionID)); err == nil {
		_ = json.Unmarshal(raw, &st)
	}
	return st
}

func writeHookState(sessionID string, st hookState) {
	if raw, err := json.Marshal(st); err == nil {
		_ = os.WriteFile(hookStatePath(sessionID), raw, 0o644)
	}
}

func coreInterval() time.Duration {
	if v := os.Getenv("BLM_CORE_INTERVAL"); v != "" {
		if m, err := strconv.Atoi(v); err == nil && m >= 0 {
			return time.Duration(m) * time.Minute
		}
	}
	return coreIntervalDefault
}

// rulesFile — blm.md ใน store (ที่ทำงานจริง) ไม่งั้นตามหาใน mirror ด้วย FindTargetFolder (โฟลเดอร์ไม่ตายตัว)
// อ่านอย่างเดียว: hook ต้องไม่ checkout ลง store เอง (ต่างจาก RulesPath ที่ใช้ตอนคำสั่งเขียน)
func rulesFile(root string, c Config) string {
	s := Open(root, c)
	if s.Has(RulesNote) {
		return filepath.Join(s.Dir, RulesNote+".md")
	}
	if folder, ok := s.FindTargetFolder(RulesNote); ok {
		return filepath.Join(s.MirrorDir, filepath.FromSlash(folder), RulesNote+".md")
	}
	return ""
}

// CoreBrief — แก่นของ blm.md: ตาราง Main Business + หัวข้อย่อย + กติกา · "" เมื่อไม่มีไฟล์กฎ
func CoreBrief(root string, c Config) (text, hash string) {
	file := rulesFile(root, c)
	if file == "" {
		return "", ""
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return "", ""
	}
	sum := sha256.Sum256(raw)
	hash = hex.EncodeToString(sum[:8])
	body := string(raw)
	if i := strings.Index(body, "\n---\n"); strings.HasPrefix(body, "---\n") && i > 0 { // ตัด front matter
		body = body[i+5:]
	}
	var table []string
	var sections []string
	inMain := false
	for _, l := range strings.Split(body, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, "## "):
			h := strings.TrimSpace(t[3:])
			inMain = strings.HasPrefix(h, "Main Business")
			if !inMain && !strings.HasPrefix(h, "วิธีอ่าน") {
				if i := strings.Index(h, " [Conflict]"); i > 0 { // ตัดป้าย conflict ออกจากชื่อ
					h = h[:i]
				}
				sections = append(sections, h)
			}
		case inMain && strings.HasPrefix(t, "|") && !strings.HasPrefix(t, "| ---") && !strings.HasPrefix(t, "| Topic"):
			cells := strings.Split(strings.Trim(t, "|"), "|")
			if len(cells) >= 2 {
				table = append(table, "  - "+strings.TrimSpace(cells[0])+": "+strings.TrimSpace(cells[1]))
			}
		}
	}
	if len(table) == 0 && len(sections) == 0 {
		return "", hash
	}
	rel, _ := filepath.Rel(root, file)
	var b strings.Builder
	fmt.Fprintf(&b, "[blm] core business logic of this project — %s (the single source of truth; a memory note that disagrees loses)\n", rel)
	if len(table) > 0 {
		b.WriteString("Main Business:\n" + strings.Join(table, "\n") + "\n")
	}
	if len(sections) > 0 {
		b.WriteString("Rule blocks: " + strings.Join(sections, " · ") + "\n")
	}
	b.WriteString("Before you change behaviour in any of these areas: blm {query:\"<topic>\"} to read the exact rules. Code that contradicts a rule = stop and report, never bend the code to memory or the rule to the code. Do only what was asked — the owner decides scope and rule changes (blm_conflict for proposals).")
	return b.String(), hash
}

// coreBriefIfDue — ฉีด core brief เมื่อ force หรือพ้น debounce หรือ blm.md เปลี่ยน · อัปเดต state ให้
func coreBriefIfDue(root string, c Config, sessionID string, force bool) string {
	text, hash := CoreBrief(root, c)
	if text == "" {
		return ""
	}
	st := readHookState(sessionID)
	due := force || st.CoreHash != hash || time.Since(time.UnixMilli(st.CoreAt)) >= coreInterval()
	if !due {
		return ""
	}
	st.CoreAt, st.CoreHash = time.Now().UnixMilli(), hash
	writeHookState(sessionID, st)
	return text
}
