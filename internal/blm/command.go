package blm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// blm_review — แตะไฟล์ใน store ของ blm เอง (review json/html ฯลฯ) ผ่านโปรเซส blm (เจ้าของกำหนด 2026-09-21)
//
// ทำไม: sandbox ของ Claude Code ปิด Bash ไม่ให้เขียน `.agentsroom/blm/**` (กำแพงกัน agent แก้ store ด้วยมือ)
// แต่กรณีอย่าง "เปิด review ที่ถูกปั๊ม done ผิดกลับมา" หรือ "ลบไฟล์ review เปล่าที่เกิดจากบั๊ก" ต้องแตะไฟล์ตรง ๆ
// blm (MCP server / CLI ของเจ้าของ) รันนอก sandbox จึงเป็นทางเดียว — ขอบเขตแค่ใต้ store เท่านั้น path นอกนั้นปฏิเสธ
// กำหนดเฉพาะบางคำสั่ง: get (review ตาม id) · list · read · write · set (แก้จุดเดียวใน json ไฟล์ที่เหลือไม่ผ่าน context) · delete (ต้องผ่านการยืนยันของเจ้าของ)
//   path รับได้ทั้ง path ใต้ store, path ต่อโปรเจ็ค หรือ id ของ review (980fe083 → reviews/review-980fe083.json)
//   delete ไม่มี confirm → ไม่ลบ คืน needConfirm ให้ agent ไปถามเจ้าของในแชตก่อน; confirm = ชื่อไฟล์ตรงตัว
//   ลบ = ย้ายไป <store>/.trash/<เวลา>-<ชื่อไฟล์> ไม่ลบจริง กู้คืนได้เสมอ (กันกรณี agent ยืนยันเองโดยที่เจ้าของไม่ได้ตอบ)
// ทุกครั้งจด <store>/commands.log (เวลา · action · path · confirm · ผล) ให้ย้อนดูได้ว่า agent สั่งอะไรไป

var reviewIDRe = regexp.MustCompile(`^[0-9a-f]{8}$`)

type CommandOpts struct {
	Action  string // get | list | read | write | set | patch | delete
	Path    string // relative ต่อ store (หรือ absolute ที่อยู่ใต้ store)
	Content string // write
	Confirm string // delete: ชื่อไฟล์ (base name) ที่เจ้าของยืนยัน
	Select  string // get/read: path เข้าไปใน JSON (SelectJSON) — คืนเฉพาะจุดนั้น · set: จุดที่จะแก้
	Value   any    // set: ค่าใหม่ (scalar/object/array) · nil = ลบ key/element
	Line    int    // patch: บรรทัดแรก (1-based) ที่จะแทน — ได้จาก blm_grep / blm_search (start/end)
	EndLine int    // patch: บรรทัดสุดท้าย (รวม) · 0 = Line
	Insert  bool   // patch: แทรก Content ก่อนบรรทัด Line โดยไม่ลบอะไร (Line = จำนวนบรรทัด+1 = ต่อท้าย)
}

type CommandResult struct {
	OK          bool     `json:"ok"`
	Action      string   `json:"action"`
	Path        string   `json:"path"` // relative ต่อโปรเจ็ค
	Bytes       int      `json:"bytes,omitempty"`
	Content     string   `json:"content,omitempty"`
	Data        any      `json:"data,omitempty"`  // get/read + select: จุดที่เลือก
	Files       []string `json:"files,omitempty"` // list: ชื่อไฟล์ในโฟลเดอร์ (โฟลเดอร์ย่อยลงท้าย /)
	NeedConfirm bool     `json:"needConfirm,omitempty"`
	Trash       string   `json:"trash,omitempty"`
	Next        string   `json:"next,omitempty"`
}

// storePath แปลง path ที่รับมาให้เป็น absolute ใต้ store · นอก store = error
func (s *Store) storePath(p string, allowRoot bool) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("path required (relative to %s)", s.rel(s.Dir))
	}
	if reviewIDRe.MatchString(p) { // id ของ review
		p = "reviews/review-" + p + ".json"
	}
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(s.Dir, p)
		// เผื่อส่ง path relative ต่อโปรเจ็ค (.agentsroom/blm/...) มา
		if r, err := filepath.Rel(s.Root, filepath.Join(s.Root, p)); err == nil && strings.HasPrefix(r, s.rel(s.Dir)+string(filepath.Separator)) {
			abs = filepath.Join(s.Root, p)
		}
	}
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(s.Dir, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || (rel == "." && !allowRoot) {
		return "", fmt.Errorf("blm_review only touches files under the store %s — refused: %s", s.rel(s.Dir), p)
	}
	return abs, nil
}

