// Package cli — คำสั่งฝั่งติดตั้ง: init (config/store/settings/hook/plugin/PATH), guard (hook), path
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Ekkapap/business-logic-memory/internal/blm"
)

const (
	PluginRepo  = "Ekkapap/business-logic-memory"
	PluginName  = "blm"
	oldStoreDir = ".agentsroom/memory-temp" // ชื่อเดิมก่อน rename เป็น blm (2026-09-09) — init ย้ายให้
	oldRules    = "project-business-logic"
)

type InitOptions struct {
	Root        string
	Backend     blm.Backend
	Store       string
	CustomCLI   string
	Plugin      bool
	Sandbox     bool
	Marketplace string
	// Tools ที่เลือก (--tools a,b) · nil = ตรวจเองว่าตัวไหนติดตั้งอยู่ · ตัวที่เลือกแล้วยังไม่ติดตั้ง init ติดตั้งให้ (เจ้าของ 2026-09-09)
	Tools []string
	// Docker = socraticode แบบ Docker (Windows บังคับ)
	Docker bool
	// NoInstall = ตั้ง config แต่ไม่รัน script ติดตั้งเครื่องมือ (ใช้ใน test / เครื่องที่ห้ามติดตั้ง)
	NoInstall bool
	// Force = ข้ามการตรวจว่า root เป็นโปรเจ็คจริง
	Force bool
	// BackendSet = ผู้ใช้ระบุ backend เอง · false + มี config อยู่แล้ว = ใช้ของเดิม (รัน install ซ้ำต้องไม่เปลี่ยน backend เงียบ ๆ)
	BackendSet bool
	// Out สำหรับ stream output ของ script ติดตั้ง (nil = ไม่พิมพ์)
	Out io.Writer
	// Run แยกไว้ให้ test แทนได้
	Run func(cmd ...string) (string, error)
}

func ParseInit(argv []string) (InitOptions, error) {
	wd, _ := os.Getwd()
	o := InitOptions{Root: wd, Backend: blm.BackendNone, Plugin: true, Marketplace: PluginRepo}
	next := func(i *int) string {
		*i++
		if *i < len(argv) {
			return argv[*i]
		}
		return ""
	}
	for i := 0; i < len(argv); i++ {
		switch a := argv[i]; a {
		case "--agentsroom":
			o.Backend, o.BackendSet = blm.BackendAgentsRoom, true
		case "--obsidian":
			o.Backend, o.BackendSet = blm.BackendObsidian, true
		case "--dir":
			o.Store = next(&i)
		case "--backend":
			o.CustomCLI = next(&i)
			o.Backend, o.BackendSet = blm.BackendCustom, true
		case "--docker":
			o.Docker = true
		case "--force":
			o.Force = true
		case "--no-plugin":
			o.Plugin = false
		case "--no-install":
			o.NoInstall = true
		case "--sandbox":
			o.Sandbox = true
		case "--marketplace":
			o.Marketplace = next(&i)
		case "--tools":
			o.Tools = []string{}
			for _, t := range strings.Split(next(&i), ",") {
				if t = strings.TrimSpace(t); t != "" {
					o.Tools = append(o.Tools, t)
				}
			}
		case "--root":
			o.Root, _ = filepath.Abs(next(&i))
		default:
			if strings.HasPrefix(a, "-") {
				return o, fmt.Errorf("unknown option %s", a)
			}
			o.Root, _ = filepath.Abs(a)
		}
	}
	if o.Backend == blm.BackendCustom && o.Store == "" {
		return o, fmt.Errorf("--backend <cli> requires --dir <path>")
	}
	return o, nil
}

func readJSON(file string) map[string]any {
	m := map[string]any{}
	if raw, err := os.ReadFile(file); err == nil {
		_ = json.Unmarshal(raw, &m)
	}
	return m
}

func writeJSON(file string, v any) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	raw, _ := json.MarshalIndent(v, "", "  ")
	return os.WriteFile(file, append(raw, '\n'), 0o644)
}

func sub(m map[string]any, k string) map[string]any {
	if v, ok := m[k].(map[string]any); ok {
		return v
	}
	v := map[string]any{}
	m[k] = v
	return v
}

