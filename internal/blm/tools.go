package blm

import (
	"bufio"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// scripts/tools.sh (macOS/Linux) และ scripts/tools.ps1 (Windows) ถือ logic ติดตั้ง/เริ่ม/หยุดทั้งหมด
// (เจ้าของ 2026-09-09: logic ใหญ่ แยกเป็น script แล้วให้ blm_tools แค่เรียก) — ฝังใน binary จึงไม่ต้องมี repo บนเครื่องผู้ใช้
//
//go:embed scripts/tools.sh scripts/tools.ps1
var scripts embed.FS

// ToolOpts ตัวเลือกของ `blm tools …` (CLI flags / MCP params ชุดเดียวกัน)
//   - Docker: socraticode/embedding ใน Docker (Qdrant+Ollama ในคอนเทนเนอร์)
//   - Local : socraticode แบบเดิม — ติดตั้ง Qdrant + Ollama ลงเครื่องนี้ (mac: brew · linux: binary+installer · windows: ไม่รองรับ)
//   - Remote: socraticode แบบใหม่ — ไม่ติดตั้งอะไรบนเครื่องนี้ ชี้ไป Ollama + Qdrant ที่มีอยู่แล้ว (host หรือ URL) แล้วจำไว้ใน .claude/blm.json
//     ไม่ระบุทั้ง Local/Remote: มี socraticode ใน config → remote · ไม่มี → local (เข้ากันได้กับก่อน 2026-09-20)
type ToolOpts struct {
	Docker bool
	Local  bool
	Remote bool
	// RemoteHost — host ของ server (port มาตรฐาน 11434/6333) · ว่าง = ใช้ SC.OllamaURL/QdrantURL หรือค่าที่จำไว้ใน config
	RemoteHost string
	// SC — ค่าที่ระบุมากับคำสั่ง ทับค่าใน config ทีละช่อง (ช่องว่าง = คงของเดิม)
	SC SocratiCodeConfig
}

// ToolsHelp ข้อความ `blm tools help` — ใช้ทั้ง CLI และ MCP
const ToolsHelp = `blm tools <action> [tool] [--docker | --local | --remote <host> …]   tools: socraticode | obsidian | graphify | tree-sitter | embedding
  status                    state of the configured tools (or the one named)
  get <tool>                what install/start/stop will run for it
  install <tool>            check first, then install what is missing
    socraticode --local     services on THIS machine: Qdrant + Ollama + nomic-embed-text (mac: brew · linux: release binary + ollama.com · windows: docker only)
    socraticode --remote <host> [--embedding-model m --embedding-dimensions n --embedding-context-length n --embedding-query-prefix p --embedding-document-prefix p]
                            services on ANOTHER machine (e.g. a GPU box on your network): nothing is installed here — blm checks Ollama :11434 + Qdrant :6333
                            on that host (or --ollama-url / --qdrant-url for other ports), saves the values to .claude/blm.json, writes the
                            OLLAMA_*/QDRANT_*/EMBEDDING_* env for SocratiCode into ~/.claude/settings.json · next time: --remote alone reuses the saved values
    socraticode             no flag: --remote when .claude/blm.json has a socraticode section, else --local
    socraticode --docker    Docker CLI (installed if missing) + images; SocratiCode manages the containers
    tree-sitter             ast-grep (tree-sitter engine) for blm_graph AST mode when no SocratiCode/graphify/Obsidian
    embedding               Ollama + nomic-embed-text for meaning (native, or --docker) — or let the agent do it
  start|stop|restart [--docker] <tool>   (socraticode remote: nothing to start here — the services live on the other host)
  gen-graph                 graphify: how to build the graph (the build itself runs as /graphify inside Claude Code)

examples
  blm tools install socraticode --local
  blm tools install socraticode --remote 192.168.1.50 --embedding-model bge-m3 --embedding-dimensions 1024 --embedding-context-length 8192
  blm tools install socraticode --remote                       # reuse .claude/blm.json → rewrite ~/.claude/settings.json env
  blm tools status socraticode`

// Tools จัดการเครื่องมือช่วยวิเคราะห์: status | get | install | start | stop | restart | gen-graph | help
func Tools(root string, c Config, action, tool string, opts ToolOpts, out io.Writer) (string, error) {
	switch action {
	case "", "help":
		return ToolsHelp, nil
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
	// socraticode แบบ remote ไม่มี script ให้รัน — ทำใน Go ทั้งหมด (ping + config + env) ใช้ได้ทุก OS รวม Windows
	if tool == "socraticode" && (opts.Remote || (!opts.Local && !opts.Docker && c.SocratiCode != nil)) {
		return toolsSocratiCodeRemote(root, c, action, opts)
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
	if opts.Docker {
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
	// socraticode --local ทับ remote ที่จำไว้: บริการกลับมาอยู่เครื่องนี้ ค่า server เดิมต้องหายจาก config (env ถูก script ลบให้)
	if action == "install" {
		changed := false
		if !containsStr(c.Tools, tool) {
			c.Tools = append(c.Tools, tool)
			changed = true
		}
		if tool == "socraticode" && c.SocratiCode != nil {
			c.SocratiCode = nil
			changed = true
		}
		if changed {
			if err := c.Save(root); err == nil {
				msg += "\nconfig: " + ConfigFile + " updated (tools" + map[bool]string{true: ", socraticode section removed", false: ""}[tool == "socraticode"] + ")"
			}
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
		// บรรทัดที่เป็น `KEY=value` ทั้งบรรทัด = รายการเดียว (value มีช่องว่างได้ เช่น prefix "search_query: ")
		// ไม่งั้นแยกตามช่องว่าง (`-A -B -C` จาก tools.sh)
		items := []string{l}
		if k, _, ok := strings.Cut(l, "="); !ok || strings.ContainsAny(k, " \t") {
			items = strings.Fields(l)
		}
		for _, item := range items {
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

// toolsSocratiCodeRemote — `blm tools install socraticode --remote …` และ get/start/stop/restart ของโหมดนั้น
// ไม่ติดตั้งอะไรบนเครื่องนี้: ตรวจว่า Ollama/Qdrant บน host ตอบจริง (และมีโมเดล) → จำลง config → เขียน env ให้ SocratiCode
func toolsSocratiCodeRemote(root string, c Config, action string, opts ToolOpts) (string, error) {
	sc := SocratiCodeConfig{}
	if c.SocratiCode != nil {
		sc = *c.SocratiCode
	}
	if h := strings.TrimSpace(opts.RemoteHost); h != "" {
		h = strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(h, "http://"), "https://"), "/"), ":")
		sc.OllamaURL, sc.QdrantURL = "http://"+h+":11434", "http://"+h+":6333"
	}
	// ค่าที่มากับคำสั่งทับทีละช่อง
	if v := opts.SC.OllamaURL; v != "" {
		sc.OllamaURL = strings.TrimRight(v, "/")
	}
	if v := opts.SC.QdrantURL; v != "" {
		sc.QdrantURL = strings.TrimRight(v, "/")
	}
	modelGiven := opts.SC.EmbeddingModel != ""
	if modelGiven {
		sc.EmbeddingModel = opts.SC.EmbeddingModel
	}
	for dst, src := range map[*string]string{&sc.EmbeddingDimensions: opts.SC.EmbeddingDimensions, &sc.EmbeddingContextLength: opts.SC.EmbeddingContextLength} {
		if src != "" {
			*dst = src
		}
	}
	// prefix: ระบุมา = ใช้ตามนั้น (ว่างก็ได้) · ไม่ระบุแต่เปลี่ยนโมเดลไปจาก nomic = ว่าง (bge-m3 / e5 ไม่ใช้ prefix ของ nomic)
	if opts.SC.EmbeddingQueryPrefix != "" || opts.SC.EmbeddingDocumentPrefix != "" {
		sc.EmbeddingQueryPrefix, sc.EmbeddingDocumentPrefix = opts.SC.EmbeddingQueryPrefix, opts.SC.EmbeddingDocumentPrefix
	} else if modelGiven && !strings.HasPrefix(sc.EmbeddingModel, "nomic-embed-text") {
		sc.EmbeddingQueryPrefix, sc.EmbeddingDocumentPrefix = "", ""
	}
	if sc.OllamaURL == "" || sc.QdrantURL == "" {
		return "", fmt.Errorf("socraticode --remote needs a host: blm tools install socraticode --remote <host> (or --ollama-url … --qdrant-url …); nothing saved in %s yet", ConfigFile)
	}
	if sc.EmbeddingModel != "" && (sc.EmbeddingDimensions == "" || sc.EmbeddingContextLength == "") {
		return "", fmt.Errorf("--embedding-model %s also needs --embedding-dimensions and --embedding-context-length (SocratiCode cannot guess them for a model it does not know)", sc.EmbeddingModel)
	}

	switch action {
	case "get":
		return "socraticode (remote " + sc.OllamaURL + " · " + sc.QdrantURL + ")\n  install  ping both, save " + ConfigFile + ", write env to ~/.claude/settings.json\n  start    nothing here — services run on the remote host\n  stop     nothing here", nil
	case "start", "stop", "restart":
		return fmt.Sprintf("socraticode %s: nothing to do on this machine — Ollama/Qdrant run on the remote host (%s · %s)", action, sc.OllamaURL, sc.QdrantURL), nil
	case "install":
	default:
		return "", fmt.Errorf("unknown action %q", action)
	}

	var lines []string
	ok := true
	if httpUp(sc.OllamaURL + "/api/tags") {
		lines = append(lines, "ollama: up at "+sc.OllamaURL)
		if sc.EmbeddingModel != "" {
			if ollamaHasModel(sc.OllamaURL, sc.EmbeddingModel) {
				lines = append(lines, "ollama: model "+sc.EmbeddingModel+" present")
			} else {
				ok = false
				lines = append(lines, "ollama: model "+sc.EmbeddingModel+" NOT found on that host — run there: ollama pull "+sc.EmbeddingModel)
			}
		}
	} else {
		ok = false
		lines = append(lines, "ollama: no answer at "+sc.OllamaURL+"/api/tags (is it bound to that address? firewall? VPN up?)")
	}
	if httpUp(sc.QdrantURL + "/collections") {
		lines = append(lines, "qdrant: up at "+sc.QdrantURL)
	} else {
		ok = false
		lines = append(lines, "qdrant: no answer at "+sc.QdrantURL+"/collections")
	}
	if !ok {
		return "", fmt.Errorf("%s\nsocraticode remote: fix the host first — nothing saved", strings.Join(lines, "\n"))
	}
	c.SocratiCode = &sc
	if !containsStr(c.Tools, "socraticode") {
		c.Tools = append(c.Tools, "socraticode")
	}
	if err := c.Save(root); err != nil {
		return "", fmt.Errorf("cannot write %s: %v", ConfigFile, err)
	}
	lines = append(lines, "config: socraticode section saved to "+ConfigFile)
	if note, err := applyEnv(sc.Env()); err != nil {
		lines = append(lines, "env: "+err.Error())
	} else {
		lines = append(lines, note)
	}
	if !socratiCodePluginInstalled() {
		lines = append(lines, "socraticode: Claude plugin not found — install it: claude plugin marketplace add giancarloerra/socraticode && claude plugin install socraticode@socraticode")
	}
	lines = append(lines, "socraticode: remote stack ready — reconnect the plugin (/plugin → socraticode) then codebase_health should show both external, and codebase_index if the collection is new")
	return strings.Join(lines, "\n"), nil
}

// ollamaHasModel — GET /api/tags มี model ชื่อนี้ไหม (`bge-m3` ตรงกับ `bge-m3:latest`)
func ollamaHasModel(base, model string) bool {
	cl := http.Client{Timeout: 3 * time.Second}
	resp, err := cl.Get(base + "/api/tags")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if json.NewDecoder(resp.Body).Decode(&tags) != nil {
		return false
	}
	for _, m := range tags.Models {
		if m.Name == model || strings.TrimSuffix(m.Name, ":latest") == model {
			return true
		}
	}
	return false
}

func socratiCodePluginInstalled() bool {
	home, _ := os.UserHomeDir()
	return home != "" && latestDir(filepath.Join(home, ".claude", "plugins", "cache", "socraticode", "socraticode")) != ""
}
