package blm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
)

// ast-grep (เครื่องยนต์ tree-sitter) สำหรับ blm_graph — เจ้าของกำหนด 2026-09-09: วิเคราะห์ด้วย AST จริง ไม่ใช่ regex
// blm shell-out ไปที่ binary `ast-grep`/`sg` (ติดตั้งด้วย `blm tools install tree-sitter`) แล้วอ่าน --json=stream
// ได้ 3 อย่างต่อไฟล์: import (ปลายทาง), definition (ชื่อ symbol ระดับบน), call (ชื่อฟังก์ชันที่ถูกเรียก)
// call ที่ชื่อตรงกับ definition ในไฟล์อื่น = edge kind "call" ซึ่ง regex ทำไม่ได้

// AstGrepBin path ของ ast-grep หรือ "" เมื่อไม่มี
func AstGrepBin() string {
	for _, b := range []string{"ast-grep", "sg"} {
		if p, err := exec.LookPath(b); err == nil {
			// `sg` บน Linux อาจเป็น shadow-utils (setgroups) — ตรวจว่าเป็น ast-grep จริง
			if b == "sg" {
				out, _ := exec.Command(p, "--version").CombinedOutput()
				if !bytes.Contains(bytes.ToLower(out), []byte("ast-grep")) {
					continue
				}
			}
			return p
		}
	}
	return ""
}

// astLang ภาษาของ ast-grep ต่อนามสกุล (เฉพาะที่มี pattern ด้านล่าง)
func astLang(rel string) string {
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".ts":
		return "ts"
	case ".tsx":
		return "tsx"
	case ".js", ".mjs", ".cjs":
		return "js"
	case ".jsx":
		return "jsx"
	case ".go":
		return "go"
	case ".py":
		return "py"
	case ".rs":
		return "rs"
	case ".java":
		return "java"
	case ".kt":
		return "kt"
	case ".php":
		return "php"
	case ".rb":
		return "rb"
	case ".cs":
		return "cs"
	}
	return ""
}

type astRule struct {
	kind    string // import | def | call
	pattern string
	meta    string // metavariable ที่ต้องดึง
}

// astRules pattern ต่อภาษา (ภาษาที่ tree-sitter รองรับผ่าน ast-grep) — $NAME / $SRC / $FN คือ metavariable
var astRules = map[string][]astRule{
	"ts": {
		{"import", "import $$$A from '$SRC'", "SRC"}, {"import", "import '$SRC'", "SRC"}, {"import", "export $$$A from '$SRC'", "SRC"},
		{"import", "require('$SRC')", "SRC"}, {"import", "import('$SRC')", "SRC"},
		{"def", "function $NAME($$$) { $$$ }", "NAME"}, {"def", "async function $NAME($$$) { $$$ }", "NAME"},
		{"def", "class $NAME { $$$ }", "NAME"}, {"def", "class $NAME extends $$$B { $$$ }", "NAME"},
		{"def", "export const $NAME = $$$", "NAME"}, {"def", "export function $NAME($$$) { $$$ }", "NAME"},
		{"def", "export async function $NAME($$$) { $$$ }", "NAME"}, {"def", "export interface $NAME { $$$ }", "NAME"}, {"def", "export type $NAME = $$$", "NAME"},
		{"call", "$FN($$$)", "FN"},
	},
	"go": {
		{"def", "func $NAME($$$) { $$$ }", "NAME"}, {"def", "func $NAME($$$) $RET { $$$ }", "NAME"},
		{"def", "func ($R) $NAME($$$) { $$$ }", "NAME"}, {"def", "func ($R) $NAME($$$) $RET { $$$ }", "NAME"},
		{"def", "type $NAME struct { $$$ }", "NAME"}, {"def", "type $NAME interface { $$$ }", "NAME"},
		{"call", "$FN($$$)", "FN"}, {"call", "$PKG.$FN($$$)", "FN"},
	},
	"py": {
		{"import", "import $SRC", "SRC"}, {"import", "from $SRC import $$$A", "SRC"},
		{"def", "def $NAME($$$): $$$", "NAME"}, {"def", "class $NAME: $$$", "NAME"}, {"def", "class $NAME($$$B): $$$", "NAME"},
		{"call", "$FN($$$)", "FN"},
	},
	"rs":   {{"def", "fn $NAME($$$) { $$$ }", "NAME"}, {"def", "pub fn $NAME($$$) { $$$ }", "NAME"}, {"def", "struct $NAME { $$$ }", "NAME"}, {"call", "$FN($$$)", "FN"}},
	"java": {{"def", "class $NAME { $$$ }", "NAME"}, {"call", "$FN($$$)", "FN"}},
	"php":  {{"def", "function $NAME($$$) { $$$ }", "NAME"}, {"def", "class $NAME { $$$ }", "NAME"}, {"call", "$FN($$$)", "FN"}},
	"rb":   {{"def", "def $NAME($$$) $$$ end", "NAME"}, {"def", "class $NAME $$$ end", "NAME"}, {"call", "$FN($$$)", "FN"}},
}

func init() {
	astRules["tsx"], astRules["js"], astRules["jsx"] = astRules["ts"], astRules["ts"], astRules["ts"]
	astRules["kt"], astRules["cs"] = astRules["java"], astRules["java"]
}

type astMatch struct {
	File string `json:"file"`
	Meta struct {
		Single map[string]struct {
			Text string `json:"text"`
		} `json:"single"`
	} `json:"metaVariables"`
	Range struct {
		Start struct {
			Line int `json:"line"`
		} `json:"start"`
	} `json:"range"`
}

// AstFacts ผลสแกนของไฟล์เดียว
type AstFacts struct {
	Imports []string
	Defs    []string
	Calls   []string
}

// RunAstGrep สแกนไฟล์ทั้งหมด (จัดกลุ่มตามภาษา รันหนึ่งครั้งต่อ pattern ต่อภาษา) คืน facts ต่อ path relative
func RunAstGrep(bin, root string, files []string) map[string]*AstFacts {
	byLang := map[string][]string{}
	for _, f := range files {
		if l := astLang(f); l != "" {
			byLang[l] = append(byLang[l], f)
		}
	}
	facts := map[string]*AstFacts{}
	get := func(rel string) *AstFacts {
		if facts[rel] == nil {
			facts[rel] = &AstFacts{}
		}
		return facts[rel]
	}
	for lang, group := range byLang {
		for _, rule := range astRules[lang] {
			args := append([]string{"run", "--pattern", rule.pattern, "--lang", lang, "--json=stream"}, group...)
			cmd := exec.Command(bin, args...)
			cmd.Dir = root
			out, err := cmd.Output()
			if err != nil && len(out) == 0 {
				continue
			}
			sc := bufio.NewScanner(bytes.NewReader(out))
			sc.Buffer(make([]byte, 1<<20), 64<<20)
			seen := map[string]bool{}
			for sc.Scan() {
				var m astMatch
				if json.Unmarshal(sc.Bytes(), &m) != nil {
					continue
				}
				v := m.Meta.Single[rule.meta].Text
				if v == "" {
					continue
				}
				rel := filepath.ToSlash(m.File)
				key := rel + "|" + rule.kind + "|" + v
				if seen[key] {
					continue
				}
				seen[key] = true
				f := get(rel)
				switch rule.kind {
				case "import":
					f.Imports = append(f.Imports, v)
				case "def":
					f.Defs = append(f.Defs, v)
				case "call":
					f.Calls = append(f.Calls, v)
				}
			}
		}
	}
	return facts
}
