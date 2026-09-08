package blm

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ToolStatus เครื่องมือช่วยวิเคราะห์โค้ด/repo หนึ่งตัว (socraticode / graphify / obsidian) — ลด token ของ agent
// คนละเรื่องกับ backend (agentsroom) ซึ่งเป็นที่เก็บ memory หลัก (เจ้าของแยกให้ชัด 2026-09-09)
type ToolStatus struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	// Used = มีร่องรอยว่าโปรเจ็คนี้ใช้จริง (ไฟล์ config/ผลลัพธ์ในโปรเจ็ค หรือถูกพูดถึงใน CLAUDE.md) ไม่ใช่แค่ติดตั้งระดับเครื่อง
	Used bool `json:"used"`
	// Applied = เลือกไว้ใน config.Tools ตอน init
	Applied bool              `json:"applied"`
	Running *bool             `json:"running,omitempty"`
	Detail  map[string]string `json:"detail,omitempty"`
	Hint    string            `json:"hint,omitempty"`
}

// BackendStatus ที่เก็บ memory หลัก (agentsroom) — ไม่ใช่ tool
type BackendStatus struct {
	Name         string `json:"name"`
	Running      *bool  `json:"running,omitempty"`
	MirrorFound  bool   `json:"mirrorFound"`
	IndexUpdated string `json:"indexUpdated,omitempty"`
}

type Status struct {
	Root        string        `json:"root"`
	Backend     Backend       `json:"backend"`
	Store       string        `json:"store"`
	Mirror      string        `json:"mirror,omitempty"`
	RulesFile   string        `json:"rulesFile,omitempty"`
	Topics      int           `json:"topics"`
	Rules       int           `json:"rules"`
	LastReadAt  string        `json:"lastReadAt,omitempty"`
	Notes       []Note        `json:"notes"`
	NotesBytes  int64         `json:"notesBytes"`
	History     int           `json:"history"`
	Reports     int           `json:"reports"`
	Tools       []ToolStatus  `json:"tools"`
	BackendInfo BackendStatus `json:"backendInfo"`
	Gain        Gain          `json:"gain"`
	ConfigFound bool          `json:"configFound"`
	// readiness (เจ้าของ 2026-09-09: status ต้องบอกก่อนว่า "ทำงานได้ ติดต่อได้ พร้อมทำงาน")
	Ready         bool     `json:"ready"`
	Problems      []string `json:"problems"`
	Binary        string   `json:"binary"`
	StoreWritable bool     `json:"storeWritable"`
	MirrorFound   bool     `json:"mirrorFound"`
}

func (s *Store) Status(c Config) Status {
	st := Status{Root: s.Root, Backend: c.Backend, Store: c.Store, Mirror: c.Mirror, Notes: []Note{}, Gain: s.GainSummary(), Problems: []string{}}
	_, err := os.Stat(filepath.Join(s.Root, ConfigFile))
	st.ConfigFound = err == nil
	st.Binary, _ = os.Executable()
	st.StoreWritable = writable(s.Dir)
	if !st.StoreWritable {
		st.Problems = append(st.Problems, "store "+c.Store+" not writable")
	}
	if c.Mirror != "" {
		st.MirrorFound = s.HasMirror()
		if !st.MirrorFound {
			st.Problems = append(st.Problems, "mirror "+c.Mirror+" missing")
		}
	}
	st.Ready = len(st.Problems) == 0
	if p := s.RulesPath(); p != "" {
		st.RulesFile = s.rel(p)
		st.Topics, st.Rules = s.TopicCount()
	}
	if raw, err := os.ReadFile(filepath.Join(s.Dir, ".rules-read.json")); err == nil {
		st.LastReadAt = strings.Trim(strings.TrimPrefix(strings.TrimSuffix(string(raw), "}"), `{"At":`), `" `)
	}
	for _, n := range s.List() {
		st.NotesBytes += int64(len(n.Content))
		n.Content = ""
		st.Notes = append(st.Notes, n)
	}
	st.History = countFiles(filepath.Join(s.Dir, "history"))
	st.Reports = countFiles(filepath.Join(s.Dir, "reports"))
	st.Tools = configuredTools(DetectTools(s.Root, c), c)
	st.BackendInfo = BackendStatus{Name: string(c.Backend), MirrorFound: st.MirrorFound}
	if c.Backend == BackendAgentsRoom {
		r := processRunning("AgentsRoom")
		st.BackendInfo.Running = &r
		if idx, err := os.Stat(filepath.Join(s.Root, c.Mirror, "INDEX.md")); err == nil {
			st.BackendInfo.IndexUpdated = idx.ModTime().Format("2006-01-02 15:04")
		}
	}
	return st
}

