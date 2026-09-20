package blm

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// blm tools socraticode <fn> — เรียกฟังก์ชันของ SocratiCode เอง (ไม่ใช่ action ของ blm tools) ผ่าน MCP ของมัน
// (เจ้าของ 2026-09-20: "blm tools socraticode index — เรียกฟังก์ชันของ socraticode ไม่ได้เรียกฟังก์ชันของ blm tools")
// blm spawn `npx -y --prefer-online socraticode@latest` เอง (คำสั่งเดียวกับ .mcp.json ของ plugin) พร้อม env ของ remote/local จาก
// .claude/blm.json จึงสั่ง index ได้จากเทอร์มินัล/agent ไหนก็ได้ โดยไม่ต้องเปิด session ในโปรเจ็คนั้น
//   index   = codebase_index แล้วรอ: index รันในโปรเซสของ MCP → ต้องคงโปรเซสไว้และ poll codebase_status จนไม่มี "in progress"
//   อื่น ๆ  = ชื่อสั้น (status, health, update, graph_build …) → codebase_<fn> · ชื่อเต็มก็รับ · args เพิ่มจาก --args '{json}'
func toolsSocratiCodeCall(root string, c Config, fn, argsJSON string, out io.Writer) (string, error) {
	fn = strings.TrimSpace(fn)
	if fn == "" {
		return "", fmt.Errorf("blm tools socraticode <fn> — index · status · health · update · graph_build · list_projects … (= codebase_<fn>)")
	}
	name := fn
	switch fn { // ชื่อสั้นของเรา → ฟังก์ชันของ socraticode
	case "graph":
		name = "codebase_graph_build"
	}
	if !strings.HasPrefix(name, "codebase_") {
		name = "codebase_" + name
	}
	args := map[string]any{}
	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("--args must be JSON: %w", err)
		}
	}
	abs, _ := filepath.Abs(root)
	if _, ok := args["projectPath"]; !ok {
		args["projectPath"] = abs
	}
	logFile := scLog(root, c)
	call := func(n string, a map[string]any) (string, error) { return ScCall(root, c, n, a, 30*time.Minute, true) }
	status := func() (string, error) { return call("codebase_status", map[string]any{"projectPath": abs}) }
	if fn == "shutdown" { // ปิด daemon (ให้ socraticode จบเอง) — คำสั่งเดียวที่แตะโปรเซส และต้องสั่งชัด ๆ
		return ScCall(root, c, "__shutdown__", nil, 90*time.Second, false)
	}
	if name == "codebase_remove" {
		// ตรวจสถานะก่อน (เจ้าของ 2026-09-20: ห้าม remove โดยไม่สนว่ากำลัง index อยู่): มีงานอยู่ = ไม่ลบ บอกให้รอหรือสั่ง stop เอง
		st, err := status()
		if err != nil {
			return "", fmt.Errorf("codebase_status: %w (log: %s)", err, logFile)
		}
		if strings.Contains(st, "in progress") {
			what := ""
			for _, l := range strings.Split(st, "\n") {
				if t := strings.TrimSpace(l); strings.HasPrefix(t, "⚠") || strings.HasPrefix(t, "Phase:") || strings.HasPrefix(t, "Progress:") {
					what += " · " + strings.TrimPrefix(t, "⚠ ")
				}
			}
			return "", fmt.Errorf("not removed — indexing is in progress%s\nwait for it (blm tools socraticode status) or stop it first: blm tools socraticode stop", what)
		}
		scQuiet(call, abs) // เจ้าของ 2026-09-20: หยุด watcher/งานค้างก่อนลบ และซ้ำอีกครั้งหลังลบ
		text, err := call(name, args)
		if err != nil {
			return "", fmt.Errorf("%s: %w (log: %s)", name, err, logFile)
		}
		for i := 0; i < 20; i++ {
			st, err := status()
			if err != nil {
				return "", fmt.Errorf("codebase_status: %w (log: %s)", err, logFile)
			}
			if strings.Contains(st, "No index found") {
				scQuiet(call, abs) // ลบแล้ว = ห้ามมีอะไรทำงานเองต่อ
				return text, nil
			}
			time.Sleep(1 * time.Second)
		}
		return "", fmt.Errorf("%s said %q but codebase_status still shows an index — see %s", name, strings.TrimSpace(text), logFile)
	}
	var text string
	var err error
	if name == "codebase_index" {
		// เช็คก่อน (เจ้าของ 2026-09-20): ถ้า socraticode กำลัง index อยู่แล้ว (resume ของมันเองหลังเปิด / คำสั่งก่อนหน้า) ไม่สั่งซ้อน —
		// เกาะดูความคืบหน้าของงานนั้นแทน (สั่งซ้อน = "already indexing, skipping" แล้วเราก็รอเก้อ)
		st, err := status()
		if err != nil {
			return "", fmt.Errorf("codebase_status: %w (log: %s)", err, logFile)
		}
		if strings.Contains(st, "in progress") {
			text = "attaching to the indexing already in progress"
		} else if text, err = call(name, args); err != nil {
			return "", fmt.Errorf("%s: %w (log: %s)", name, err, logFile)
		}
	} else if text, err = call(name, args); err != nil {
		return "", fmt.Errorf("%s: %w (log: %s)", name, err, logFile)
	}
	if name == "codebase_graph_build" {
		// graph ฉบับของเรา (เจ้าของ 2026-09-20): ให้ socraticode สร้าง symbol graph ลง Qdrant (แม่นกว่า ast-grep/regex เพราะมี index แล้ว)
		// แล้ว blm ดึงมาเป็น <store>/graph.json + graph.html ของตัวเอง — ที่เดียวกับ `blm graph` ตอนไม่มี tools
		fmt.Fprintln(out, text)
		st := Open(root, c)
		g, err := st.BuildGraph("")
		if err != nil {
			return "", fmt.Errorf("blm graph: %w", err)
		}
		html, _ := st.WriteGraphHTML(g)
		return fmt.Sprintf("graph: %d nodes · %d edges · %d clusters → %s · %s", len(g.Nodes), len(g.Edges), len(g.Clusters), st.rel(filepath.Join(st.Dir, "graph.json")), st.rel(html)), nil
	}
	if name == "codebase_status" { // status ก็แสดงเป็น Summary เดียวกับตอน index จบ (เจ้าของ 2026-09-20)
		if strings.Contains(text, "No index found") { // ไม่มี index = คุม off ในโปรเซสที่รันอยู่ผ่าน tool ของเขา (เจ้าของ 2026-09-20): ไม่ให้ watcher/
			// index ใด ๆ ทำงานเองจนกว่าจะสั่ง index — ทำซ้ำได้ ไม่มีผลข้างเคียง
			scQuiet(call, abs)
			text, _ = status()
		}
		return strings.TrimPrefix(indexSummary(text), "\n"), nil
	}
	if name != "codebase_index" {
		return text, nil
	}
	if !strings.Contains(text, "Indexing started") { // เช่น อีกโปรเซสกำลัง index อยู่ — ต้องเห็น ไม่ใช่นั่งดู Discovering เฉย ๆ
		fmt.Fprintln(out, text)
	}
	// index: โปรเซสต้องอยู่จนจบ — poll status ทุก 3 วิ วาดบล็อกสถานะ (Batch/Chunks/Progress/Time) ทับที่เดิม แล้วปิดด้วย Summary
	// (เจ้าของ 2026-09-20: บล็อกคอลัมน์ตรงกัน อ่านง่ายกว่าบรรทัดเดียว)
	started, drawn, lastBlock, lastLive, lastBatch, seen := time.Now(), 0, "", "", "", false
	// spinner = สี่เหลี่ยม 6 ช่อง วิ่งไปกลับ (เจ้าของ 2026-09-20: จุด braille เล็กมองไม่เห็น)
	var frames []string
	for i := 0; i < 6; i++ {
		frames = append(frames, strings.Repeat("□", i)+"■"+strings.Repeat("□", 5-i))
	}
	for i := 4; i > 0; i-- {
		frames = append(frames, strings.Repeat("□", i)+"■"+strings.Repeat("□", 5-i))
	}
	// spinner หมุนใน goroutine ของตัวเองทุก 100ms ไม่หยุดตอน poll/วาดบล็อก (เจ้าของ 2026-09-20: ต้องสม่ำเสมอ) · mu กันเขียนชนกัน
	var mu sync.Mutex
	stop := make(chan struct{})
	if Color {
		fmt.Fprint(out, "\033[?25l") // ซ่อน cursor ระหว่างวาด (เจ้าของ 2026-09-20: มี cursor โผล่หลัง spinner)
		defer fmt.Fprint(out, "\033[?25h")
		go func() {
			t := time.NewTicker(100 * time.Millisecond)
			defer t.Stop()
			for fi := 0; ; fi++ {
				select {
				case <-stop:
					return
				case <-t.C:
					mu.Lock()
					if fi%10 == 0 { // ทุก 1 วิ วาดบล็อกใหม่จากสถานะล่าสุด (Time เดินตลอด ไม่รอ % เปลี่ยน — เจ้าของ 2026-09-20)
						drawn = redraw(out, liveBlock(lastLive, time.Since(started), false), drawn)
					}
					if drawn > 0 {
						fmt.Fprintf(out, "\r\033[2K%s %s", slC.blue(frames[fi%len(frames)]), Dim("indexing…"))
					}
					mu.Unlock()
				}
			}
		}()
	}
	defer close(stop)
	for {
		time.Sleep(3 * time.Second)
		st, err := status()
		if err != nil {
			return "", fmt.Errorf("codebase_status: %w", err)
		}
		// จบเฉพาะเมื่อเคยเห็น "in progress" ของรอบนี้แล้ว และ Last operation … completed — วินาทีแรก ๆ collection เปล่า ๆ เกิดก่อน
		// index จะเริ่ม (Indexed chunks: 0, ไม่มี in progress) ต้องไม่นับว่าจบ (เจ้าของ 2026-09-20 เจอหลัง remove → index)
		if strings.Contains(st, "in progress") {
			seen = true
		}
		running := strings.Contains(st, "in progress") || strings.Contains(st, "No index found") || strings.Contains(st, "(partial)")
		if !running && seen && strings.Contains(st, "completed") {
			if strings.Contains(st, "File watcher: inactive") { // daemon ที่เปิดแบบไม่มี index (WATCHER=manual) → เปิด watcher ให้เหมือน default
				_, _ = call("codebase_watch", map[string]any{"projectPath": abs, "action": "start"})
				st, _ = status()
			}
			final := lastBatch
			if final == "" {
				final = lastLive
			}
			block := liveBlock(final, time.Since(started), true) // ค่าสุดท้ายที่มี batch (4/4, 733/733) + 100%
			mu.Lock()
			drawn = redraw(out, block, drawn)
			if Color {
				fmt.Fprint(out, "\r\033[2K") // ลบ spinner
			}
			fmt.Fprintln(out, indexSummary(st))
			mu.Unlock()
			return "", nil
		}
		if time.Since(started) > 2*time.Hour {
			return st, fmt.Errorf("index still not finished after 2h — check with blm tools socraticode status (log: %s)", logFile)
		}
		mu.Lock()
		if strings.Contains(st, "Phase:") {
			lastLive = st
			if strings.Contains(st, "(batch ") { // บล็อกสุดท้ายใช้สถานะที่ยังมี batch (phase building code graph ไม่มี → เลขหาย)
				lastBatch = st
			}
		}
		if !Color { // ถูก pipe: พิมพ์เมื่อเปลี่ยนเท่านั้น
			if block := liveBlock(st, time.Since(started), false); block != lastBlock {
				drawn = redraw(out, block, drawn)
				lastBlock = block
			}
		}
		mu.Unlock()
	}
}

