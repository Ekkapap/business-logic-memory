package blm

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Scan สำรวจโครงสร้าง repo แบบทั่วไป (ไม่ hardcode ชื่อโปรเจ็คใด — เจ้าของย้ำ 2026-09-09) เพื่อให้ /blm_init เสนอหัวข้อหลักได้:
//   - รวม ignore จาก .gitignore + .socraticodeignore + .ignorememory
//   - นับไฟล์/ขนาด/ภาษาต่อโฟลเดอร์ชั้นบน และประเมิน token (bytes/4)
//   - ตรวจ "โปรเจ็คย่อย": โฟลเดอร์ที่มี marker ของตัวเอง (go.mod, package.json, PLANNING.md, README …) → ผู้สมัครหัวข้อหลักแยก

var subProjectMarkers = []string{"go.mod", "package.json", "pyproject.toml", "requirements.txt", "Cargo.toml", "pom.xml", "build.gradle", "composer.json", "Gemfile", "Dockerfile", "docker-compose.yml", "PLANNING.md", "README.md", "CLAUDE.md", "wails.json", "main.go"}

// markers ที่บอกว่าเป็น "โปรเจ็ค" จริง ไม่ใช่แค่โฟลเดอร์ที่มี README
var strongMarkers = map[string]bool{"go.mod": true, "package.json": true, "pyproject.toml": true, "requirements.txt": true, "Cargo.toml": true, "pom.xml": true, "build.gradle": true, "composer.json": true, "Gemfile": true, "wails.json": true, "PLANNING.md": true}

type DirStat struct {
	Path  string         `json:"path"`
	Files int            `json:"files"`
	Bytes int64          `json:"bytes"`
	Langs map[string]int `json:"langs"`
}

type SubProject struct {
	Path    string   `json:"path"`
	Markers []string `json:"markers"`
	Docs    []string `json:"docs"`
	Files   int      `json:"files"`
	Bytes   int64    `json:"bytes"`
	Langs   []string `json:"langs"`
}

type ScanResult struct {
	Root         string       `json:"root"`
	Files        int          `json:"files"`
	Bytes        int64        `json:"bytes"`
	TokensApprox int64        `json:"tokensApprox"`
	IgnoreFiles  []string     `json:"ignoreFiles"`
	Ignored      int          `json:"ignoredFiles"`
	TopDirs      []DirStat    `json:"topDirs"`
	SubProjects  []SubProject `json:"subProjects"`
	MemoryNotes  int          `json:"memoryNotes"`
	Docs         []string     `json:"docs"`
	Warning      string       `json:"warning,omitempty"`
}

