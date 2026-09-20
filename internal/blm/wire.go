package blm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ต่อสาย Claude Code ให้ SocratiCode index ผ่าน blm — รันท้าย `blm tools install socraticode` ทั้ง --local และ --remote
// (เจ้าของ 2026-09-20: install ต้องพ่วง hooks + statusline และแก้ ~/.claude/settings.json ให้เอง)
//
//  1. ~/.claude/settings.json "hooks": SessionStart / UserPromptSubmit / PostToolUse(Grep|Glob) → `blm hook …`
//     ถอดเฉพาะของเราเอง (blm hook รุ่นก่อน, socraticode-hooks.cjs ที่ blm เคยให้วาง) แล้วใส่ใหม่ — idempotent
//     ของคนอื่นที่ทำหน้าที่เดียวกัน (graft) **ไม่แตะ** hooks อยู่ร่วมกันได้ แค่บอกวิธีถอดเอง (เจ้าของ 2026-09-20)
//  2. ~/.claude/statusline-command.sh: มีอยู่ → ลบ block ของเรา (ถ้ามี) แล้วต่อท้ายใหม่ · ไม่มี → เขียนสคริปต์ค่าเริ่มต้นที่ฝังมา
//     (statusline ของเจ้าของ: เวลา/โฟลเดอร์/git/โมเดล/ctx เทียบ auto-compact + บรรทัด socraticode)
//  3. settings.json "statusLine" ชี้สคริปต์นั้น เมื่อยังไม่มี หรือชี้ socraticode-statusline.cjs ของเราเดิม — statusLine อื่น (graft, ของผู้ใช้) ไม่แตะ แค่บอก

const (
	claudeStatuslineFile = ".claude/statusline-command.sh"
	slBlockBegin         = "# >>> blm socraticode statusline >>>"
	slBlockEnd           = "# <<< blm socraticode statusline <<<"
	slBlock              = slBlockBegin + `
sc=$(echo "$input" | blm statusline --top-only 2>/dev/null)
[ -n "$sc" ] && printf '\n%s' "$sc"
` + slBlockEnd + "\n"
)

// hook ที่เราเป็นเจ้าของ (ถอดก่อนใส่): blm เอง และ socraticode-hooks.cjs ที่ blm เคยให้วางก่อนมี `blm hook`
var ownedHookMarks = []string{"blm hook ", "socraticode-hooks.cjs"}

// hook ของคนอื่นที่ทำหน้าที่เดียวกัน — ไม่ถอด แต่เตือน
var rivalHookMarks = map[string]string{"graft-hooks.cjs": "graft hooks still installed — you now get both; remove them with: graft uninstall -y"}

type claudeHook struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

// WireClaude ทำทั้ง 3 ข้อ · คืนบรรทัดรายงานให้ต่อท้ายผล install
func WireClaude() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return []string{"claude: cannot resolve home: " + err.Error()}
	}
	var lines []string
	lines = append(lines, wireStatuslineScript(home)...)
	lines = append(lines, wireSettings(home)...)
	return lines
}

func wireStatuslineScript(home string) []string {
	file := filepath.Join(home, claudeStatuslineFile)
	raw, err := os.ReadFile(file)
	if err != nil {
		def, _ := scripts.ReadFile("scripts/statusline-command.sh")
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return []string{"statusline: cannot create " + filepath.Dir(file) + ": " + err.Error()}
		}
		if err := os.WriteFile(file, append(def, []byte("\n"+slBlock)...), 0o755); err != nil {
			return []string{"statusline: cannot write " + file + " (" + err.Error() + ") — copy it by hand: blm statusline --help"}
		}
		return []string{"statusline: wrote default " + file + " (time · dir · git · model · ctx + socraticode line)"}
	}
	body := stripStatuslineBlock(string(raw))
	body = strings.TrimRight(body, "\n") + "\n\n" + slBlock
	if err := os.WriteFile(file, []byte(body), 0o755); err != nil {
		return []string{"statusline: cannot write " + file + " (" + err.Error() + ") — append by hand:\n" + slBlock}
	}
	return []string{"statusline: socraticode block refreshed at the end of " + file}
}