// liveBlock — หัว "Generating Embeddings" (phase ปัจจุบัน) + Batch / Chunks / Progress / Time คอลัมน์ตรงกัน
func liveBlock(st string, elapsed time.Duration, done bool) string {
	phase, batch, chunks, pct := "Discovering files", "", "", ""
	for _, l := range strings.Split(st, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, "Phase:"):
			ph := strings.TrimSpace(strings.TrimPrefix(t, "Phase:"))
			if i := strings.Index(ph, "(batch "); i >= 0 {
				batch = strings.TrimSuffix(strings.TrimSpace(ph[i+7:]), ")")
				ph = strings.TrimSpace(ph[:i])
			}
			phase = strings.ToUpper(ph[:1]) + ph[1:]
		case strings.HasPrefix(t, "Progress:"):
			pr := strings.TrimSpace(strings.TrimPrefix(t, "Progress:")) // "32/733 chunks embedded (4%)"
			if f := strings.Fields(pr); len(f) > 0 {
				chunks = f[0]
			}
			if i := strings.LastIndex(pr, "("); i >= 0 {
				pct = strings.TrimSuffix(pr[i+1:], ")")
			}
		case strings.Contains(t, "ANOTHER PROCESS"):
			phase = "Waiting: " + strings.TrimPrefix(t, "⚠ ")
		}
	}
	if !strings.Contains(st, "Phase:") && !done { // ยังไม่มี phase: โชว์บรรทัดแรกที่ socraticode ตอบจริง จะได้เห็นว่าติดอะไร (เจ้าของ 2026-09-20)
		for _, l := range strings.Split(st, "\n") {
			if t := strings.TrimSpace(l); t != "" && !strings.HasPrefix(t, "Project:") && !strings.HasPrefix(t, "Collection:") && !strings.HasPrefix(t, "Status:") && !strings.HasPrefix(t, "Indexed chunks:") {
				phase = "Waiting · " + t
				break
			}
		}
	}
	if done {
		phase, pct = "Generating Embeddings", "100%"
		if i := strings.Index(batch, "/"); i >= 0 { // 3/4 → 4/4 (poll ทุก 3 วิ อาจไม่ทันเห็น batch สุดท้าย)
			batch = batch[i+1:] + batch[i:]
		}
		if chunks != "" { // 733/733
			if i := strings.Index(chunks, "/"); i >= 0 {
				chunks = chunks[i+1:] + chunks[i:]
			}
		}
	}
	rows := [][2]string{{"Batch", batch}, {"Chunks", chunks}, {"Progress", pct}, {"Time", elapsed.Round(time.Second).String()}}
	for _, l := range strings.Split(st, "\n") {
		if strings.Contains(l, "log: ") {
			rows = append(rows, [2]string{"Log", strings.TrimSpace(l[strings.Index(l, "log: ")+5:])})
		}
	}
	if Color {
		phase = slC.blue(phase) // น้ำเงินเดียวกับ statusline socraticode (เจ้าของ 2026-09-20)
	}
	return phase + "\n" + KV(rows)
}

