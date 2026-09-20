package blm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const indexRules = `---
target: blm
---

# กฎ

## Main Business
| Topic | ความหมาย |
| --- | --- |
| [Authentication](global/conventions/blm/blm-authentication.md) | ทางเข้าระบบทั้งหมด |
| [Consent](global/conventions/blm/blm-consent.md) | ใบยินยอม |
| Plain | หัวข้อที่ยังไม่แยกไฟล์ |

## Plain
- rule in index itself
memory: x
`

func TestRulesFollowTopicNotes(t *testing.T) {
	root := t.TempDir()
	c := Config{Backend: BackendAgentsRoom, Store: ".agentsroom/blm", Mirror: ".agentsroom/memory"}
	_ = os.MkdirAll(filepath.Join(root, c.Store), 0o755)
	_ = os.MkdirAll(filepath.Join(root, c.Mirror, "global", "conventions", "blm"), 0o755)
	_ = os.WriteFile(filepath.Join(root, c.Store, "blm.md"), []byte(indexRules), 0o644)
	// authentication อยู่ใน store (ร่างล่าสุด) · consent อยู่แค่ mirror
	_ = os.WriteFile(filepath.Join(root, c.Store, "blm-authentication.md"), []byte("---\ntarget: blm-authentication\nfolder: global/conventions/blm\n---\n\n## Email & Password\n- OTP ทาง LINE\nmemory: a\ncode: b\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, c.Mirror, "global", "conventions", "blm", "blm-consent.md"), []byte("---\nname: blm-consent\n---\n\n## ใบยินยอม\n- ลงนาม LIFF\nmemory: c\n"), 0o644)

	s := Open(root, c)
	res := s.Rules("", "test")
	var headings []string
	for _, b := range res.Blocks {
		headings = append(headings, b.Topic+" › "+b.Heading)
	}
	joined := strings.Join(headings, " | ")
	for _, want := range []string{"Authentication › Email & Password", "Consent › ใบยินยอม", "กฎ › Plain"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %s", want, joined)
		}
	}
	// query เจาะหัวข้อในโน้ตแยก · ref ชี้ไฟล์หัวข้อ ไม่ใช่ blm.md
	res = s.Rules("OTP", "test")
	if len(res.Blocks) != 1 || res.Blocks[0].Heading != "Email & Password" || !strings.HasSuffix(res.Blocks[0].RefShort, "blm-authentication.md:6") || !res.Blocks[0].Rule {
		t.Fatalf("query into topic note wrong: %+v", res.Blocks)
	}
	topics, rules := s.TopicCount()
	if topics != 3 || rules != 3 {
		t.Fatalf("TopicCount = %d topics %d rules, want 3/3", topics, rules)
	}
	// ลิงก์ค้าง → block (missing) ไม่เงียบ
	withGhost := strings.Replace(indexRules, "| Plain | หัวข้อที่ยังไม่แยกไฟล์ |", "| [Ghost](global/conventions/blm/blm-ghost.md) | x |\n| Plain | หัวข้อที่ยังไม่แยกไฟล์ |", 1)
	_ = os.WriteFile(filepath.Join(root, c.Store, "blm.md"), []byte(withGhost), 0o644)
	res = s.Rules("blm-ghost", "test")
	found := false
	for _, b := range res.Blocks {
		if b.Topic == "Ghost" && strings.Contains(b.Heading, "missing") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing topic note must surface: %+v", res.Blocks)
	}
}

func TestTopicFolderDefaultsAndEnv(t *testing.T) {
	ar := Config{Backend: BackendAgentsRoom, Store: ".agentsroom/blm", Mirror: ".agentsroom/memory"}
	if ar.TopicFolder() != "global/conventions/blm" || ar.MemoryRoot() != ".agentsroom/memory" || ar.TopicLink("blm-x") != "global/conventions/blm/blm-x.md" {
		t.Fatalf("agentsroom defaults: %s %s", ar.TopicFolder(), ar.MemoryRoot())
	}
	none := Config{Backend: BackendNone, Store: ".claude/blm"}
	if none.TopicFolder() != "blm" || none.MemoryRoot() != ".claude/memory" {
		t.Fatalf("none defaults: %s %s", none.TopicFolder(), none.MemoryRoot())
	}
	t.Setenv("BLM_TOPICS_FOLDER", "/rules/topics/")
	t.Setenv("BLM_MEMORY_DIR", "docs/memory")
	if none.TopicFolder() != "rules/topics" || none.MemoryRoot() != "docs/memory" {
		t.Fatalf("env override: %s %s", none.TopicFolder(), none.MemoryRoot())
	}
	explicit := Config{Backend: BackendNone, Store: ".claude/blm", TopicsFolder: "biz"}
	if explicit.TopicFolder() != "biz" {
		t.Fatalf("config beats env: %s", explicit.TopicFolder())
	}
}

func TestTopicNotesResolveUnderMemoryRoot(t *testing.T) {
	root := t.TempDir()
	c := Config{Backend: BackendNone, Store: ".claude/blm"}
	_ = os.MkdirAll(filepath.Join(root, c.Store), 0o755)
	_ = os.MkdirAll(filepath.Join(root, ".claude", "memory", "blm"), 0o755)
	idx := "---\ntarget: blm\n---\n\n## Main Business\n| Topic | ความหมาย |\n| --- | --- |\n| [Auth](blm/blm-auth.md) | login |\n"
	_ = os.WriteFile(filepath.Join(root, c.Store, "blm.md"), []byte(idx), 0o644)
	_ = os.WriteFile(filepath.Join(root, ".claude", "memory", "blm", "blm-auth.md"), []byte("## Password\n- 8 chars\n"), 0o644)
	res := Open(root, c).Rules("chars", "test")
	if len(res.Blocks) != 1 || res.Blocks[0].Topic != "Auth" || !strings.HasSuffix(res.Blocks[0].RefShort, ".claude/memory/blm/blm-auth.md:1") {
		t.Fatalf("topic note under .claude/memory/blm not followed: %+v", res.Blocks)
	}
}

func TestProposeTargetsTopicNoteAndMarksTableRow(t *testing.T) {
	root := t.TempDir()
	c := Config{Backend: BackendAgentsRoom, Store: ".agentsroom/blm", Mirror: ".agentsroom/memory"}
	_ = os.MkdirAll(filepath.Join(root, c.Store), 0o755)
	_ = os.MkdirAll(filepath.Join(root, c.Mirror, "global", "conventions", "blm"), 0o755)
	_ = os.WriteFile(filepath.Join(root, c.Store, "blm.md"), []byte(indexRules), 0o644)
	// โน้ตหัวข้ออยู่แค่ mirror → Propose ต้อง checkout มาก่อน
	_ = os.WriteFile(filepath.Join(root, c.Mirror, "global", "conventions", "blm", "blm-consent.md"), []byte("---\nname: blm-consent\ndescription: consent\nfolder: global/conventions/blm\n---\n\n# Consent\n\n## ใบยินยอม\n- ลงนาม LIFF\nmemory: c\n"), 0o644)
	s := Open(root, c)
	r, err := s.Propose("Consent", "ใบยินยอม", "## ใบยินยอม\n- ลงนามผ่าน LIFF เท่านั้น\nmemory: c\n", "กฎเก่าไม่ครบ")
	if err != nil {
		t.Fatal(err)
	}
	if r.Draft != "blm-consent" || !s.Has("blm-consent") || exists(filepath.Join(s.conflictsDir(), "blm-consent.merged.md")) { // ข้อเสนอไม่มี snapshot
		t.Fatalf("proposal must target the topic note: %+v", r)
	}
	// หัวข้อที่ยังอยู่ใน blm.md เอง → ยังลง blm.md
	r2, err := s.Propose("กฎ", "Plain", "## Plain\n- changed\nmemory: x\n", "x")
	if err != nil || r2.Draft != RulesNote {
		t.Fatalf("plain topic must stay on blm.md: %+v %v", r2, err)
	}
	s.Mark()
	rules, _ := s.Get(RulesNote)
	if !strings.Contains(rules.Content, "| [Conflict](") || !strings.Contains(rules.Content, ") [Consent](global/conventions/blm/blm-consent.md) |") {
		t.Fatalf("table row of the topic not tagged:\n%s", rules.Content)
	}
	if got := s.topicNotes(rules.Content); len(got) != 2 || got[1].Name != "blm-consent" {
		t.Fatalf("tagged row must still parse: %+v", got)
	}
	note, _ := s.Get("blm-consent")
	if !strings.Contains(note.Content, "## ใบยินยอม [Conflict](") {
		t.Fatalf("heading in topic note not tagged:\n%s", note.Content)
	}
	// เจ้าของรับข้อเสนอ → เขียนลงโน้ตหัวข้อ ไม่ใช่ blm.md
	raw, _ := os.ReadFile(filepath.Join(root, r.File))
	_ = os.WriteFile(filepath.Join(root, r.File), []byte(strings.Replace(string(raw), "- [ ] เอา Incoming — รับข้อเสนอ (**A**)", "- [x] เอา Incoming — รับข้อเสนอ (**A**)", 1)), 0o644)
	if _, err := s.ResolveConflicts("blm-consent"); err != nil {
		t.Fatal(err)
	}
	note, _ = s.Get("blm-consent")
	rules, _ = s.Get(RulesNote)
	if !strings.Contains(note.Content, "ลงนามผ่าน LIFF เท่านั้น") || strings.Contains(note.Content, "[Conflict]") || strings.Contains(rules.Content, "| [Conflict](") || !strings.Contains(rules.Content, "## Plain [Conflict](") || strings.Contains(rules.Content, "LIFF เท่านั้น") {
		t.Fatalf("resolve wrote to the wrong place:\n%s\n---\n%s", note.Content, rules.Content)
	}
}

// เคสจริง 2026-09-20: ข้อเสนอค้างตั้งแต่ blm.md ยังเป็นฉบับเต็ม แล้ว blm.md ถูกแยกเป็นสารบัญ — resolve ต้องต่อเข้าฉบับปัจจุบัน ไม่ใช่คืนฉบับเก่า
func TestResolveProposalUsesCurrentNoteNotStaleSnapshot(t *testing.T) {
	root := t.TempDir()
	c := Config{Backend: BackendNone, Store: ".claude/blm"}
	_ = os.MkdirAll(filepath.Join(root, c.Store), 0o755)
	full := "---\ntarget: blm\n---\n\n# Working Agreement\n\n## Memory\n- old rule\nmemory: m\n\n## Other\n- keep\n"
	_ = os.WriteFile(filepath.Join(root, c.Store, "blm.md"), []byte(full), 0o644)
	s := Open(root, c)
	r, err := s.Propose("Working Agreement", "Memory", "## Memory\n- new rule\nmemory: m\n", "x")
	if err != nil {
		t.Fatal(err)
	}
	// เจ้าของ (หรือ agent) เขียน blm.md ใหม่เป็นสารบัญระหว่างที่ข้อเสนอค้าง
	index := "---\ntarget: blm\n---\n\n## Main Business\n| Topic | cue |\n| --- | --- |\n| WA | x |\n\n# Working Agreement\n\n## Memory\n- old rule\nmemory: m\n"
	if _, err := s.save(Input{Name: RulesNote, Content: index, HasContent: true}, "test"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, r.File))
	_ = os.WriteFile(filepath.Join(root, r.File), []byte(strings.Replace(string(raw), "- [ ] เอา Incoming — รับข้อเสนอ (**A**)", "- [x] เอา Incoming — รับข้อเสนอ (**A**)", 1)), 0o644)
	if _, err := s.ResolveConflicts(RulesNote); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(RulesNote)
	if !strings.Contains(got.Content, "## Main Business") || strings.Contains(got.Content, "## Other") || !strings.Contains(got.Content, "- new rule") || strings.Contains(got.Content, "- old rule") {
		t.Fatalf("resolve must splice into the current index, got:\n%s", got.Content)
	}
}

