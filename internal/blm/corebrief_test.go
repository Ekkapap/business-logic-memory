package blm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sampleRules = `---
target: blm
---

# วิธีอ่านไฟล์นี้
- กฎ

## Main Business
| Topic | ความหมาย |
| --- | --- |
| Authentication | ทางเข้าระบบทั้งหมด |
| Consent & Signing | ใบยินยอม PDF |

## Email & Password
- rule a

## Memory [Conflict](conflicts/x.md)
- rule b
`

func rulesProject(t *testing.T) (string, Config) {
	t.Helper()
	root := t.TempDir()
	c := Config{Backend: BackendNone, Store: ".claude/blm"}
	_ = os.MkdirAll(filepath.Join(root, c.Store), 0o755)
	_ = os.WriteFile(filepath.Join(root, c.Store, "blm.md"), []byte(sampleRules), 0o644)
	return root, c
}

func TestCoreBriefExtract(t *testing.T) {
	root, c := rulesProject(t)
	text, hash := CoreBrief(root, c)
	if hash == "" || !strings.Contains(text, "- Authentication: ทางเข้าระบบทั้งหมด") || !strings.Contains(text, "- Consent & Signing: ใบยินยอม PDF") {
		t.Fatalf("table missing:\n%s", text)
	}
	if !strings.Contains(text, "Rule blocks: Email & Password · Memory") || strings.Contains(text, "[Conflict]") || strings.Contains(text, "วิธีอ่าน") || strings.Contains(text, "Main Business ·") {
		t.Fatalf("sections wrong:\n%s", text)
	}
	if !strings.Contains(text, "blm {query:") || !strings.Contains(text, "Do only what was asked") {
		t.Fatalf("rules line missing:\n%s", text)
	}
	// ไม่มีไฟล์กฎ = ว่าง เงียบ
	if text, _ := CoreBrief(t.TempDir(), c); text != "" {
		t.Fatal("no rules file must give empty brief")
	}
	// "## Read first" ถูกฉีดตรง ๆ และไม่นับเป็น rule block
	_ = os.WriteFile(filepath.Join(root, c.Store, "blm.md"), []byte("## Read first\n- memory_get {note:\"read-first\", scope:\"agent\"} — กติกาการทำงาน\n\n"+sampleRules), 0o644)
	text, _ = CoreBrief(root, c)
	if !strings.Contains(text, "Read first (before anything else in this session):\n  - memory_get {note:\"read-first\"") || strings.Contains(text, "Rule blocks: Read first") || strings.Contains(text, "· Read first") {
		t.Fatalf("read first:\n%s", text)
	}
}

func TestCoreBriefDebounce(t *testing.T) {
	root, c := rulesProject(t)
	t.Setenv("TMPDIR", t.TempDir())
	sid := "sess-1"
	// ครั้งแรก (prompt, ไม่ force) ฉีด เพราะยังไม่เคย
	if coreBriefIfDue(root, c, sid, false) == "" {
		t.Fatal("first call must inject")
	}
	// ทันทีหลังจากนั้น: เงียบ
	if coreBriefIfDue(root, c, sid, false) != "" {
		t.Fatal("second call within interval must be silent")
	}
	// force (session-start) ฉีดเสมอ
	if coreBriefIfDue(root, c, sid, true) == "" {
		t.Fatal("force must inject")
	}
	// blm.md เปลี่ยน → ฉีดแม้ยังไม่พ้นเวลา
	_ = os.WriteFile(filepath.Join(root, c.Store, "blm.md"), []byte(sampleRules+"\n## New\n- x\n"), 0o644)
	if out := coreBriefIfDue(root, c, sid, false); !strings.Contains(out, "New") {
		t.Fatalf("changed rules must re-inject, got %q", out)
	}
	// พ้น interval → ฉีด (ย้อน CoreAt ไป 21 นาที)
	st := readHookState(sid)
	st.CoreAt = time.Now().Add(-21 * time.Minute).UnixMilli()
	writeHookState(sid, st)
	if coreBriefIfDue(root, c, sid, false) == "" {
		t.Fatal("after the interval must inject")
	}
	// BLM_CORE_INTERVAL=0 = ทุกครั้ง
	t.Setenv("BLM_CORE_INTERVAL", "0")
	if coreBriefIfDue(root, c, sid, false) == "" {
		t.Fatal("interval 0 must inject every time")
	}
	// hook events ผ่าน Hook(): session-start compact มีบรรทัดบอก · post-edit เงียบทันทีหลังนั้น (interval กลับเป็น default)
	t.Setenv("BLM_CORE_INTERVAL", "20")
	var out strings.Builder
	in := HookInput{SessionID: sid, Source: "compact"}
	_ = Hook("session-start", in, root, &out)
	if !strings.Contains(out.String(), "context was just compacted") || !strings.Contains(out.String(), "Authentication") {
		t.Fatalf("session-start compact: %s", out.String())
	}
	out.Reset()
	_ = Hook("post-edit", HookInput{SessionID: sid}, root, &out)
	if out.Len() != 0 {
		t.Fatalf("post-edit right after session-start must be silent: %s", out.String())
	}
}