// indexSummary — Summary สีน้ำเงิน: Project / Status / Indexed chunks / Last operation / File watcher / Code graph
func indexSummary(st string) string {
	get := func(prefix string) string {
		for _, l := range strings.Split(st, "\n") {
			if t := strings.TrimSpace(l); strings.HasPrefix(t, prefix) {
				return strings.TrimSpace(strings.TrimPrefix(t, prefix))
			}
		}
		return ""
	}
	if get("Project:") == "" { // ไม่ใช่บล็อก status ปกติ (No index found / Qdrant is not available …) → พิมพ์ตามที่ socraticode ตอบ
		return strings.TrimSpace(st)
	}
	project, coll := filepath.Base(get("Project:")), get("Collection:")
	// Status = สถานะ collection บน Qdrant (green/yellow/red/grey) → คำที่คนอ่านรู้เรื่อง (เจ้าของ 2026-09-20: "green" แปลก ๆ)
	status := get("Status:")
	switch status {
	case "green":
		status = Green("✔ Ready")
	case "yellow":
		status = Yellow("⟳ Optimizing") + Dim(" (Qdrant is still merging segments — searchable)")
	case "red":
		status = Red("✘ Error") + Dim(" (Qdrant collection unhealthy)")
	case "grey":
		status = Dim("○ Empty")
	}
	files := ""
	if f := get("Files:"); f != "" { // "160, Chunks: 733"
		files = " [" + strings.TrimSpace(strings.SplitN(f, ",", 2)[0]) + " files]"
	}
	lastOp := strings.ReplaceAll(get("Last operation:"), " — ", " -> ")
	watcher := get("File watcher:")
	if strings.HasPrefix(watcher, "active") {
		watcher = Green("Active") + Dim(strings.TrimPrefix(watcher, "active"))
	}
	graph := strings.Replace(get("Code graph:"), ", ", " [", 1)
	if strings.Contains(graph, " [") {
		graph += "]"
	}
	rows := [][2]string{{"Project", project + " [" + coll + "]"}, {"Status", status}, {"Indexed chunks", get("Indexed chunks:") + files}}
	if strings.Contains(st, "in progress") { // ตอนนี้กำลังทำอะไร (phase + progress) — เห็นจาก status เท่านั้น
		what := strings.TrimSpace(strings.TrimPrefix(get("⚠"), "⚠"))
		if ph := get("Phase:"); ph != "" {
			what += " · " + ph
		}
		if pr := get("Progress:"); pr != "" {
			what += " · " + pr
		}
		rows = append(rows, [2]string{"In progress", Yellow(what)})
	}
	if lastOp != "" {
		rows = append(rows, [2]string{"Last operation", lastOp})
	}
	if built := get("Last built:"); built != "" && graph != "" { // "171s ago (cached in memory)" → "3m ago"
		var secs int64
		if n, _ := fmt.Sscanf(built, "%ds ago", &secs); n == 1 {
			built = humanAgo(secs)
		}
		graph += Dim(" · built " + built)
	}
	rows = append(rows, [2]string{"File watcher", watcher}, [2]string{"Code graph", graph})
	head := "Summary"
	if Color {
		head = slC.blue(head)
	}
	return "\n" + head + "\n" + KV(rows)
}

