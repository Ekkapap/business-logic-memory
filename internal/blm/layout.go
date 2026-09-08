package blm

import (
	"fmt"
	"regexp"
	"strings"
)

// CheckRow ผลตรวจหนึ่งหัวข้อย่อย (agent ตัดสิน tool จัดหน้า)
type CheckRow struct {
	Topic   string `json:"topic"`
	Heading string `json:"heading"`
	Result  string `json:"result"` // PASSED | NOT PASSED | UNKNOWN
	Ref     string `json:"ref"`    // เช่น blm.md:29
}

// UNKNOWN แสดง "-" เพราะอยู่ใต้หัวข้อ **UNKNOWN** อยู่แล้ว ไม่ต้องซ้ำทุกแถว (เจ้าของ 2026-09-08)
var resultLabel = map[string]string{"PASSED": "✅ PASSED", "NOT PASSED": "❌ NOT PASSED", "UNKNOWN": "-"}

func ValidResult(r string) bool { _, ok := resultLabel[r]; return ok }

// DisplayWidth ความกว้างที่ตัวอักษรกินใน terminal monospace — สระ/วรรณยุกต์ไทยลอยอยู่บนตัวหน้า (0 ช่อง)
// CJK/emoji กว้าง 2 ช่อง ที่เหลือ 1 ช่อง ใกล้เคียงกับที่ terminal วาดจริง ไม่ใช่ตามจำนวนตัวอักษร
var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

func DisplayWidth(text string) int {
	w := 0
	if strings.Contains(text, "\x1b[") {
		text = ansiRe.ReplaceAllString(text, "") // สี ANSI ไม่กินช่อง
	}
	for _, r := range text {
		switch {
		case r == 0x0e31, r >= 0x0e34 && r <= 0x0e3a, r >= 0x0e47 && r <= 0x0e4e:
		case r >= 0x0300 && r <= 0x036f, r == 0x200d, r >= 0xfe00 && r <= 0xfe0f:
		case r >= 0x1100 && r <= 0x115f, r >= 0x2e80 && r <= 0xa4cf, r >= 0xac00 && r <= 0xd7a3,
			r >= 0xf900 && r <= 0xfaff, r >= 0xfe30 && r <= 0xfe4f, r >= 0xff00 && r <= 0xff60,
			r >= 0xffe0 && r <= 0xffe6, r >= 0x1f300 && r <= 0x1faff, wideSymbol(r):
			w += 2
		default:
			w++
		}
	}
	return w
}

func PadEnd(text string, width int) string {
	if d := width - DisplayWidth(text); d > 0 {
		return text + strings.Repeat(" ", d)
	}
	return text
}