// ScanRepo สำรวจ root (หรือ sub path) · tokenWarn = เพดาน token ที่ควรเตือน (0 = 200k)
func ScanRepo(root, sub string, tokenWarn int64) ScanResult {
	if tokenWarn <= 0 {
		tokenWarn = 200_000
	}
	start := root
	if sub != "" {
		start = filepath.Join(root, sub)
	}
	ig, files := loadIgnores(root)
	res := ScanResult{Root: start, IgnoreFiles: files, TopDirs: []DirStat{}, SubProjects: []SubProject{}, Docs: []string{}}
	top := map[string]*DirStat{}
	markersByDir := map[string][]string{}
	dirFiles := map[string]*DirStat{}
	_ = filepath.WalkDir(start, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			base := d.Name()
			// dot-dir: ข้ามทั้งหมดยกเว้น mirror memory ของ AgentsRoom (เป็น knowledge)
			if strings.HasPrefix(base, ".") && rel != ".agentsroom" && !strings.HasPrefix(rel, ".agentsroom/memory") {
				return filepath.SkipDir
			}
			if rel == ".agentsroom" {
				return nil
			}
			if ig.match(rel, true) {
				res.Ignored++
				return filepath.SkipDir
			}
			return nil
		}
		if ig.match(rel, false) || strings.HasPrefix(d.Name(), ".") {
			res.Ignored++
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		res.Files++
		res.Bytes += info.Size()
		dir := filepath.ToSlash(filepath.Dir(rel))
		if strings.HasPrefix(rel, ".agentsroom/memory/") && strings.HasSuffix(rel, ".md") {
			res.MemoryNotes++
		}
		if dir != "." {
			for _, m := range subProjectMarkers {
				if d.Name() == m {
					markersByDir[dir] = append(markersByDir[dir], m)
				}
			}
		} else if strings.HasSuffix(d.Name(), ".md") {
			res.Docs = append(res.Docs, rel)
		}
		lang := langOf(d.Name())
		// สถิติต่อโฟลเดอร์ชั้นบน
		first := rel
		if i := strings.Index(rel, "/"); i >= 0 {
			first = rel[:i]
		} else {
			first = "(root files)"
		}
		ds := top[first]
		if ds == nil {
			ds = &DirStat{Path: first, Langs: map[string]int{}}
			top[first] = ds
		}
		ds.Files++
		ds.Bytes += info.Size()
		if lang != "" {
			ds.Langs[lang]++
		}
		// สถิติต่อทุกโฟลเดอร์ (ใช้กับ sub-project)
		for cur := dir; cur != "." && cur != ""; cur = filepath.ToSlash(filepath.Dir(cur)) {
			fs := dirFiles[cur]
			if fs == nil {
				fs = &DirStat{Path: cur, Langs: map[string]int{}}
				dirFiles[cur] = fs
			}
			fs.Files++
			fs.Bytes += info.Size()
			if lang != "" {
				fs.Langs[lang]++
			}
			if cur == filepath.ToSlash(filepath.Dir(cur)) {
				break
			}
		}
		return nil
	})
	for _, ds := range top {
		res.TopDirs = append(res.TopDirs, *ds)
	}
	sort.Slice(res.TopDirs, func(i, j int) bool { return res.TopDirs[i].Bytes > res.TopDirs[j].Bytes })
	// sub-project: ต้องมี strong marker อย่างน้อยหนึ่ง และไม่ซ้อนอยู่ใต้ sub-project อื่นที่ตรวจพบแล้ว (เอาชั้นบนสุด)
	var cands []string
	for dir, ms := range markersByDir {
		for _, m := range ms {
			if strongMarkers[m] {
				cands = append(cands, dir)
				break
			}
		}
	}
	sort.Strings(cands)
	for _, dir := range cands {
		nested := false
		for _, sp := range res.SubProjects {
			if strings.HasPrefix(dir, sp.Path+"/") {
				nested = true
				break
			}
		}
		if nested {
			continue
		}
		sp := SubProject{Path: dir, Markers: markersByDir[dir]}
		sort.Strings(sp.Markers)
		for _, m := range sp.Markers {
			if strings.HasSuffix(m, ".md") {
				sp.Docs = append(sp.Docs, dir+"/"+m)
			}
		}
		if fs := dirFiles[dir]; fs != nil {
			sp.Files, sp.Bytes, sp.Langs = fs.Files, fs.Bytes, topLangs(fs.Langs, 3)
		}
		res.SubProjects = append(res.SubProjects, sp)
	}
	res.TokensApprox = res.Bytes / 4
	if res.TokensApprox > tokenWarn {
		res.Warning = fmt.Sprintf("about %s tokens to read everything (> %s) — analyse per sub-project/top dir, or lean on SocratiCode/graphify instead of reading files", humanCount(res.TokensApprox), humanCount(tokenWarn))
	}
	return res
}

func topLangs(m map[string]int, n int) []string {
	type kv struct {
		k string
		v int
	}
	var all []kv
	for k, v := range m {
		all = append(all, kv{k, v})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].v > all[j].v })
	var out []string
	for i, x := range all {
		if i >= n {
			break
		}
		out = append(out, fmt.Sprintf("%s %d", x.k, x.v))
	}
	return out
}

func langOf(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".ts", ".tsx":
		return "ts"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "js"
	case ".go":
		return "go"
	case ".py":
		return "py"
	case ".rs":
		return "rs"
	case ".java", ".kt":
		return "jvm"
	case ".php":
		return "php"
	case ".rb":
		return "rb"
	case ".sql":
		return "sql"
	case ".md", ".mdx":
		return "md"
	case ".css", ".scss":
		return "css"
	case ".html":
		return "html"
	case ".json", ".yaml", ".yml", ".toml":
		return "config"
	case ".sh", ".ps1":
		return "shell"
	}
	return ""
}

func humanCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.0fk", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

// ---- gitignore-style matcher (พอสำหรับคัดโฟลเดอร์/นามสกุล — ponytail: ไม่ทำ ** ครบสเปก) ----