func (s *Store) logCommand(o CommandOpts, result string) {
	_ = os.MkdirAll(s.Dir, 0o755)
	line := fmt.Sprintf("%s\t%s\t%s\tconfirm=%q\t%s\n", time.Now().Format(time.RFC3339), o.Action, o.Path, o.Confirm, result)
	if f, err := os.OpenFile(filepath.Join(s.Dir, "commands.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		_, _ = f.WriteString(line)
		_ = f.Close()
	}
}

func (s *Store) Command(o CommandOpts) (CommandResult, error) {
	abs, err := s.storePath(o.Path, o.Action == "list")
	if err != nil {
		s.logCommand(o, "refused: "+err.Error())
		return CommandResult{}, err
	}
	res := CommandResult{Action: o.Action, Path: s.rel(abs)}
	switch o.Action {
	case "write":
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return res, err
		}
		if err := os.WriteFile(abs, []byte(o.Content), 0o644); err != nil {
			s.logCommand(o, "error: "+err.Error())
			return res, err
		}
		res.OK, res.Bytes = true, len(o.Content)
		s.logCommand(o, fmt.Sprintf("ok %d bytes", res.Bytes))
	case "read", "get":
		raw, err := os.ReadFile(abs)
		if err != nil {
			return res, err
		}
		res.OK, res.Bytes = true, len(raw)
		if o.Select != "" { // เฉพาะจุดที่ขอ ไม่ใช่ทั้งไฟล์
			if res.Data, err = SelectRaw(raw, o.Select); err != nil {
				return res, err
			}
			break
		}
		res.Content = string(raw)
	case "set":
		if o.Select == "" {
			return res, fmt.Errorf("set: select required (e.g. status · topics[id=x].status)")
		}
		raw, err := os.ReadFile(abs)
		if err != nil {
			return res, err
		}
		var root any
		if err := json.Unmarshal(raw, &root); err != nil {
			return res, err
		}
		if err := SetJSON(root, o.Select, o.Value); err != nil {
			s.logCommand(o, "error: "+err.Error())
			return res, err
		}
		out, _ := json.MarshalIndent(root, "", "  ")
		out = append(out, '\n')
		if err := os.WriteFile(abs, out, 0o644); err != nil {
			return res, err
		}
		res.OK, res.Bytes, res.Next = true, len(out), "set "+o.Select // ไม่สะท้อน value กลับ (ผู้เรียกมีอยู่แล้ว)
		s.logCommand(o, "ok set "+o.Select)
	case "patch": // แทนช่วงบรรทัด — ไฟล์ที่เหลือไม่ผ่าน context (เจ้าของ 2026-09-21: ใช้ start/end จาก grep/search แทน read all + write all)
		raw, err := os.ReadFile(abs)
		if err != nil {
			return res, err
		}
		lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
		end := o.EndLine
		if end == 0 {
			end = o.Line
		}
		if o.Insert {
			end = o.Line - 1
		}
		if o.Line < 1 || o.Line > len(lines)+1 || end < o.Line-1 || end > len(lines) {
			return res, fmt.Errorf("patch: line %d–%d outside 1–%d", o.Line, end, len(lines))
		}
		var repl []string
		if o.Content != "" {
			repl = strings.Split(strings.TrimSuffix(o.Content, "\n"), "\n")
		}
		out := append(append(append([]string{}, lines[:o.Line-1]...), repl...), lines[end:]...)
		if err := os.WriteFile(abs, []byte(strings.Join(out, "\n")+"\n"), 0o644); err != nil {
			return res, err
		}
		res.OK, res.Bytes = true, len(out)
		res.Next = fmt.Sprintf("lines %d–%d → %d lines · file now %d lines", o.Line, end, len(repl), len(out))
		s.logCommand(o, "ok "+res.Next)
	case "list":
		ents, err := os.ReadDir(abs)
		if err != nil {
			return res, err
		}
		res.OK, res.Files = true, []string{}
		for _, e := range ents {
			n := e.Name()
			if e.IsDir() {
				n += "/"
			}
			res.Files = append(res.Files, n)
		}
	case "delete":
		if _, err := os.Stat(abs); err != nil {
			return res, err
		}
		name := filepath.Base(abs)
		if o.Confirm != name {
			res.NeedConfirm = true
			res.Next = fmt.Sprintf("not deleted — ask the owner in chat: delete %s? then call again with confirm:%q (moved to %s/.trash, recoverable)", res.Path, name, s.rel(s.Dir))
			s.logCommand(o, "needConfirm")
			return res, nil
		}
		trash := filepath.Join(s.Dir, ".trash", time.Now().Format("20060102-150405")+"-"+name)
		if err := os.MkdirAll(filepath.Dir(trash), 0o755); err != nil {
			return res, err
		}
		if err := os.Rename(abs, trash); err != nil {
			s.logCommand(o, "error: "+err.Error())
			return res, err
		}
		res.OK, res.Trash = true, s.rel(trash)
		s.logCommand(o, "ok → "+res.Trash)
	default:
		return res, fmt.Errorf("action must be get | list | read | write | set | patch | delete")
	}
	return res, nil
}
