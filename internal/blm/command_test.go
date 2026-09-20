package blm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandWriteReadDelete(t *testing.T) {
	root := t.TempDir()
	c := Config{Store: ".claude/blm"}
	s := Open(root, c)

	// write (path relative ต่อ store) → read
	r, err := s.Command(CommandOpts{Action: "write", Path: "reviews/review-x.json", Content: `{"status":"open"}`})
	if err != nil || !r.OK || r.Bytes != 17 {
		t.Fatalf("write: %+v %v", r, err)
	}
	if r, err = s.Command(CommandOpts{Action: "read", Path: ".claude/blm/reviews/review-x.json"}); err != nil || r.Content != `{"status":"open"}` {
		t.Fatalf("read via project path: %+v %v", r, err)
	}

	// delete ไม่มี confirm = ไม่ลบ
	r, err = s.Command(CommandOpts{Action: "delete", Path: "reviews/review-x.json"})
	if err != nil || r.OK || !r.NeedConfirm {
		t.Fatalf("delete without confirm: %+v %v", r, err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "reviews/review-x.json")); err != nil {
		t.Fatal("file must still exist")
	}
	// confirm ผิดชื่อ = ไม่ลบ
	if r, _ = s.Command(CommandOpts{Action: "delete", Path: "reviews/review-x.json", Confirm: "other"}); r.OK {
		t.Fatal("wrong confirm must not delete")
	}
	// confirm ถูก = ย้ายไป .trash
	r, err = s.Command(CommandOpts{Action: "delete", Path: "reviews/review-x.json", Confirm: "review-x.json"})
	if err != nil || !r.OK || !strings.Contains(r.Trash, ".trash/") {
		t.Fatalf("delete: %+v %v", r, err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "reviews/review-x.json")); err == nil {
		t.Fatal("file must be gone")
	}
	if m, _ := filepath.Glob(filepath.Join(s.Dir, ".trash", "*-review-x.json")); len(m) != 1 {
		t.Fatalf("trash: %v", m)
	}

	// get ตาม id
	if _, err := s.Command(CommandOpts{Action: "write", Path: "deadbeef", Content: "{}"}); err != nil {
		t.Fatal(err)
	}
	if r, err = s.Command(CommandOpts{Action: "get", Path: "deadbeef"}); err != nil || r.Content != "{}" || r.Path != ".claude/blm/reviews/review-deadbeef.json" {
		t.Fatalf("get by id: %+v %v", r, err)
	}

	// list: "." = store เอง, โฟลเดอร์ย่อยลงท้าย /
	r, err = s.Command(CommandOpts{Action: "list", Path: "."})
	if err != nil || !r.OK || !strings.Contains(strings.Join(r.Files, ","), "reviews/") || !strings.Contains(strings.Join(r.Files, ","), ".trash/") {
		t.Fatalf("list: %+v %v", r, err)
	}
	if r, err = s.Command(CommandOpts{Action: "list", Path: "reviews"}); err != nil || len(r.Files) != 1 {
		t.Fatalf("list reviews: %+v %v", r, err)
	}

	// นอก store = ปฏิเสธ
	for _, p := range []string{"../../x.txt", "/etc/hosts", filepath.Join(root, "a.txt"), "."} {
		if _, err := s.Command(CommandOpts{Action: "write", Path: p, Content: "x"}); err == nil {
			t.Fatalf("must refuse %s", p)
		}
	}
	log, _ := os.ReadFile(filepath.Join(s.Dir, "commands.log"))
	if n := strings.Count(string(log), "\n"); n < 6 {
		t.Fatalf("log lines %d:\n%s", n, log)
	}
}

func TestSetJSON(t *testing.T) {
	root := t.TempDir()
	s := Open(root, Config{Store: ".claude/blm"})
	if _, err := s.Command(CommandOpts{Action: "write", Path: "abcd1234", Content: `{"status":"done","doneAt":"x","topics":[{"id":"a","status":"created"},{"id":"b","status":"new"}]}`}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		sel string
		val any
	}{{"status", "open"}, {"doneAt", nil}, {"topics[id=b].status", "proposed"}, {"topics[0]", nil}, {"topics[+]", map[string]any{"id": "c"}}, {"tags[+]", "x"}} {
		if r, err := s.Command(CommandOpts{Action: "set", Path: "abcd1234", Select: c.sel, Value: c.val}); err != nil || !r.OK {
			t.Fatalf("set %s: %+v %v", c.sel, r, err)
		}
	}
	r, _ := s.Command(CommandOpts{Action: "get", Path: "abcd1234", Select: "."})
	b, _ := json.Marshal(r.Data)
	if string(b) != `{"status":"open","tags":["x"],"topics":[{"id":"b","status":"proposed"},{"id":"c"}]}` {
		t.Fatalf("after set: %s", b)
	}
	if _, err := s.Command(CommandOpts{Action: "set", Path: "abcd1234", Select: "topics[id=zz].status", Value: 1}); err == nil {
		t.Fatal("must fail when not found")
	}
}

func TestPatchLines(t *testing.T) {
	s := Open(t.TempDir(), Config{Store: ".claude/blm"})
	_, _ = s.Command(CommandOpts{Action: "write", Path: "n.md", Content: "a\nb\nc\nd\n"})
	if r, err := s.Command(CommandOpts{Action: "patch", Path: "n.md", Line: 2, EndLine: 3, Content: "B\nB2"}); err != nil || !r.OK {
		t.Fatal(err)
	}
	if _, err := s.Command(CommandOpts{Action: "patch", Path: "n.md", Line: 1, Insert: true, Content: "top"}); err != nil { // แทรกก่อนบรรทัด 1
		t.Fatal(err)
	}
	if _, err := s.Command(CommandOpts{Action: "patch", Path: "n.md", Line: 5, Content: ""}); err != nil { // ลบบรรทัด 5 (d)
		t.Fatal(err)
	}
	r, _ := s.Command(CommandOpts{Action: "read", Path: "n.md"})
	if r.Content != "top\na\nB\nB2\n" {
		t.Fatalf("%q", r.Content)
	}
	if _, err := s.Command(CommandOpts{Action: "patch", Path: "n.md", Line: 9, Content: "x"}); err == nil {
		t.Fatal("out of range must fail")
	}
}