type ignoreRule struct {
	pattern string
	negate  bool
	dirOnly bool
}

type ignoreSet struct{ rules []ignoreRule }

func loadIgnores(root string) (ignoreSet, []string) {
	var set ignoreSet
	var used []string
	for _, f := range []string{".gitignore", ".socraticodeignore", ".ignorememory"} {
		fh, err := os.Open(filepath.Join(root, f))
		if err != nil {
			continue
		}
		used = append(used, f)
		sc := bufio.NewScanner(fh)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			r := ignoreRule{}
			if strings.HasPrefix(line, "!") {
				r.negate = true
				line = line[1:]
			}
			if strings.HasSuffix(line, "/") {
				r.dirOnly = true
				line = strings.TrimSuffix(line, "/")
			}
			r.pattern = strings.TrimPrefix(line, "/")
			set.rules = append(set.rules, r)
		}
		fh.Close()
	}
	// ของ blm เองไม่ใช่ knowledge ของโปรเจ็ค
	set.rules = append(set.rules, ignoreRule{pattern: "node_modules", dirOnly: true}, ignoreRule{pattern: ".git", dirOnly: true})
	return set, used
}

func (s ignoreSet) match(rel string, isDir bool) bool {
	base := filepath.Base(rel)
	matched := false
	for _, r := range s.rules {
		if r.dirOnly && !isDir {
			// pattern โฟลเดอร์ก็ครอบไฟล์ใต้มันด้วย
			if !strings.HasPrefix(rel, r.pattern+"/") && !strings.Contains(rel, "/"+r.pattern+"/") {
				continue
			}
			matched = !r.negate
			continue
		}
		hit := false
		switch {
		case strings.Contains(r.pattern, "/"):
			p := strings.TrimSuffix(r.pattern, "/*")
			if ok, _ := filepath.Match(r.pattern, rel); ok {
				hit = true
			} else if strings.HasSuffix(r.pattern, "/*") && strings.HasPrefix(rel, p+"/") {
				hit = true
			} else if rel == p || strings.HasPrefix(rel, r.pattern+"/") {
				hit = true
			}
		default:
			if ok, _ := filepath.Match(r.pattern, base); ok {
				hit = true
			} else if strings.HasPrefix(rel, r.pattern+"/") || strings.Contains(rel, "/"+r.pattern+"/") {
				hit = true
			}
		}
		if hit {
			matched = !r.negate
		}
	}
	return matched
}

// RenderScan ข้อความ terminal สำหรับ `blm scan`
func RenderScan(r ScanResult) string {
	b := &strings.Builder{}
	b.WriteString(Title("blm scan — "+r.Root) + "\n\n")
	b.WriteString(KV([][2]string{
		{"Files", fmt.Sprintf("%d  (%s, ~%s tokens)  ignored %d via %s", r.Files, humanBytes(r.Bytes), humanCount(r.TokensApprox), r.Ignored, strings.Join(r.IgnoreFiles, "+"))},
		{"Memory notes", fmt.Sprintf("%d", r.MemoryNotes)},
		{"Root docs", strings.Join(r.Docs, " ")},
	}) + "\n")
	if r.Warning != "" {
		b.WriteString("\n" + Red("WARNING") + "  " + r.Warning + "\n")
	}
	b.WriteString(Section("Sub-projects  (own markers → candidate main topics)") + "\n")
	if len(r.SubProjects) == 0 {
		b.WriteString("none\n")
	} else {
		var rows [][]string
		for _, sp := range r.SubProjects {
			rows = append(rows, []string{Cyan(sp.Path), fmt.Sprintf("%d files · %s", sp.Files, humanBytes(sp.Bytes)), strings.Join(sp.Langs, " "), strings.Join(sp.Markers, " ")})
		}
		b.WriteString(Table([]string{"Path", "Size", "Langs", "Markers"}, rows) + "\n")
	}
	b.WriteString(Section("Top-level dirs") + "\n")
	var rows [][]string
	for i, d := range r.TopDirs {
		if i >= 25 {
			break
		}
		rows = append(rows, []string{d.Path, fmt.Sprintf("%d", d.Files), humanBytes(d.Bytes), strings.Join(topLangs(d.Langs, 3), " ")})
	}
	b.WriteString(Table([]string{"Dir", "Files", "Size", "Langs"}, rows) + "\n")
	return b.String()
}
