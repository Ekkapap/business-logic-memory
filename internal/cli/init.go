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
			o.Backend = blm.BackendAgentsRoom
		case "--obsidian":
			o.Backend = blm.BackendObsidian
		case "--dir":
			o.Store = next(&i)
		case "--backend":
			o.CustomCLI = next(&i)
			o.Backend = blm.BackendCustom
		case "--docker":
			o.Docker = true
		case "--no-plugin":
			o.Plugin = false
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

func Init(o InitOptions) []string {
	var log []string
	c := blm.Config{Backend: blm.BackendCustom, Store: o.Store}
	if o.Backend != blm.BackendCustom {
		c = blm.Presets[o.Backend]
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
	// ติดตั้งตัวที่เลือกไว้แต่ยังไม่มี (เฉพาะเมื่อสั่ง --tools ชัด ๆ ไม่ติดตั้งจากการเดา)
	if o.Tools != nil {
		installed := map[string]bool{}
		for _, t := range blm.DetectTools(o.Root, c) {
			installed[t.Name] = t.Installed
		}
		for _, t := range c.Tools {
			if installed[t] {
				log = append(log, "tools    "+t+" already installed")
				continue
			}
			if _, err := blm.Tools(o.Root, c, "install", t, o.Docker, o.Out); err != nil {
				log = append(log, "tools    "+t+" install failed: "+err.Error())
			} else {
				log = append(log, "tools    "+t+" installed")
			}
		}
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
					log = append(log, "migrate  "+oldStoreDir+" -> "+c.Store+" (project-business-logic -> blm)")
				} else {
					log = append(log, "migrate  could not move "+oldStoreDir+": "+err.Error())
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
	log = append(log, line+"  tools="+strings.Join(c.Tools, ","))
	_ = os.MkdirAll(filepath.Join(o.Root, c.Store), 0o755)
	ign := filepath.Join(o.Root, ".ignorememory")
	if _, err := os.Stat(ign); err != nil {
		_ = os.WriteFile(ign, []byte("# blm: paths to skip during /blm_init analysis, on top of .gitignore and .socraticodeignore (merged automatically)\n# one gitignore-style pattern per line\nnode_modules/\ndist/\nbuild/\n*.lock\n"), 0o644)
		log = append(log, "ignore   .ignorememory created")
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
		log = append(log, "settings cannot write .claude/settings.json ("+err.Error()+") — add manually: permissions.deny Edit/Write("+c.Store+"/**) · sandbox.filesystem.denyWrite "+c.Store+" · hooks.PreToolUse Bash -> blm guard")
	} else {
		log = append(log, "settings .claude/settings.json merged (deny Edit/Write · sandbox denyWrite · PreToolUse hook `blm guard`)")
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
			log = append(log, "sandbox  cannot write ~/.claude/settings.json: "+err.Error())
		} else {
			log = append(log, "sandbox  ~/.claude/settings.json enabled=true allowUnsandboxedCommands=false (OS-level guard)")
		}
	} else if home, _ := os.UserHomeDir(); sub(readJSON(filepath.Join(home, ".claude", "settings.json")), "sandbox")["enabled"] == true {
		log = append(log, "sandbox  already enabled in ~/.claude/settings.json (OS-level guard covers denyWrite)")
	} else {
		log = append(log, "sandbox  off — `blm guard` only filters command text; for a kernel-level lock rerun with --sandbox or use /sandbox in Claude Code")
	}

	// 4. PATH + plugin
	log = append(log, "path     "+EnsurePath())
	if o.Plugin {
		run := o.Run
		if run == nil {
			run = shell
		}
		if _, err := run("claude", "--version"); err != nil {
			log = append(log, "plugin   `claude` not on PATH — install Claude Code, then run: claude plugin marketplace add "+o.Marketplace+" && claude plugin install "+PluginName+"@"+PluginName)
		} else {
			_, e1 := run("claude", "plugin", "marketplace", "add", o.Marketplace)
			out, e2 := run("claude", "plugin", "install", PluginName+"@"+PluginName)
			status := "ok"
			if e1 != nil || e2 != nil {
				status = "failed (" + strings.TrimSpace(out) + ") — run: claude plugin marketplace add " + o.Marketplace + " && claude plugin install " + PluginName + "@" + PluginName
			}
			log = append(log, "plugin   "+status)
		}
	}
	return append(log, "next     open Claude Code in the project -> /reload-plugins -> /blm_init to analyse the project and draft blm.md · check: blm status")
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