func strList(m map[string]any, k string) []string {
	raw, _ := m[k].([]any)
	var out []string
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// mergeList ตัดรายการเก่าที่เกี่ยวกับ memory-temp/project-business-logic แล้วเติมของใหม่แบบไม่ซ้ำ
func mergeList(cur []string, add ...string) []any {
	var out []any
	seen := map[string]bool{}
	for _, s := range cur {
		if strings.Contains(s, "memory-temp") || strings.Contains(s, oldRules) || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	for _, s := range add {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// projectMarkers ร่องรอยว่าโฟลเดอร์นี้คือ root ของโปรเจ็ค — init ต้องรันในโปรเจ็ค ไม่ใช่ $HOME หรือโฟลเดอร์สุ่ม (เจ้าของ 2026-09-09)
var projectMarkers = []string{".git", ".agentsroom", ".obsidian", "CLAUDE.md", "package.json", "go.mod", "pyproject.toml", "Cargo.toml", "pom.xml", "composer.json", "Gemfile", "*.sln"}

func looksLikeProject(root string) bool {
	for _, m := range projectMarkers {
		if strings.Contains(m, "*") {
			if hits, _ := filepath.Glob(filepath.Join(root, m)); len(hits) > 0 {
				return true
			}
			continue
		}
		if exists(filepath.Join(root, m)) {
			return true
		}
	}
	return false
}

// failure หนึ่งขั้นที่ล้ม พร้อมผลกระทบและสิ่งที่เจ้าของควรทำ (เจ้าของ 2026-09-09: error แล้วไม่หยุด ทำต่อจนจบ แล้วสรุปตอนท้าย)
type failure struct{ step, err, impact, action string }

func Init(o InitOptions) []string {
	var log []string
	var failures []failure
	say := func(line string) {
		log = append(log, line)
		if o.Out != nil {
			fmt.Fprintln(o.Out, line)
		}
	}
	// step พิมพ์ "installing: <name> …" ก่อนทำ แล้ว "  ✔ done" หรือ "  ✘ error … (continuing)" — ไม่หยุด process
	step := func(name, impact, action string, fn func() (string, error)) {
		if o.Out != nil {
			fmt.Fprintf(o.Out, "installing: %s …\n", name)
		}
		detail, err := fn()
		if err != nil {
			failures = append(failures, failure{name, err.Error(), impact, action})
			say("  ✘ " + name + " error: " + err.Error() + " (continuing)")
			return
		}
		say("  ✔ " + name + " done" + map[bool]string{true: "  " + detail, false: ""}[detail != ""])
	}
	if !o.Force && !looksLikeProject(o.Root) {
		return []string{
			"stop     " + o.Root + " does not look like a project root (no .git, .agentsroom, package.json, go.mod, CLAUDE.md, …)",
			"         blm must be initialised inside the project it will remember: cd <your-project> && blm init …",
			"         to initialise here anyway: blm init --force",
		}
	}
	c := blm.Config{Backend: blm.BackendCustom, Store: o.Store}
	if o.Backend != blm.BackendCustom {
		c = blm.Presets[o.Backend]
		if o.Store != "" {
			c.Store = o.Store
		}
	}
	if !o.BackendSet {
		switch {
		case exists(filepath.Join(o.Root, blm.ConfigFile)):
			existing := blm.Load(o.Root)
			c = existing // รันซ้ำโดยไม่ระบุ backend = คงของเดิมทั้ง backend/store/mirror/tools
			if o.Tools == nil {
				o.Tools = existing.Tools
			}
			say("config   existing " + blm.ConfigFile + " kept (backend " + string(c.Backend) + ") — pass --agentsroom/--obsidian/--dir to change")
		// ไม่ระบุ backend = ดูโปรเจ็คก่อน (เจ้าของ 2026-09-09): มี AgentsRoom → agentsroom · เป็น vault → obsidian · ไม่งั้น none
		case exists(filepath.Join(o.Root, blm.Presets[blm.BackendAgentsRoom].Mirror)) || exists(filepath.Join(o.Root, ".agentsroom")):
			c = blm.Presets[blm.BackendAgentsRoom]
			say("backend  agentsroom auto-detected (.agentsroom/ found) — pass --obsidian or --dir to override")
		case exists(filepath.Join(o.Root, ".obsidian")):
			c = blm.Presets[blm.BackendObsidian]
			say("backend  obsidian auto-detected (.obsidian/ found)")
		default:
			say("backend  none (no .agentsroom/ or .obsidian/ found) — files in " + c.Store + " are the truth")
		}
		if o.Store != "" {
			c.Store = o.Store
		}
	}
	// เครื่องมือข้างเคียง: ตามที่สั่ง หรือตรวจว่าตัวไหนติดตั้งอยู่ (obsidian backend นับ obsidian เสมอ)
	c.Tools = o.Tools
	if c.Tools == nil {
		c.Tools = []string{}
		for _, t := range blm.DetectTools(o.Root, c) {
			if t.Installed && t.Used {
				c.Tools = append(c.Tools, t.Name)
			}
		}
	}
	if o.Backend == blm.BackendObsidian && !contains(c.Tools, "obsidian") {
		c.Tools = append(c.Tools, "obsidian")
	}
	// ไม่ระบุ --tools → tree-sitter (ast-grep) ติดตั้งอัตโนมัติเสมอ (เจ้าของ 2026-09-09) เพราะ blm_graph ต้องใช้ AST จริง
	if o.Tools == nil && !contains(c.Tools, "tree-sitter") {
		c.Tools = append(c.Tools, "tree-sitter")
	}
	// ติดตั้งตัวที่เลือก/ตั้งอัตโนมัติแต่ยังไม่มี — ทีละตัว ล้มก็ไปต่อ
	installed := map[string]bool{}
	for _, t := range blm.DetectTools(o.Root, c) {
		installed[t.Name] = t.Installed
	}
	toolImpact := map[string][2]string{
		"tree-sitter": {"blm_graph falls back to coarse regex (imports only, no call graph)", "blm tools install tree-sitter   # mac: brew install ast-grep · linux: cargo install ast-grep / npm i -g @ast-grep/cli"},
		"socraticode": {"no semantic code search for the agent; blm_graph still works", "blm tools install socraticode  (needs Ollama; add --docker to run Qdrant/Ollama in Docker)"},
		"embedding":   {"no local embeddings — the agent infers meaning from code itself", "blm tools install embedding [--docker]"},
		"graphify":    {"no graphify knowledge graph; blm_graph covers structure", "pipx install graphifyy && graphify install --platform claude"},
		"obsidian":    {"no Obsidian vault view of the notes", "brew install --cask obsidian (mac) · winget install Obsidian.Obsidian"},
	}
	for _, t := range c.Tools {
		if installed[t] {
			say("  ✔ " + t + " already installed")
			continue
		}
		if o.NoInstall {
			say("  · " + t + " not installed (skipped: --no-install)")
			continue
		}
		imp := toolImpact[t]
		step(t, imp[0], imp[1], func() (string, error) {
			out, err := blm.Tools(o.Root, c, "install", t, o.Docker, o.Out)
			return strings.TrimSpace(out), err
		})
	}
	if o.CustomCLI != "" {
		// /blm_init จะอ่าน `<cli> --help` แล้วแทนสองบรรทัดนี้ด้วยคำสั่งจริง ใช้ {name} {file} {folder} {description}
		c.PushCommand = o.CustomCLI + " --help  # TODO: save command using {name} {file} {folder} {description}"
		c.PullCommand = o.CustomCLI + " --help  # TODO: get/export command using {name}"
	}

	// 0. ย้าย store ชื่อเดิม (.agentsroom/memory-temp) → ใหม่ ครั้งเดียว
	if old := filepath.Join(o.Root, oldStoreDir); c.Store != oldStoreDir {
		if _, err := os.Stat(old); err == nil {
			if entries, _ := os.ReadDir(filepath.Join(o.Root, c.Store)); len(entries) == 0 {
				_ = os.Remove(filepath.Join(o.Root, c.Store)) // dir ว่างที่ blm status สร้างไว้ก่อน init
				if err := migrate(old, filepath.Join(o.Root, c.Store)); err == nil {
					say("migrate  " + oldStoreDir + " -> " + c.Store + " (project-business-logic -> blm)")
				} else {
					say("migrate  could not move " + oldStoreDir + ": " + err.Error())
				}
			}
		}
		_ = os.Remove(filepath.Join(o.Root, ".agentsroom", oldRules)) // symlink สั้นชื่อเดิม
	}

	// 1. config + store + .ignorememory
	if err := c.Save(o.Root); err != nil {
		return append(log, "config   cannot write "+blm.ConfigFile+": "+err.Error())
	}
	line := fmt.Sprintf("config   %s  backend=%s store=%s", blm.ConfigFile, c.Backend, c.Store)
	if c.Mirror != "" {
		line += " mirror=" + c.Mirror
	}
	say(line + "  tools=" + strings.Join(c.Tools, ","))
	_ = os.MkdirAll(filepath.Join(o.Root, c.Store), 0o755)
	// ignore: ของที่เป็น output/สำเนาของเครื่องมืออื่นต้องไม่ถูกวิเคราะห์หรือ index ซ้ำ (เจ้าของ 2026-09-09):
	// store ของ blm เอง (ร่าง/history/reports), graphify-out/, vault ของ Obsidian, สถานะแอปใน .agentsroom (ยกเว้น mirror memory)
	ignoreLines := []string{c.Store + "/", ".claude/", ".agentsroom/*", "!.agentsroom/memory/", "graphify-out/", ".obsidian/", "node_modules/", "dist/", "build/", "*.lock"}
	ign := filepath.Join(o.Root, ".ignorememory")
	if _, err := os.Stat(ign); err != nil {
		_ = os.WriteFile(ign, []byte("# blm: paths to skip during /blm_init analysis, on top of .gitignore and .socraticodeignore (merged automatically)\n# one gitignore-style pattern per line — tool outputs and blm's own store are never project knowledge\n"+strings.Join(ignoreLines, "\n")+"\n"), 0o644)
		say("ignore   .ignorememory created (" + strings.Join(ignoreLines, " ") + ")")
	}
	// socraticode ใช้ในโปรเจ็คนี้ → กัน index ซ้ำ: เติมบรรทัดที่ยังไม่มีเข้า .socraticodeignore (ไม่แตะของเดิม)
	if contains(c.Tools, "socraticode") {
		sci := filepath.Join(o.Root, ".socraticodeignore")
		cur := readFileOr(sci)
		var add []string
		for _, l := range []string{c.Store + "/", "graphify-out/", ".obsidian/"} {
			if !strings.Contains(cur, "\n"+l) && !strings.HasPrefix(cur, l) && !strings.Contains(cur, "\n"+strings.TrimSuffix(l, "/")+"\n") {
				add = append(add, l)
			}
		}
		if len(add) > 0 {
			if cur != "" && !strings.HasSuffix(cur, "\n") {
				cur += "\n"
			}
			cur += "\n# blm: tool outputs — indexed elsewhere or regenerated, never index twice\n" + strings.Join(add, "\n") + "\n"
			if err := os.WriteFile(sci, []byte(cur), 0o644); err == nil {
				say("ignore   .socraticodeignore += " + strings.Join(add, " "))
			}
		}
	}

	// 2. .claude/settings.json ของโปรเจ็ค (merge ไม่ทับ) — Edit/Write ห้าม · sandbox denyWrite · hook `blm guard`
	sf := filepath.Join(o.Root, ".claude", "settings.json")
	settings := readJSON(sf)
	perm := sub(settings, "permissions")
	perm["deny"] = mergeList(strList(perm, "deny"), "Edit("+c.Store+"/**)", "Write("+c.Store+"/**)", "NotebookEdit("+c.Store+"/**)")
	fs := sub(sub(settings, "sandbox"), "filesystem")
	fs["denyWrite"] = mergeList(strList(fs, "denyWrite"), c.Store)
	hooks := sub(settings, "hooks")
	pre, _ := hooks["PreToolUse"].([]any)
	var kept []any
	for _, h := range pre {
		raw, _ := json.Marshal(h)
		if strings.Contains(string(raw), "guard-memory-temp") || strings.Contains(string(raw), "blm guard") {
			continue
		}
		kept = append(kept, h)
	}
	kept = append(kept, map[string]any{"matcher": "Bash", "hooks": []any{map[string]any{"type": "command", "command": "blm guard", "timeout": 5, "statusMessage": "blm guard"}}})
	hooks["PreToolUse"] = kept
	if err := writeJSON(sf, settings); err != nil {
		say("settings cannot write .claude/settings.json (" + err.Error() + ")")
		failures = append(failures, failure{"settings", err.Error(), "the store is not locked — Edit/Write/Bash can modify blm notes", "add to .claude/settings.json: permissions.deny Edit/Write(" + c.Store + "/**) · sandbox.filesystem.denyWrite " + c.Store + " · hooks.PreToolUse Bash -> blm guard"})
	} else {
		say("settings .claude/settings.json merged (deny Edit/Write · sandbox denyWrite · PreToolUse hook `blm guard`)")
	}
	_ = os.Remove(filepath.Join(o.Root, ".claude", "hooks", "guard-memory-temp.sh"))

	// 3. sandbox ใน user settings (ตามขอเท่านั้น — กระทบทุกโปรเจ็ค)
	if o.Sandbox {
		home, _ := os.UserHomeDir()
		uf := filepath.Join(home, ".claude", "settings.json")
		u := readJSON(uf)
		sb := sub(u, "sandbox")
		sb["enabled"], sb["allowUnsandboxedCommands"] = true, false
		if err := writeJSON(uf, u); err != nil {
			say("sandbox  cannot write ~/.claude/settings.json: " + err.Error())
		} else {
			say("sandbox  ~/.claude/settings.json enabled=true allowUnsandboxedCommands=false (OS-level guard)")
		}
	} else if home, _ := os.UserHomeDir(); sub(readJSON(filepath.Join(home, ".claude", "settings.json")), "sandbox")["enabled"] == true {
		say("sandbox  already enabled in ~/.claude/settings.json (OS-level guard covers denyWrite)")
	} else {
		say("sandbox  off — `blm guard` only filters command text; for a kernel-level lock rerun with --sandbox or use /sandbox in Claude Code")
	}

	// 4. PATH + plugin
	say("path     " + EnsurePath())
	if o.Plugin {
		run := o.Run
		if run == nil {
			run = shell
		}
		manual := "claude plugin marketplace add " + o.Marketplace + " && claude plugin install " + PluginName + "@" + PluginName
		step("claude plugin blm", "Claude Code has no blm_* tools / slash commands", manual, func() (string, error) {
			if _, err := run("claude", "--version"); err != nil {
				return "", fmt.Errorf("`claude` not on PATH")
			}
			if out, err := run("claude", "plugin", "marketplace", "add", o.Marketplace); err != nil && !strings.Contains(out, "already") {
				return "", fmt.Errorf("marketplace add: %s", strings.TrimSpace(out))
			}
			if out, err := run("claude", "plugin", "install", PluginName+"@"+PluginName); err != nil && !strings.Contains(out, "already") {
				return "", fmt.Errorf("install: %s", strings.TrimSpace(out))
			}
			return "", nil
		})
	}
	// สรุปท้าย: อะไรล้ม กระทบอะไร ควรทำอะไร (ทั้งหมดถูกพิมพ์ระหว่างทางแล้ว นี่คือรายการรวม)
	if len(failures) > 0 {
		say("")
		say(fmt.Sprintf("summary  %d step(s) failed — blm still works, with these limits:", len(failures)))
		for _, f := range failures {
			say("  ✘ " + f.step + ": " + f.err)
			say("      impact: " + f.impact)
			say("      fix:    " + f.action)
		}
	} else {
		say("")
		say("summary  all steps done")
	}
	return append(log, "next     open Claude Code in the project -> /reload-plugins -> /blm_init to analyse the project and draft blm.md · check: blm status")
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func readFileOr(p string) string {
	raw, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(raw)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

var targetRe = regexp.MustCompile(`(?m)^target: ` + oldRules + `$`)

// migrate ย้ายโฟลเดอร์ store เดิมมาชื่อใหม่ + เปลี่ยนชื่อไฟล์กฎ/target จาก project-business-logic → blm
func migrate(old, new string) error {
	if err := os.MkdirAll(filepath.Dir(new), 0o755); err != nil {
		return err
	}
	if err := os.Rename(old, new); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(new, oldRules+".md")); err == nil {
		_ = os.Rename(filepath.Join(new, oldRules+".md"), filepath.Join(new, blm.RulesNote+".md"))
	}
	entries, _ := os.ReadDir(new)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		p := filepath.Join(new, e.Name())
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if targetRe.Match(raw) {
			_ = os.WriteFile(p, targetRe.ReplaceAll(raw, []byte("target: "+blm.RulesNote)), 0o644)
		}
	}
	return nil
}

func shell(cmd ...string) (string, error) {
	out, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput()
	return string(out), err
}