// writable ลองเขียนไฟล์ชั่วคราวจริง (สร้าง dir ถ้ายังไม่มีแล้วลบทิ้ง) — สิทธิ์บนดิสก์ตอบได้ทางเดียวคือลองเขียน
func writable(dir string) bool {
	created := false
	if _, err := os.Stat(dir); err != nil {
		if os.MkdirAll(dir, 0o755) != nil {
			return false
		}
		created = true
	}
	f, err := os.CreateTemp(dir, ".blm-probe-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	if created {
		os.Remove(dir)
	}
	return true
}

func countFiles(dir string) int {
	entries, _ := os.ReadDir(dir)
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			n++
		}
	}
	return n
}

// DetectTools ตรวจเครื่องมือช่วยวิเคราะห์ (ลำดับที่เจ้าของกำหนด: SocratiCode > Obsidian > graphify)
// ponytail: ตรวจแบบเบา ๆ (มีไฟล์/มี binary/พอร์ตตอบ) ไม่ต่อ docker API เพราะแค่จะบอกว่า "มีให้ใช้ไหม ใช้ในโปรเจ็คนี้ไหม"
func DetectTools(root string, c Config) []ToolStatus {
	claudeMD := strings.ToLower(readFileOr(filepath.Join(root, "CLAUDE.md")))
	mentioned := func(name string) bool { return strings.Contains(claudeMD, name) }
	var out []ToolStatus

	// socraticode: plugin cache ของ Claude Code + Qdrant (6333) / Ollama (11434) · ใช้ในโปรเจ็ค = มี .socraticodeignore / context artifacts / พูดถึงใน CLAUDE.md
	sc := ToolStatus{Name: "socraticode", Detail: map[string]string{}}
	if home, _ := os.UserHomeDir(); home != "" {
		if v := latestDir(filepath.Join(home, ".claude", "plugins", "cache", "socraticode", "socraticode")); v != "" {
			sc.Installed = true
			sc.Detail["plugin"] = v
		}
	}
	if exists(filepath.Join(root, ".socraticodeignore")) || exists(filepath.Join(root, ".socraticodecontextartifacts.json")) || mentioned("socraticode") {
		sc.Used = true
	}
	if sc.Installed {
		// native ports (6333/11434) หรือคอนเทนเนอร์ที่ SocratiCode จัดการเอง (16333/11435)
		q := httpUp("http://127.0.0.1:6333/collections") || httpUp("http://127.0.0.1:16333/collections")
		o := httpUp("http://127.0.0.1:11434/api/tags") || httpUp("http://127.0.0.1:11435/api/tags")
		sc.Detail["qdrant"] = upDown(q)
		sc.Detail["ollama"] = upDown(o)
		r := q && o
		sc.Running = &r
	} else {
		sc.Hint = "blm tools install socraticode"
	}
	out = append(out, sc)

	// obsidian: app + vault (.obsidian/) ในโปรเจ็ค
	ob := ToolStatus{Name: "obsidian", Detail: map[string]string{}}
	if p := obsidianApp(); p != "" {
		ob.Installed = true
		ob.Detail["app"] = p
		r := processRunning("Obsidian")
		ob.Running = &r
	} else {
		ob.Hint = "blm tools install obsidian"
	}
	if exists(filepath.Join(root, ".obsidian")) {
		ob.Used = true
		ob.Detail["vault"] = ".obsidian/"
	}
	out = append(out, ob)

	// graphify: CLI ระดับเครื่อง · ใช้ในโปรเจ็ค = มี graphify-out/ หรือพูดถึงใน CLAUDE.md
	gr := ToolStatus{Name: "graphify", Detail: map[string]string{}}
	if p, err := exec.LookPath("graphify"); err == nil {
		gr.Installed = true
		gr.Detail["bin"] = p
	} else {
		gr.Hint = "blm tools install graphify"
	}
	if fi, err := os.Stat(filepath.Join(root, "graphify-out", "graph.json")); err == nil {
		gr.Used = true
		gr.Detail["graph"] = "graphify-out/graph.json (" + fi.ModTime().Format("2006-01-02") + ")"
	} else if mentioned("graphify") {
		gr.Used = true
	}
	out = append(out, gr)
	return out
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func readFileOr(p string) string {
	raw, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(raw)
}

// configuredTools ติดธง Applied ตาม config.Tools แล้วคัดออกตัวที่ทั้งไม่ได้ติดตั้งและไม่ได้เลือก (ไม่มีอะไรจะบอก)
func configuredTools(all []ToolStatus, c Config) []ToolStatus {
	chosen := map[string]bool{}
	for _, t := range c.Tools {
		chosen[t] = true
	}
	var out []ToolStatus
	for _, t := range all {
		t.Applied = chosen[t.Name]
		if t.Installed || t.Applied {
			out = append(out, t)
		}
	}
	return out
}

// ToolRows แถวตารางเครื่องมือ (ใช้ร่วมกันใน status และ tools status): ไอคอนแบบ plugin status ของ Claude Code
//
//	✔ = เลือกใช้ในโปรเจ็คนี้ (applied) · ○ = ติดตั้งระดับเครื่องแต่ไม่ได้ใช้ในโปรเจ็คนี้ · ✘ = เลือกไว้แต่ยังไม่ติดตั้ง
func ToolRows(tools []ToolStatus) [][]string {
	var rows [][]string
	for _, t := range tools {
		switch {
		case t.Applied && t.Installed:
			state := "active in this project"
			if !t.Used {
				state = "applied, no project files yet"
			}
			if t.Running != nil {
				if *t.Running {
					state += " · " + Green("running")
				} else {
					state += " · " + Dim("stopped")
				}
			}
			var det []string
			for _, k := range sortedKeys(t.Detail) {
				det = append(det, k+"="+t.Detail[k])
			}
			rows = append(rows, []string{Green("✔") + " " + Cyan(t.Name), state, strings.Join(det, "  ")})
		case t.Applied:
			rows = append(rows, []string{Red("✘") + " " + Cyan(t.Name), "not installed", "→ " + t.Hint})
		default:
			rows = append(rows, []string{Dim("○") + " " + Dim(t.Name), Dim("installed globally, not used in this project"), ""})
		}
	}
	return rows
}

func upDown(b bool) string {
	if b {
		return "up"
	}
	return "down"
}

func httpUp(url string) bool {
	cl := http.Client{Timeout: 800 * time.Millisecond}
	resp, err := cl.Get(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 500
}

func latestDir(dir string) string {
	entries, _ := os.ReadDir(dir)
	best := ""
	for _, e := range entries {
		if e.IsDir() && e.Name() > best {
			best = e.Name()
		}
	}
	return best
}

func processRunning(name string) bool {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("tasklist", "/FI", "IMAGENAME eq "+name+".exe", "/NH")
	} else {
		cmd = exec.Command("pgrep", "-f", name)
	}
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), strings.ToLower(name)) || (runtime.GOOS != "windows" && len(strings.TrimSpace(string(out))) > 0)
}