// WrapWidth ตัดข้อความให้แต่ละบรรทัดกว้างไม่เกิน max ตัดที่ช่องว่างก่อน คำเดียวที่ยาวเกิน (ไทยไม่มีช่องว่าง) ค่อยหั่นตามความกว้าง
func WrapWidth(text string, max int) []string {
	if max <= 0 || DisplayWidth(text) <= max {
		return []string{text}
	}
	var lines []string
	line := ""
	push := func(word string) {
		switch {
		case line == "":
			line = word
		case DisplayWidth(line+" "+word) <= max:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}
	for _, word := range strings.Split(text, " ") {
		if DisplayWidth(word) <= max {
			push(word)
			continue
		}
		piece := ""
		for _, r := range word {
			if DisplayWidth(piece+string(r)) > max && piece != "" {
				push(piece)
				piece = ""
			}
			piece += string(r)
		}
		if piece != "" {
			push(piece)
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

type Layout struct {
	Terminal string `json:"terminal"`
	Markdown string `json:"markdown"`
	Summary  string `json:"summary"`
}

// RenderCheck จัดหน้าผลตรวจให้สองปลายทาง (เจ้าของสั่ง 2026-09-08: script วัดความกว้าง monospace แล้วจัดคอลัมน์เอง)
//   - terminal: กลุ่มตาม topic · บรรทัดละ ชื่อ → ผล → ref · ชื่อยาวเกิน maxName ตัดขึ้นบรรทัดใหม่ indent 2 · UNKNOWN รวมท้าย
//   - markdown: ตารางสำหรับไฟล์รายงาน ref เป็นลิงก์ relative จาก reports/
func RenderCheck(rows []CheckRow, maxName int) Layout {
	if maxName <= 0 {
		maxName = 40
	}
	const gap = 3
	nameOf := func(r CheckRow) string {
		if r.Result == "UNKNOWN" {
			return r.Topic + " › " + r.Heading
		}
		return r.Heading
	}
	resW, refW, nameW := 0, 0, 0
	for _, r := range rows {
		resW = maxInt(resW, DisplayWidth(resultLabel[r.Result]))
		refW = maxInt(refW, DisplayWidth(r.Ref))
		nameW = maxInt(nameW, DisplayWidth(nameOf(r)))
	}
	_ = refW
	nameW = minInt(nameW, maxName)
	line := func(r CheckRow) string {
		parts := WrapWidth(nameOf(r), nameW)
		out := []string{"- " + PadEnd(parts[0], nameW+gap) + PadEnd(resultLabel[r.Result], resW+gap) + r.Ref}
		for _, p := range parts[1:] {
			out = append(out, "  "+p)
		}
		return strings.Join(out, "\n")
	}
	link := func(ref string) string { return "[" + ref + "](../../" + ref + ")" }

	var order []string
	groups := map[string][]CheckRow{}
	var unknown []CheckRow
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.Result]++
		if r.Result == "UNKNOWN" {
			unknown = append(unknown, r)
			continue
		}
		if _, ok := groups[r.Topic]; !ok {
			order = append(order, r.Topic)
		}
		groups[r.Topic] = append(groups[r.Topic], r)
	}
	var term []string
	md := []string{"| Business Logic | Result | Ref |", "| --- | --- | --- |"}
	for _, topic := range order {
		term = append(term, "**"+topic+"**")
		md = append(md, "| **"+topic+"** | | |")
		for _, r := range groups[topic] {
			term = append(term, line(r))
			md = append(md, "| - "+r.Heading+" | "+resultLabel[r.Result]+" | "+link(r.Ref)+" |")
		}
		term = append(term, "")
	}
	if len(unknown) > 0 {
		term = append(term, "**UNKNOWN**")
		md = append(md, "| **UNKNOWN** | | |")
		for _, r := range unknown {
			term = append(term, line(r))
			md = append(md, "| - "+r.Topic+" › "+r.Heading+" | - | "+link(r.Ref)+" |")
		}
	}
	return Layout{
		Terminal: strings.TrimRight(strings.Join(term, "\n"), "\n"),
		Markdown: strings.Join(md, "\n"),
		Summary:  fmt.Sprintf("`Summary` ✅ PASSED %d · ❌ NOT PASSED %d · UNKNOWN %d", counts["PASSED"], counts["NOT PASSED"], counts["UNKNOWN"]),
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ---- terminal ui (เจ้าของ 2026-09-09: `blm status` ให้หน้าตาแบบ `rtk gain` — หัวสีเขียว เส้นคู่ key/value ตรงคอลัมน์ ตารางมีหัว) ----

// Color เปิดเมื่อ stdout เป็น terminal จริง (CLI ตั้งให้) · MCP/ไฟล์ = plain text เสมอ
var Color = false

func paint(code, s string) string {
	if !Color || s == "" {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}

func Green(s string) string { return paint("1;32", s) }
func Red(s string) string   { return paint("1;31", s) }
func Cyan(s string) string  { return paint("1;36", s) }
func Dim(s string) string   { return paint("2", s) }

const ruleWidth = 72

// Title หัวเรื่องสีเขียว + เส้นคู่
func Title(s string) string { return Green(s) + "\n" + strings.Repeat("═", ruleWidth) }

// Section หัวข้อย่อยสีเขียว + เส้นเดี่ยว
func Section(s string) string { return "\n" + Green(s) + "\n" + strings.Repeat("─", ruleWidth) }

// KV จัด "Key:   value" ให้ค่าเริ่มคอลัมน์เดียวกัน (วัดด้วย DisplayWidth)
func KV(rows [][2]string) string {
	w := 0
	for _, r := range rows {
		w = maxInt(w, DisplayWidth(r[0]+":"))
	}
	var out []string
	for _, r := range rows {
		out = append(out, PadEnd(r[0]+":", w+2)+r[1])
	}
	return strings.Join(out, "\n")
}

// Table หัวตาราง + แถว จัดทุกคอลัมน์ด้วย DisplayWidth · คอลัมน์สุดท้ายไม่ pad
func Table(header []string, rows [][]string) string {
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = DisplayWidth(h)
	}
	for _, r := range rows {
		for i := range header {
			if i < len(r) {
				widths[i] = maxInt(widths[i], DisplayWidth(r[i]))
			}
		}
	}
	line := func(cells []string, dim bool) string {
		var parts []string
		for i := range header {
			c := ""
			if i < len(cells) {
				c = cells[i]
			}
			if i == len(header)-1 {
				parts = append(parts, c)
			} else {
				parts = append(parts, PadEnd(c, widths[i]+2))
			}
		}
		s := "  " + strings.Join(parts, "")
		if dim {
			return Dim(s)
		}
		return s
	}
	out := []string{line(header, true)}
	for _, r := range rows {
		out = append(out, line(r, false))
	}
	return strings.Join(out, "\n")
}

// wideSymbol เฉพาะตัวใน U+2600–27BF ที่ terminal วาดเป็น emoji กว้าง 2 (✅ ❌ ⚡ …) — ✔ ✘ ✓ และเพื่อนเป็นตัวอักษรกว้าง 1
// (ก่อนหน้านับทั้งช่วงเป็น 2 ทำให้แถวที่มี ✔ เลื่อนไปหนึ่งช่อง เจ้าของเห็น 2026-09-09)
func wideSymbol(r rune) bool {
	switch {
	case r == 0x2614, r == 0x2615, r >= 0x2648 && r <= 0x2653, r == 0x267f, r == 0x2693, r == 0x26a1,
		r == 0x26aa, r == 0x26ab, r == 0x26bd, r == 0x26be, r == 0x26c4, r == 0x26c5, r == 0x26ce, r == 0x26d4,
		r == 0x26ea, r == 0x26f2, r == 0x26f3, r == 0x26f5, r == 0x26fa, r == 0x26fd, r == 0x2705, r == 0x270a,
		r == 0x270b, r == 0x2728, r == 0x274c, r == 0x274e, r >= 0x2753 && r <= 0x2755, r == 0x2757,
		r >= 0x2795 && r <= 0x2797, r == 0x27b0, r == 0x27bf:
		return true
	}
	return false
}
