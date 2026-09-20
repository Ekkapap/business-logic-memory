package blm

import (
	"strings"
	"testing"
)

func TestSplitComments(t *testing.T) {
	ts := `import { x } from "y";
/**
 * Seal the LINE link ticket.
 * Why: the OTP must not be issued twice.
 */
export function seal(id: string) {
  const url = "http://example.com"; // keep the scheme
  return sealToken(id); // why: purpose scoped
}`
	code, com := splitComments(ts, "typescript")
	if strings.Contains(code, "Seal the LINE") || !strings.Contains(code, "export function seal") || !strings.Contains(code, `"http://example.com";`) {
		t.Fatalf("ts code wrong:\n%s", code)
	}
	if !strings.Contains(com, "Seal the LINE link ticket.") || !strings.Contains(com, "keep the scheme") || !strings.Contains(com, "purpose scoped") || strings.Contains(com, "export") {
		t.Fatalf("ts comment wrong:\n%s", com)
	}
	py := "# top note\nx = 1  # inline\ndef f():\n    return x\n"
	code, com = splitComments(py, "python")
	if code != "x = 1\ndef f():\nreturn x" || com != "top note\ninline" {
		t.Fatalf("py: code=%q com=%q", code, com)
	}
	sql := "-- who: admin\nselect 1; /* block */\n"
	code, com = splitComments(sql, "sql")
	if !strings.HasPrefix(code, "select 1;") || !strings.Contains(com, "who: admin") || !strings.Contains(com, "block") {
		t.Fatalf("sql: code=%q com=%q", code, com)
	}
	// ไม่มีคอมเมนต์เลย
	if _, com := splitComments("func a() {}\n", "go"); com != "" {
		t.Fatalf("go: unexpected comment %q", com)
	}
	// ก้อนที่เริ่มกลาง /** … */ (ไม่มี /* ในก้อน แต่มี */) → บรรทัดก่อน */ เป็นคอมเมนต์
	code, com = splitComments(" * แล้วรับ OTP ของเขาแทน\n * การพิสูจน์ด้วยรหัสผ่าน\n */\nexport function X() {}\n", "typescript")
	if code != "export function X() {}" || !strings.Contains(com, "แล้วรับ OTP") {
		t.Fatalf("mid-block: code=%q com=%q", code, com)
	}
	if !proseLanguage("markdown") || !proseLanguage("sql") || !proseLanguage("json") || proseLanguage("typescript") || proseLanguage("go") {
		t.Fatal("codeLanguage allowlist wrong")
	}
}

func TestTrustLabel(t *testing.T) {
	cases := []struct {
		code, com, agree float64
		want             string
	}{
		{0.7, 0.5, 0.8, "code"},      // ชนโค้ด
		{0.6, 0.62, 0.9, "both ✓"},   // ชนทั้งคู่ ไปทางเดียวกัน
		{0.6, 0.62, 0.3, "both"},     // ชนทั้งคู่ แต่ไม่ค่อยตรงกัน
		{0.3, 0.7, 0.8, "comment ~"}, // ชนคอมเมนต์ แต่คอมเมนต์ตรงกับโค้ด
		{0.3, 0.7, 0.2, "comment ⚠"}, // ชนคอมเมนต์ และโค้ดพูดอีกเรื่อง
	}
	for _, c := range cases {
		tr := &Trust{CodeSim: c.code, CommentSim: c.com, Agree: c.agree}
		trustLabel(tr)
		if tr.Label != c.want {
			t.Errorf("%+v → %q want %q", c, tr.Label, c.want)
		}
	}
	none := &Trust{Agree: -1}
	trustLabel(none)
	if none.Label != "code" {
		t.Fatalf("no comment → code, got %q", none.Label)
	}
	if cosine([]float64{1, 0}, []float64{1, 0}) < 0.999 || cosine([]float64{1, 0}, []float64{0, 1}) != 0 {
		t.Fatal("cosine wrong")
	}
}