// stripStatuslineBlock ลบ block ที่มี marker และบรรทัด sc= / [ -n "$sc" ] รุ่นก่อน marker (ที่เคยให้เจ้าของวางมือ)
func stripStatuslineBlock(s string) string {
	var out []string
	skip := false
	for _, l := range strings.Split(s, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case t == slBlockBegin:
			skip = true
			continue
		case t == slBlockEnd:
			skip = false
			continue
		case skip:
			continue
		case strings.HasPrefix(t, "sc=$(") && (strings.Contains(t, "blm statusline") || strings.Contains(t, "socraticode-statusline")):
			continue
		case strings.HasPrefix(t, `[ -n "$sc" ]`):
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

func wireSettings(home string) []string {
	file := filepath.Join(home, ".claude", "settings.json")
	settings := map[string]any{}
	if raw, err := os.ReadFile(file); err == nil {
		if err := json.Unmarshal(raw, &settings); err != nil {
			return []string{"claude: " + file + " is not valid JSON (" + err.Error() + ") — hooks/statusline not touched"}
		}
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	// event → hook ที่ต้องมี (PostToolUse มี 2 กลุ่มคนละ matcher)
	type hookSpec struct {
		matcher, cmd string
		timeout      int
	}
	want := map[string][]hookSpec{
		"SessionStart":     {{"", "blm hook session-start", 8000}},
		"UserPromptSubmit": {{"", "blm hook prompt", 15000}},
		"PostToolUse":      {{"Grep|Glob", "blm hook grep-nudge", 8000}, {"Write|Edit|MultiEdit", "blm hook post-edit", 8000}},
	}
	rivals := map[string]bool{}
	for ev, specs := range want {
		groups, _ := hooks[ev].([]any)
		var kept []any
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			if gm == nil {
				continue
			}
			hs, _ := gm["hooks"].([]any)
			var keptHooks []any
			for _, h := range hs {
				hm, _ := h.(map[string]any)
				cmd, _ := hm["command"].(string)
				if ownedHook(cmd) {
					continue
				}
				for mark, advice := range rivalHookMarks {
					if strings.Contains(cmd, mark) {
						rivals[advice] = true
					}
				}
				keptHooks = append(keptHooks, h)
			}
			if len(keptHooks) > 0 {
				gm["hooks"] = keptHooks
				kept = append(kept, gm)
			}
		}
		for _, w := range specs {
			group := map[string]any{"hooks": []any{claudeHook{Type: "command", Command: w.cmd, Timeout: w.timeout}}}
			if w.matcher != "" {
				group["matcher"] = w.matcher
			}
			kept = append(kept, group)
		}
		hooks[ev] = kept
	}
	settings["hooks"] = hooks
	lines := []string{"claude: hooks → blm hook session-start / prompt / post-edit (Write|Edit) / grep-nudge (Grep|Glob) in " + file}
	for advice := range rivals {
		lines = append(lines, "note: "+advice)
	}

	// statusLine: ชี้สคริปต์ของเราเมื่อยังไม่มี หรือยังชี้ socraticode-statusline.cjs ของเราเดิม · ของคนอื่นไม่แตะ
	slCmd := "~/" + claudeStatuslineFile
	for _, key := range []string{"statusLine", "subagentStatusLine"} {
		cur, _ := settings[key].(map[string]any)
		curCmd, _ := cur["command"].(string)
		switch {
		case curCmd == "" || strings.Contains(curCmd, "socraticode-statusline"):
			settings[key] = map[string]any{"type": "command", "command": slCmd}
			lines = append(lines, "claude: "+key+" → "+slCmd)
		case strings.Contains(curCmd, "statusline-command.sh"):
			// ของเรา/ของเจ้าของอยู่แล้ว
		case strings.Contains(curCmd, "graft-statusline"):
			lines = append(lines, "note: "+key+" still points at graft ("+curCmd+") — switch to "+slCmd+" after: graft uninstall -y")
		default:
			lines = append(lines, "claude: "+key+" left as is ("+curCmd+") — add the socraticode line yourself: blm statusline --help")
		}
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return []string{"claude: cannot create " + filepath.Dir(file) + ": " + err.Error()}
	}
	raw, _ := json.MarshalIndent(settings, "", "  ")
	if err := os.WriteFile(file, append(raw, '\n'), 0o644); err != nil {
		return []string{fmt.Sprintf("claude: cannot write %s (%v) — paste this into \"hooks\":\n%s", file, err, hookSnippet())}
	}
	return lines
}

func ownedHook(cmd string) bool {
	for _, m := range ownedHookMarks {
		if strings.Contains(cmd, m) {
			return true
		}
	}
	return false
}

// hookSnippet ข้อความให้ก๊อปเมื่อเขียน settings.json ไม่ได้ (sandbox)
func hookSnippet() string {
	return `"SessionStart":     [ { "hooks": [ { "type": "command", "command": "blm hook session-start", "timeout": 8000 } ] } ],
"UserPromptSubmit": [ { "hooks": [ { "type": "command", "command": "blm hook prompt", "timeout": 15000 } ] } ],
"PostToolUse":      [ { "matcher": "Grep|Glob", "hooks": [ { "type": "command", "command": "blm hook grep-nudge", "timeout": 8000 } ] },
                      { "matcher": "Write|Edit|MultiEdit", "hooks": [ { "type": "command", "command": "blm hook post-edit", "timeout": 8000 } ] } ]`
}
