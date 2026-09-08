package blm

import (
	"bufio"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// scripts/tools.sh (macOS/Linux) และ scripts/tools.ps1 (Windows) ถือ logic ติดตั้ง/เริ่ม/หยุดทั้งหมด
// (เจ้าของ 2026-09-09: logic ใหญ่ แยกเป็น script แล้วให้ blm_tools แค่เรียก) — ฝังใน binary จึงไม่ต้องมี repo บนเครื่องผู้ใช้
//
//go:embed scripts/tools.sh scripts/tools.ps1
var scripts embed.FS

// Tools จัดการเครื่องมือช่วยวิเคราะห์: status | get | install | start | stop | restart | gen-graph | help
// docker=true → socraticode แบบ Docker (Qdrant+Ollama ในคอนเทนเนอร์) · false → native (mac: brew, linux: binary+installer, windows: ไม่รองรับ)
func Tools(root string, c Config, action, tool string, docker bool, out io.Writer) (string, error) {
	switch action {
	case "", "help":
		return strings.Join([]string{
			"blm tools <action> [--docker] [socraticode|obsidian|graphify|tree-sitter|embedding]",
			"  status                    state of the configured tools (or the one named)",
			"  get <tool>                what install/start/stop will run for it",
			"  install [--docker] <tool> check first, then install what is missing",
			"                            socraticode native = Qdrant + Ollama (mac: brew · linux: release binary + ollama.com installer · windows: docker only)",
			"                            socraticode --docker = Docker CLI (installed if missing) + images; SocratiCode manages the containers",
			"                            tree-sitter = ast-grep (tree-sitter engine) for blm_graph AST mode when no SocratiCode/graphify/Obsidian",
			"                            embedding = Ollama + nomic-embed-text for meaning (native, or --docker) — or let the agent do it",
			"  start|stop|restart [--docker] <tool>",
			"  gen-graph                 graphify: how to build the graph (the build itself runs as /graphify inside Claude Code)",
		}, "\n"), nil
	case "status":
		var tools []ToolStatus
		for _, t := range configuredTools(DetectTools(root, c), c) {
			if tool == "" || t.Name == tool {
				tools = append(tools, t)
			}
		}
		rows := ToolRows(tools)
		if len(rows) == 0 {
			return "no tools configured (blm init … --tools socraticode,obsidian,graphify,tree-sitter,embedding)", nil
		}
		return Table([]string{"Tool", "State", "Detail"}, rows), nil
	}
	if tool == "" {
		return "", fmt.Errorf("tool required: socraticode | obsidian | graphify")
	}
	switch tool {
	case "socraticode", "obsidian", "graphify", "tree-sitter", "embedding":
	default:
		return "", fmt.Errorf("unknown tool %q (socraticode | obsidian | graphify | tree-sitter | embedding)", tool)
	}
	mode := ""
	if docker {
		mode = "--docker"
	}
	switch action {
	case "get":
		return fmt.Sprintf("%s\n  install  %s\n  start    %s\n  stop     %s", tool, scriptCmd("install", tool, mode), scriptCmd("start", tool, mode), scriptCmd("stop", tool, mode)), nil
	case "gen-graph":
		return "graphify builds its graph only through the Claude Code skill: type /graphify in the project (output: graphify-out/graph.json) · path: graphify path \"A\" \"B\" · explain a node: graphify explain \"X\"", nil
	case "restart":
		if _, err := RunToolScript("stop", tool, mode, out); err != nil {
			return "", err
		}
		action = "start"
	case "install", "start", "stop":
	default:
		return "", fmt.Errorf("unknown action %q", action)
	}
	envLines, err := RunToolScript(action, tool, mode, out)
	if err != nil {
		return "", fmt.Errorf("%s %s failed: %v", tool, action, err)
	}
	msg := fmt.Sprintf("%s %s: done", tool, action)
	// ติดตั้งภายหลัง (ไม่ได้ระบุตอน init) → เพิ่มเข้า config.Tools ให้ status/tools เห็นเป็น ✔ ทันที
	if action == "install" && !containsStr(c.Tools, tool) {
		c.Tools = append(c.Tools, tool)
		if err := c.Save(root); err == nil {
			msg += "\nconfig: added " + tool + " to tools in " + ConfigFile
		}
	}
	if len(envLines) > 0 {
		if note, err := applyEnv(envLines); err == nil && note != "" {
			msg += "\n" + note
		} else if err != nil {
			msg += "\nenv: " + err.Error()
		}
	}
	return msg, nil
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func scriptCmd(action, tool, mode string) string {
	name := "tools.sh"
	if runtime.GOOS == "windows" {
		name = "tools.ps1"
	}
	return strings.TrimSpace(fmt.Sprintf("%s %s %s %s", name, action, tool, mode))
}

// RunToolScript เขียน script ที่ฝังไว้ลง temp แล้วรัน stream output ไป out · คืนบรรทัด `ENV …` ที่ script ขอให้ blm ตั้งค่า
func RunToolScript(action, tool, mode string, out io.Writer) ([]string, error) {
	name, shell := "tools.sh", []string{"sh"}
	if runtime.GOOS == "windows" {
		name, shell = "tools.ps1", []string{"powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File"}
	}
	raw, err := scripts.ReadFile("scripts/" + name)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "blm-tools-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, raw, 0o755); err != nil {
		return nil, err
	}
	args := append(shell[1:], path, action, tool)
	if mode != "" {
		args = append(args, mode)
	}
	cmd := exec.Command(shell[0], args...)
	cmd.Stdin = os.Stdin
	cmd.Stderr = out
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	var envLines []string
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "ENV ") {
			envLines = append(envLines, strings.TrimPrefix(line, "ENV "))
			continue
		}
		if out != nil {
			fmt.Fprintln(out, line)
		}
	}
	return envLines, cmd.Wait()
}

// applyEnv ตั้ง/ลบตัวแปรใน ~/.claude/settings.json "env" (MCP server ของ SocratiCode สืบทอด env นี้)
// รูปแบบ: `KEY=value` = ตั้ง · `-KEY` = ลบ
func applyEnv(lines []string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	file := filepath.Join(home, ".claude", "settings.json")
	settings := map[string]any{}
	if raw, err := os.ReadFile(file); err == nil {
		_ = json.Unmarshal(raw, &settings)
	}
	env, _ := settings["env"].(map[string]any)
	if env == nil {
		env = map[string]any{}
	}
	var set, del []string
	for _, l := range lines {
		for _, item := range strings.Fields(l) {
			if strings.HasPrefix(item, "-") {
				delete(env, item[1:])
				del = append(del, item[1:])
			} else if k, v, ok := strings.Cut(item, "="); ok {
				env[k] = v
				set = append(set, item)
			}
		}
	}
	settings["env"] = env
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return "", err
	}
	raw, _ := json.MarshalIndent(settings, "", "  ")
	if err := os.WriteFile(file, append(raw, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("cannot write %s (%v) — set manually: %s", file, err, strings.Join(lines, " "))
	}
	var parts []string
	if len(set) > 0 {
		parts = append(parts, "set "+strings.Join(set, " "))
	}
	if len(del) > 0 {
		parts = append(parts, "removed "+strings.Join(del, " "))
	}
	return "env (~/.claude/settings.json): " + strings.Join(parts, " · ") + " — reconnect the socraticode MCP to apply", nil
}