func obsidianApp() string {
	cands := []string{"/Applications/Obsidian.app"}
	if home, _ := os.UserHomeDir(); home != "" {
		cands = append(cands, filepath.Join(home, "Applications", "Obsidian.app"), filepath.Join(home, "AppData", "Local", "Obsidian", "Obsidian.exe"))
	}
	for _, p := range cands {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, bin := range []string{"obsidian", "Obsidian"} {
		if p, err := exec.LookPath(bin); err == nil {
			return p
		}
	}
	return ""
}

// RenderStatus หน้าตาแบบ `rtk gain` (เจ้าของ 2026-09-09): บรรทัดแรกตอบว่าพร้อมทำงานไหม แล้วค่อยลงรายละเอียดเป็น section
// สีมีเฉพาะเมื่อ Color เปิด (stdout เป็น terminal) · ไม่มี markdown เพราะ terminal ไม่เรนเดอร์
func RenderStatus(st Status) string {
	ready := Green("READY")
	if !st.Ready {
		ready = Red("NOT READY") + "  " + strings.Join(st.Problems, "; ")
	}
	b := &strings.Builder{}
	b.WriteString(Title(fmt.Sprintf("blm %s — Status", Version)) + "\n\n")
	b.WriteString(KV([][2]string{{"State", ready}}) + "\n\n")

	cfg := ConfigFile + "  (backend " + string(st.Backend) + ")"
	if !st.ConfigFound {
		cfg = "none — run `blm init`  (backend " + string(st.Backend) + " guessed)"
	}
	store := st.Store + "  " + Green("writable")
	if !st.StoreWritable {
		store = st.Store + "  " + Red("not writable")
	}
	rows := [][2]string{{"Binary", st.Binary}, {"Project", st.Root}, {"Config", cfg}, {"Store", store}}
	// backend = ที่เก็บ memory หลัก แยกจาก tools ชัด ๆ
	be := Green("✔") + " " + st.BackendInfo.Name
	if st.Backend == BackendNone {
		be = Dim("none — files in store are the truth")
	}
	if st.BackendInfo.Running != nil {
		if *st.BackendInfo.Running {
			be += " · " + Green("running")
		} else {
			be += " · " + Dim("not running")
		}
	}
	if st.Mirror != "" {
		m := Green("ok")
		if !st.MirrorFound {
			m = Red("missing")
		}
		be += " · mirror " + st.Mirror + " " + m
		if st.BackendInfo.IndexUpdated != "" {
			be += " · index " + st.BackendInfo.IndexUpdated
		}
	}
	rows = append(rows, [2]string{"Backend", be})
	b.WriteString(KV(rows) + "\n")

	b.WriteString(Section("Rules") + "\n")
	if st.RulesFile != "" {
		rr := [][2]string{{"File", st.RulesFile}, {"Topics", itoa(st.Topics)}, {"Rules", itoa(st.Rules)}}
		if st.LastReadAt != "" {
			rr = append(rr, [2]string{"Last read", shortTime(st.LastReadAt)})
		}
		b.WriteString(KV(rr) + "\n")
	} else {
		b.WriteString("No blm.md yet — run /blm_init in Claude Code\n")
	}

	b.WriteString(Section(fmt.Sprintf("Temp Notes  (%d files · %s · history %d · reports %d)", len(st.Notes), humanBytes(st.NotesBytes), st.History, st.Reports)) + "\n")
	if len(st.Notes) > 0 {
		var tr [][]string
		for i, n := range st.Notes {
			tr = append(tr, []string{itoa(i+1) + ".", Cyan(n.Name), n.Target, n.Mode, shortTime(n.UpdatedAt)})
		}
		b.WriteString(Table([]string{"#", "Name", "Target", "Mode", "Updated"}, tr) + "\n")
	} else {
		b.WriteString("none\n")
	}

	b.WriteString(Section("Tools  (code/repo analysis — cut agent token usage)") + "\n")
	if rows := ToolRows(st.Tools); len(rows) == 0 {
		b.WriteString("none — blm init … --tools socraticode,obsidian,graphify\n")
	} else {
		b.WriteString(Table([]string{"Tool", "State", "Detail"}, rows) + "\n")
	}

	b.WriteString(RenderGain(st.Gain))
	return b.String()
}

// shortTime "2026-09-08T12:58:43.996Z" → "2026-09-08 12:58" (พอสำหรับตาราง)
func shortTime(iso string) string {
	if len(iso) >= 16 {
		return iso[:10] + " " + iso[11:16]
	}
	return iso
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