// redraw — วาดบล็อกทับที่เดิมบน terminal (cursor อยู่ที่บรรทัด spinner ใต้บล็อก → \r แล้วเลื่อนขึ้นเท่าบรรทัดของบล็อก)
// ถูก pipe = พิมพ์ต่อท้ายตามปกติ ไม่มี spinner
func redraw(out io.Writer, block string, drawn int) int {
	lines := strings.Count(block, "\n") + 1
	if Color {
		if drawn > 0 {
			fmt.Fprintf(out, "\r\033[%dA", drawn)
		}
		for _, l := range strings.Split(block, "\n") {
			fmt.Fprintf(out, "\033[2K%s\n", l)
		}
		return lines
	}
	fmt.Fprintln(out, block)
	return lines
}

// cachedSocratiCode — dist/index.js ของ socraticode เวอร์ชันสูงสุดใน ~/.npm/_npx (ที่ plugin เคยรันผ่าน npx) · "" = ไม่มี
func cachedSocratiCode() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dirs, _ := filepath.Glob(filepath.Join(home, ".npm", "_npx", "*", "node_modules", "socraticode", "package.json"))
	best, bestVer := "", ""
	for _, pj := range dirs {
		raw, err := os.ReadFile(pj)
		if err != nil {
			continue
		}
		var p struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(raw, &p) != nil || p.Version == "" {
			continue
		}
		bin := filepath.Join(filepath.Dir(pj), "dist", "index.js")
		if _, err := os.Stat(bin); err != nil {
			continue
		}
		if best == "" || versionLess(bestVer, p.Version) {
			best, bestVer = bin, p.Version
		}
	}
	return best
}

