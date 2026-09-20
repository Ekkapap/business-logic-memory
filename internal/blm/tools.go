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
//go:embed scripts/tools.sh scripts/tools.ps1 scripts/statusline-command.sh
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
    (both modes)            also wire Claude Code: hooks "blm hook session-start|prompt|grep-nudge" in ~/.claude/settings.json and the socraticode
                            line at the end of ~/.claude/statusline-command.sh (file created from blm's default statusline when missing) — idempotent
    socraticode             no flag: --local when nothing is configured · when .claude/blm.json already has a remote section blm stops and asks for --remote or --local
    socraticode --docker    Docker CLI (installed if missing) + images; SocratiCode manages the containers
    tree-sitter             ast-grep (tree-sitter engine) for blm_graph AST mode when no SocratiCode/graphify/Obsidian
    embedding               Ollama + nomic-embed-text for meaning (native, or --docker) — or let the agent do it
  update socraticode        latest SocratiCode plugin: claude plugin marketplace update socraticode → claude plugin update socraticode@socraticode
                            (the MCP itself runs "npx socraticode@latest", so the server code is already latest on every start; this refreshes the
                            plugin skill/commands/hooks) · --remote/--local not needed: Ollama/Qdrant are updated where they run
  start|stop|restart [--docker] <tool>   (socraticode remote: nothing to start here — the services live on the other host)
  gen-graph                 graphify: how to build the graph (the build itself runs as /graphify inside Claude Code)

examples
  blm tools install socraticode --local
  blm tools install socraticode --remote 192.168.1.50 --embedding-model bge-m3 --embedding-dimensions 1024 --embedding-context-length 8192
  blm tools install socraticode --remote                       # reuse .claude/blm.json → rewrite ~/.claude/settings.json env
  blm tools update socraticode                                 # latest plugin from the marketplace, then /plugin reconnect
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
	if action == "update" {
		if tool != "socraticode" {
			return "", fmt.Errorf("update is only implemented for socraticode (others: brew upgrade / pipx upgrade / the app's own updater)")
		}
		return toolsSocratiCodeUpdate(out)
	}
	// socraticode: ไม่ระบุโหมดแต่โปรเจ็คนี้จำ server ไว้ → หยุด ไม่เดา (เจ้าของ 2026-09-20: กันเผลอติดตั้ง local ทับ)
	if tool == "socraticode" && action == "install" && !opts.Remote && !opts.Local && !opts.Docker && c.SocratiCode != nil {
		return "", fmt.Errorf("%s\n%s\n\n%s\n%s\n\n%s\n%s\n\n%s\n\n%s",
			Yellow("This project have socraticode remote at :"),
			KV([][2]string{{"  " + Dim("LLM"), c.SocratiCode.OllamaURL}, {"  " + Dim("Qdrant"), c.SocratiCode.QdrantURL}}),
			"To install socraticode remote run this :",
			"  "+Cyan("blm tools install socraticode --remote"),
			"To install socraticode local run this :",
			"  "+Cyan("blm tools install socraticode --local"),
			Dim("Note : Local installation will drops the remote section from "+ConfigFile+" and the EMBEDDING_* env"),
			"Nothing changed")
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
		return "", fmt.Errorf("unknown action %q (status | get | install | update | start | stop | restart | gen-graph | help)", action)
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
	if action == "install" && tool == "socraticode" {
		msg += "\n" + Section("Claude Code  "+Dim("hooks + statusline"))
		for _, l := range WireClaude() {
			msg += "\n  " + paintWireLine(l)
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
	var rows [][2]string
	for _, item := range set {
		k, v, _ := strings.Cut(item, "=")
		if v == "" {
			v = Dim(`""`)
		}
		rows = append(rows, [2]string{"  " + Dim(k), v})
	}
	for _, k := range del {
		rows = append(rows, [2]string{"  " + Dim(k), Dim("removed")})
	}
	return Section("Env  "+Dim("~/.claude/settings.json → reconnect the socraticode MCP to apply")) + "\n" + KV(rows), nil
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

	// ---- Services: ping ก่อน ล้ม = ไม่บันทึกอะไร
	ok := true
	up := func(good bool) string {
		if good {
			return Green("✔ up")
		}
		ok = false
		return Red("✘ down")
	}
	ollamaOK := httpUp(sc.OllamaURL + "/api/tags")
	ollamaRow := up(ollamaOK) + "   " + sc.OllamaURL
	if ollamaOK && sc.EmbeddingModel != "" {
		if ollamaHasModel(sc.OllamaURL, sc.EmbeddingModel) {
			ollamaRow += "   " + Dim("model") + " " + sc.EmbeddingModel + " " + Green("✔")
		} else {
			ok = false
			ollamaRow += "   " + Dim("model") + " " + sc.EmbeddingModel + " " + Red("✘ not found — run there: ollama pull "+sc.EmbeddingModel)
		}
	} else if !ollamaOK {
		ollamaRow += "   " + Dim("(bound to that address? firewall? VPN up?)")
	}
	qdrantRow := up(httpUp(sc.QdrantURL+"/collections")) + "   " + sc.QdrantURL
	var b strings.Builder
	b.WriteString(Title("socraticode  " + Dim("remote — services stay on the other host, nothing installed here")))
	b.WriteString("\n" + Section("Services") + "\n" + KV([][2]string{{"  Ollama", ollamaRow}, {"  Qdrant", qdrantRow}}))
	if !ok {
		return "", fmt.Errorf("%s\n\n%s", b.String(), Red("fix the host first — nothing saved"))
	}

	// ---- Config
	c.SocratiCode = &sc
	if !containsStr(c.Tools, "socraticode") {
		c.Tools = append(c.Tools, "socraticode")
	}
	if err := c.Save(root); err != nil {
		return "", fmt.Errorf("cannot write %s: %v", ConfigFile, err)
	}
	show := func(v string) string {
		if v == "" {
			return Dim(`""`)
		}
		return v
	}
	b.WriteString("\n" + Section("Config  "+Dim(ConfigFile+" · socraticode section saved")) + "\n" + KV([][2]string{
		{"  " + Dim("ollamaUrl"), sc.OllamaURL}, {"  " + Dim("qdrantUrl"), sc.QdrantURL},
		{"  " + Dim("embeddingModel"), show(sc.EmbeddingModel)}, {"  " + Dim("embeddingDimensions"), show(sc.EmbeddingDimensions)}, {"  " + Dim("embeddingContextLength"), show(sc.EmbeddingContextLength)},
		{"  " + Dim("embeddingQueryPrefix"), show(sc.EmbeddingQueryPrefix)}, {"  " + Dim("embeddingDocumentPrefix"), show(sc.EmbeddingDocumentPrefix)},
	}))

	// ---- Env
	if note, err := applyEnv(sc.Env()); err != nil {
		b.WriteString("\n" + Section("Env") + "\n  " + Red(err.Error()))
	} else {
		b.WriteString("\n" + note)
	}

	// ---- Claude Code wiring
	b.WriteString("\n" + Section("Claude Code  "+Dim("hooks + statusline")))
	for _, l := range WireClaude() {
		b.WriteString("\n  " + paintWireLine(l))
	}
	if !socratiCodePluginInstalled() {
		b.WriteString("\n  " + Yellow("note:") + " Claude plugin not found — claude plugin marketplace add giancarloerra/socraticode && claude plugin install socraticode@socraticode")
	}

	// ---- Next
	b.WriteString("\n" + Section("Next") + "\n" + KV([][2]string{
		{"  1", "/plugin → reconnect " + Cyan("socraticode") + " " + Dim("(env is read at MCP start)")},
		{"  2", Cyan("codebase_health") + " → both external, model available"},
		{"  3", Cyan("codebase_index") + " " + Dim("only if this project's collection does not exist on that Qdrant yet")},
	}))
	return b.String(), nil
}

// paintWireLine สีของบรรทัดจาก WireClaude: note = เหลือง, cannot = แดง, ที่เหลือ label dim
func paintWireLine(l string) string {
	switch {
	case strings.HasPrefix(l, "note:"):
		return Yellow("note:") + strings.TrimPrefix(l, "note:")
	case strings.Contains(l, "cannot"):
		return Red(l)
	}
	if k, v, ok := strings.Cut(l, ": "); ok {
		return Dim(k+":") + " " + v
	}
	return l
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

// toolsSocratiCodeUpdate — `blm tools update socraticode`: plugin ล่าสุดจาก marketplace ของ socraticode
// ตัว MCP ของมันรันด้วย `npx -y --prefer-online socraticode@latest` (.mcp.json ของ plugin) จึงเป็นโค้ดล่าสุดทุกครั้งที่ start อยู่แล้ว
// ที่ต้อง update จริงคือ plugin (skill/commands/hooks ใน ~/.claude/plugins/cache) — ผ่าน `claude plugin …` เท่านั้น ไม่แตะ cache เอง
func toolsSocratiCodeUpdate(out io.Writer) (string, error) {
	if _, err := exec.LookPath("claude"); err != nil {
		return "", fmt.Errorf("`claude` not on PATH — update by hand inside Claude Code: /plugin → socraticode → update")
	}
	before := socratiCodePluginVersion()
	run := func(args ...string) (string, error) {
		cmd := exec.Command("claude", args...)
		raw, err := cmd.CombinedOutput()
		text := strings.TrimSpace(string(raw))
		if out != nil && text != "" {
			fmt.Fprintln(out, Dim("$ claude "+strings.Join(args, " ")))
			fmt.Fprintln(out, text)
		}
		return text, err
	}
	var b strings.Builder
	b.WriteString(Title("socraticode  " + Dim("update plugin")))
	b.WriteString("\n" + Section("Steps"))
	if _, err := run("plugin", "marketplace", "update", "socraticode"); err != nil {
		b.WriteString("\n  " + Red("✘") + " marketplace update failed: " + err.Error())
	} else {
		b.WriteString("\n  " + Green("✔") + " marketplace refreshed " + Dim("(giancarloerra/socraticode)"))
	}
	if _, err := run("plugin", "update", "socraticode@socraticode"); err != nil {
		if before == "" {
			return "", fmt.Errorf("%s\n  %s plugin not installed — run: claude plugin marketplace add giancarloerra/socraticode && claude plugin install socraticode@socraticode", b.String(), Red("✘"))
		}
		return "", fmt.Errorf("%s\n  %s plugin update failed: %v", b.String(), Red("✘"), err)
	}
	after := socratiCodePluginVersion()
	switch {
	case before == after:
		b.WriteString("\n  " + Green("✔") + " plugin already latest " + Dim(after))
	default:
		b.WriteString("\n  " + Green("✔") + " plugin " + Dim(before) + " → " + after)
	}
	b.WriteString("\n" + Section("Next") + "\n" + KV([][2]string{
		{"  1", "/plugin → reconnect " + Cyan("socraticode") + " " + Dim("(or restart Claude Code — the MCP runs npx socraticode@latest on start)")},
		{"  2", Cyan("codebase_health") + " " + Dim("· Ollama/Qdrant are not touched: update them where they run (server: ollama / qdrant binaries)")},
	}))
	return b.String(), nil
}

func socratiCodePluginVersion() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}
	return latestDir(filepath.Join(home, ".claude", "plugins", "cache", "socraticode", "socraticode"))
}