// ข้อเสนอเก่าที่ยื่นตอนหัวข้อยังอยู่ใน blm.md → หลังแยกไฟล์ ต้องชี้ไปโน้ตหัวข้อเอง
func TestOldProposalRedirectsToTopicNote(t *testing.T) {
	root := t.TempDir()
	c := Config{Backend: BackendNone, Store: ".claude/blm"}
	_ = os.MkdirAll(filepath.Join(root, c.Store), 0o755)
	_ = os.WriteFile(filepath.Join(root, c.Store, "blm.md"), []byte("---\ntarget: blm\n---\n\n# Consent\n\n## ใบยินยอม\n- old\n"), 0o644)
	s := Open(root, c)
	if _, err := s.Propose("Consent", "ใบยินยอม", "## ใบยินยอม\n- new\n", "x"); err != nil {
		t.Fatal(err)
	}
	// แยกไฟล์
	_ = os.WriteFile(filepath.Join(root, c.Store, "blm.md"), []byte("---\ntarget: blm\n---\n\n## Main Business\n| Topic | cue |\n| --- | --- |\n| [Consent](blm/blm-consent.md) | x |\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, c.Store, "blm-consent.md"), []byte("---\ntarget: blm-consent\n---\n\n# Consent\n\n## ใบยินยอม\n- old\n"), 0o644)
	list := s.ListConflicts()
	if len(list) != 1 || list[0].Draft != "blm-consent" {
		t.Fatalf("old proposal must point at the topic note: %+v", list)
	}
	if _, err := s.ResolveConflicts(RulesNote); err == nil {
		t.Fatal("resolve blm must find nothing now")
	}
	raw, _ := os.ReadFile(filepath.Join(root, list[0].File))
	_ = os.WriteFile(filepath.Join(root, list[0].File), []byte(strings.Replace(string(raw), "- [ ] เอา Incoming — รับข้อเสนอ (**A**)", "- [x] เอา Incoming — รับข้อเสนอ (**A**)", 1)), 0o644)
	if _, err := s.ResolveConflicts("blm-consent"); err != nil {
		t.Fatal(err)
	}
	note, _ := s.Get("blm-consent")
	idx, _ := s.Get(RulesNote)
	if !strings.Contains(note.Content, "- new") || strings.Contains(idx.Content, "# Consent") {
		t.Fatalf("wrong target:\n%s\n---\n%s", note.Content, idx.Content)
	}
}

// เขียนโน้ตหัวข้อผ่าน append/patch → path ในบรรทัด code:/verify: เป็นลิงก์เอง (เหมือน blm.md ตอน checkout)
func TestTopicNoteSaveLinksPaths(t *testing.T) {
	root := t.TempDir()
	c := Config{Backend: BackendNone, Store: ".claude/blm"}
	_ = os.MkdirAll(filepath.Join(root, c.Store), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "wireguard", "tools"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "wireguard", "tools", "gateway-fix.sh"), []byte("#!/bin/sh\n"), 0o755)
	_ = os.WriteFile(filepath.Join(root, c.Store, "blm.md"), []byte("---\ntarget: blm\n---\n\n## Main Business\n| Topic | cue |\n| --- | --- |\n| [VPN](blm/blm-vpn.md) | x |\n"), 0o644)
	s := Open(root, c)
	if _, err := s.Create(Input{Name: "blm-vpn", Mode: "replace", Content: "# VPN\n\n## Gateway\n- rule\nmemory: -\ncode: wireguard/tools/gateway-fix.sh\n", HasContent: true}); err != nil {
		t.Fatal(err)
	}
	n, _ := s.Get("blm-vpn")
	if !strings.Contains(n.Content, "code: [gateway-fix.sh](wireguard/tools/gateway-fix.sh)") {
		t.Fatalf("path not linked:\n%s", n.Content)
	}
	// ในโค้ดสแปนไม่ลิงก์ · ลิงก์ที่เคยติดอยู่ในสแปนถูกถอด backtick
	_, _ = s.Append(Input{Name: "blm-vpn", Content: "## Ops\n- run `wireguard/tools/gateway-fix.sh status` in `[wireguard/](wireguard/)`\ncode: wireguard/tools/gateway-fix.sh\n", HasContent: true})
	n, _ = s.Get("blm-vpn")
	if !strings.Contains(n.Content, "run `wireguard/tools/gateway-fix.sh status` in [wireguard/](wireguard/)") {
		t.Fatalf("code span handling wrong:\n%s", n.Content)
	}
	// โน้ตธรรมดา (ไม่ใช่ไฟล์กฎ) ไม่ถูกแตะ
	_, _ = s.Create(Input{Name: "other", Content: "see wireguard/tools/gateway-fix.sh\n", HasContent: true})
	o, _ := s.Get("other")
	if strings.Contains(o.Content, "](") {
		t.Fatalf("non-rules note must not be linked: %s", o.Content)
	}
}