func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		var x, y int
		if i < len(pa) {
			fmt.Sscanf(pa[i], "%d", &x)
		}
		if i < len(pb) {
			fmt.Sscanf(pb[i], "%d", &y)
		}
		if x != y {
			return x < y
		}
	}
	return false
}

// humanAgo — วินาทีดิบจาก socraticode → 47s / 3m / 2h / 5d / 3mo / 1y ago (เจ้าของ 2026-09-20: ผ่านไป 1 ปีจะเป็นเลขอะไร)
func humanAgo(secs int64) string {
	switch {
	case secs < 60:
		return fmt.Sprintf("%ds ago", secs)
	case secs < 3600:
		return fmt.Sprintf("%dm ago", secs/60)
	case secs < 86400:
		return fmt.Sprintf("%dh ago", secs/3600)
	case secs < 30*86400:
		return fmt.Sprintf("%dd ago", secs/86400)
	case secs < 365*86400:
		return fmt.Sprintf("%dmo ago", secs/(30*86400))
	}
	return fmt.Sprintf("%dy ago", secs/(365*86400))
}

// scQuiet — "off" ขณะรัน: หยุด watcher และ index ที่ค้างของโปรเจ็คนี้ด้วย tool ของ socraticode เอง (codebase_watch stop · codebase_stop)
func scQuiet(call func(string, map[string]any) (string, error), abs string) {
	_, _ = call("codebase_watch", map[string]any{"projectPath": abs, "action": "stop"})
	_, _ = call("codebase_stop", map[string]any{"projectPath": abs})
}
