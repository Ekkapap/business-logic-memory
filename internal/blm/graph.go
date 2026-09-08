package blm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// blm_graph — ตัวช่วยวิเคราะห์ repo ในตัว เมื่อไม่มี SocratiCode/graphify/Obsidian (เจ้าของสั่ง 2026-09-09):
// กราฟไฟล์↔ไฟล์จาก import/require/wiki-link และรายชื่อ symbol ระดับบนต่อไฟล์ ใช้ regex ต่อภาษา ไม่พึ่ง tree-sitter
// (ponytail: ตอบคำถาม "เรื่องนี้อยู่ไฟล์ไหน ใครเรียกใช้ ใครถูกเรียก" ได้พอสำหรับ /blm_init — ไม่ใช่ static analyser)
// ผลเก็บที่ <store>/graph.json ใช้ซ้ำจนกว่าจะสั่ง rebuild

type GraphNode struct {
	Path    string   `json:"path"`
	Lang    string   `json:"lang"`
	Symbols []string `json:"symbols,omitempty"`
	In      int      `json:"in"`
	Out     int      `json:"out"`
}

type GraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"` // import | link
}

type Graph struct {
	Root     string         `json:"root"`
	BuiltAt  string         `json:"builtAt"`
	Files    int            `json:"files"`
	Nodes    []GraphNode    `json:"nodes"`
	Edges    []GraphEdge    `json:"edges"`
	Hubs     []GraphNode    `json:"hubs"`     // in-degree สูงสุด (โค้ด) = โมดูลแกน
	DocHubs  []GraphNode    `json:"docHubs"`  // เอกสาร/โน้ตที่ถูกลิงก์มากสุด
	Clusters []GraphCluster `json:"clusters"` // ต่อโฟลเดอร์ชั้นสอง (src/lib, src/features/X …)
	Unlinked int            `json:"unlinked"`
	// Engine = "ast-grep" (tree-sitter จริง มี call graph) หรือ "regex" (โหมดหยาบ ไม่มี tree-sitter บนเครื่อง)
	Engine string `json:"engine"`
	Calls  int    `json:"callEdges"`
}

type GraphCluster struct {
	Dir      string   `json:"dir"`
	Files    int      `json:"files"`
	Symbols  int      `json:"symbols"`
	Internal int      `json:"internalEdges"`
	Inbound  int      `json:"inbound"`
	Outbound int      `json:"outbound"`
	Top      []string `json:"topSymbols,omitempty"`
}

var (
	reTSImport = regexp.MustCompile(`(?m)^\s*(?:import|export)\s[^;]*?\bfrom\s+['"]([^'"]+)['"]|\brequire\(\s*['"]([^'"]+)['"]\s*\)|\bimport\(\s*['"]([^'"]+)['"]\s*\)`)
	reTSSym    = regexp.MustCompile(`(?m)^\s*export\s+(?:default\s+)?(?:async\s+)?(?:function\*?|class|const|let|var|interface|type|enum)\s+([A-Za-z_$][\w$]*)`)
	reGoImport = regexp.MustCompile(`(?m)^\s*(?:import\s+)?(?:\w+\s+)?"([^"]+)"\s*$`)
	reGoSym    = regexp.MustCompile(`(?m)^func\s+(?:\([^)]*\)\s*)?([A-Z]\w*)\s*\(|^type\s+([A-Z]\w*)\b`)
	rePyImport = regexp.MustCompile(`(?m)^\s*(?:from\s+([\w.]+)\s+import|import\s+([\w.]+))`)
	rePySym    = regexp.MustCompile(`(?m)^(?:def|class)\s+([A-Za-z_]\w*)`)
	rePHPSym   = regexp.MustCompile(`(?m)^\s*(?:abstract\s+|final\s+)?(?:class|interface|trait|function)\s+([A-Za-z_]\w*)`)
	reMDLink   = regexp.MustCompile(`\[\[([^\]|#]+)(?:[|#][^\]]*)?\]\]|\]\(([^)\s]+\.md)(?:#[^)]*)?\)`)
	reMDHead   = regexp.MustCompile(`(?m)^#{1,2}\s+(.+?)\s*$`)
)

// BuildGraph สร้างกราฟจากไฟล์ที่ ScanRepo เห็น (ignore เดียวกัน) แล้วเขียน graph.json ลง store
func (s *Store) BuildGraph(sub string) (Graph, error) {
	root := s.Root
	ig, _ := loadIgnores(root)
	aliases := tsAliases(root)
	goMods := goModules(root)
	start := root
	if sub != "" {
		start = filepath.Join(root, sub)
	}
	nodes := map[string]*GraphNode{}
	var files []string
	astBin := AstGrepBin()
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
			if strings.HasPrefix(d.Name(), ".") && rel != ".agentsroom" && !strings.HasPrefix(rel, ".agentsroom/memory") {
				return filepath.SkipDir
			}
			if rel == ".agentsroom" {
				return nil
			}
			if ig.match(rel, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || ig.match(rel, false) {
			return nil
		}
		lang := langOf(d.Name())
		if lang == "" || lang == "config" || lang == "css" || lang == "html" || lang == "sql" || lang == "shell" {
			return nil
		}
		if info, err := d.Info(); err != nil || info.Size() > 1<<20 {
			return nil // ไฟล์ใหญ่กว่า 1 MB คือ generated/bundle ไม่ใช่ซอร์สที่คนอ่าน
		}
		files = append(files, rel)
		nodes[rel] = &GraphNode{Path: rel, Lang: lang}
		return nil
	})
	sort.Strings(files)
	edgeSet := map[string]GraphEdge{}
	var facts map[string]*AstFacts
	if astBin != "" {
		facts = RunAstGrep(astBin, root, files)
	}
	for _, rel := range files {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		src := string(raw)
		n := nodes[rel]
		var targets []string
		kind := "import"
		if f := facts[rel]; f != nil && n.Lang != "md" {
			// AST mode: symbol + import จาก tree-sitter · Go import ยังใช้ regex เพราะ pattern ของ import block ไม่คงที่
			n.Symbols = uniq(f.Defs)
			for _, spec := range f.Imports {
				var t string
				switch n.Lang {
				case "ts", "js":
					t = resolveTS(root, rel, spec, aliases, nodes)
				case "py":
					t = resolvePy(rel, spec, nodes)
				}
				if t != "" {
					targets = append(targets, t)
				}
			}
			if n.Lang == "go" {
				for _, m := range reGoImport.FindAllStringSubmatch(src, -1) {
					if t := resolveGo(rel, m[1], goMods, nodes); t != "" {
						targets = append(targets, t)
					}
				}
			}
		} else {
			switch n.Lang {
			case "ts", "js":
				n.Symbols = uniq(matchAll(reTSSym, src))
				for _, m := range reTSImport.FindAllStringSubmatch(src, -1) {
					spec := firstNonEmpty(m[1:])
					if t := resolveTS(root, rel, spec, aliases, nodes); t != "" {
						targets = append(targets, t)
					}
				}
			case "go":
				n.Symbols = uniq(matchAll(reGoSym, src))
				for _, m := range reGoImport.FindAllStringSubmatch(src, -1) {
					if t := resolveGo(rel, m[1], goMods, nodes); t != "" {
						targets = append(targets, t)
					}
				}
			case "py":
				n.Symbols = uniq(matchAll(rePySym, src))
				for _, m := range rePyImport.FindAllStringSubmatch(src, -1) {
					if t := resolvePy(rel, firstNonEmpty(m[1:]), nodes); t != "" {
						targets = append(targets, t)
					}
				}
			case "php", "rb", "rs", "jvm":
				n.Symbols = uniq(matchAll(rePHPSym, src))
			}
		}
		switch n.Lang {
		case "md":
			kind = "link"
			heads := matchAll(reMDHead, src)
			if len(heads) > 8 {
				heads = heads[:8]
			}
			n.Symbols = heads
			for _, m := range reMDLink.FindAllStringSubmatch(src, -1) {
				if t := resolveMD(rel, firstNonEmpty(m[1:]), nodes); t != "" {
					targets = append(targets, t)
				}
			}
		}
		for _, t := range targets {
			if t == rel {
				continue
			}
			k := rel + "->" + t
			if _, ok := edgeSet[k]; !ok {
				edgeSet[k] = GraphEdge{From: rel, To: t, Kind: kind}
				n.Out++
				nodes[t].In++
			}
		}
	}
	// call graph (AST เท่านั้น): ชื่อที่ถูกเรียกตรงกับ definition ในไฟล์อื่น → edge "call" (ชื่อที่นิยามซ้ำหลายไฟล์ข้าม เพราะชี้ไม่ได้ว่าตัวไหน)
	callEdges := 0
	if facts != nil {
		defOwner := map[string]string{}
		ambiguous := map[string]bool{}
		for rel, f := range facts {
			for _, d := range f.Defs {
				if o, ok := defOwner[d]; ok && o != rel {
					ambiguous[d] = true
				}
				defOwner[d] = rel
			}
		}
		for rel, f := range facts {
			for _, c := range f.Calls {
				owner, ok := defOwner[c]
				if !ok || ambiguous[c] || owner == rel || nodes[owner] == nil || nodes[rel] == nil {
					continue
				}
				k := rel + "->" + owner
				if _, dup := edgeSet[k]; dup {
					continue
				}
				edgeSet[k] = GraphEdge{From: rel, To: owner, Kind: "call"}
				nodes[rel].Out++
				nodes[owner].In++
				callEdges++
			}
		}
	}
	g := Graph{Root: root, BuiltAt: time.Now().UTC().Format(time.RFC3339), Files: len(files), Engine: "regex", Calls: callEdges}
	if astBin != "" {
		g.Engine = "ast-grep"
	}
	for _, rel := range files {
		g.Nodes = append(g.Nodes, *nodes[rel])
		if nodes[rel].In == 0 && nodes[rel].Out == 0 {
			g.Unlinked++
		}
	}
	for _, e := range edgeSet {
		g.Edges = append(g.Edges, e)
	}
	sort.Slice(g.Edges, func(i, j int) bool { return g.Edges[i].From+g.Edges[i].To < g.Edges[j].From+g.Edges[j].To })
	hubs := append([]GraphNode(nil), g.Nodes...)
	sort.Slice(hubs, func(i, j int) bool {
		return hubs[i].In > hubs[j].In || (hubs[i].In == hubs[j].In && hubs[i].Path < hubs[j].Path)
	})
	for _, h := range hubs {
		if h.In == 0 {
			break
		}
		row := GraphNode{Path: h.Path, Lang: h.Lang, In: h.In, Out: h.Out, Symbols: head(h.Symbols, 5)}
		if h.Lang == "md" {
			if len(g.DocHubs) < 8 {
				g.DocHubs = append(g.DocHubs, row)
			}
		} else if len(g.Hubs) < 15 {
			g.Hubs = append(g.Hubs, row)
		}
	}
	g.Clusters = clusters(g)
	_ = os.MkdirAll(s.Dir, 0o755)
	raw, _ := json.Marshal(g)
	return g, os.WriteFile(filepath.Join(s.Dir, "graph.json"), raw, 0o644)
}

// LoadGraph อ่าน graph.json ถ้ามี (ไม่ต้อง build ทุกครั้ง)
func (s *Store) LoadGraph() (Graph, bool) {
	raw, err := os.ReadFile(filepath.Join(s.Dir, "graph.json"))
	if err != nil {
		return Graph{}, false
	}
	var g Graph
	return g, json.Unmarshal(raw, &g) == nil
}

// GraphQuery ค้นไฟล์/symbol ที่มีคำนี้ แล้วคืนพร้อมเพื่อนบ้าน (ใครเรียก / เรียกใคร) — ตัดที่ limit ผลลัพธ์
type GraphHit struct {
	Path    string   `json:"path"`
	Symbols []string `json:"symbols,omitempty"`
	// UsedBy / Uses รวมทั้ง import และ call (AST mode) — ใครพึ่งไฟล์นี้ และไฟล์นี้พึ่งใคร
	UsedBy []string `json:"usedBy,omitempty"`
	Uses   []string `json:"uses,omitempty"`
}

func GraphQuery(g Graph, query string, limit int) []GraphHit {
	q := strings.ToLower(strings.TrimSpace(query))
	if limit <= 0 {
		limit = 12
	}
	in := map[string][]string{}
	out := map[string][]string{}
	for _, e := range g.Edges {
		in[e.To] = append(in[e.To], e.From)
		out[e.From] = append(out[e.From], e.To)
	}
	var hits []GraphHit
	for _, n := range g.Nodes {
		var syms []string
		for _, sName := range n.Symbols {
			if strings.Contains(strings.ToLower(sName), q) {
				syms = append(syms, sName)
			}
		}
		if len(syms) == 0 && !strings.Contains(strings.ToLower(n.Path), q) {
			continue
		}
		hits = append(hits, GraphHit{Path: n.Path, Symbols: head(syms, 8), UsedBy: head(in[n.Path], 8), Uses: head(out[n.Path], 8)})
	}
	// ไฟล์ที่ถูกเรียกมากอยู่ก่อน = แกนของเรื่องนั้น
	sort.Slice(hits, func(i, j int) bool { return len(in[hits[i].Path]) > len(in[hits[j].Path]) })
	return head(hits, limit)
}

// ---- helpers ------------------------------------------------------------------

func clusters(g Graph) []GraphCluster {
	key := func(p string) string {
		parts := strings.Split(p, "/")
		if len(parts) <= 2 {
			return parts[0]
		}
		return strings.Join(parts[:2], "/")
	}
	cs := map[string]*GraphCluster{}
	symCount := map[string]map[string]int{}
	for _, n := range g.Nodes {
		k := key(n.Path)
		c := cs[k]
		if c == nil {
			c = &GraphCluster{Dir: k}
			cs[k] = c
			symCount[k] = map[string]int{}
		}
		c.Files++
		c.Symbols += len(n.Symbols)
		for _, sName := range n.Symbols {
			symCount[k][sName] += n.In + 1
		}
	}
	for _, e := range g.Edges {
		a, b := key(e.From), key(e.To)
		if a == b {
			cs[a].Internal++
		} else {
			cs[a].Outbound++
			cs[b].Inbound++
		}
	}
	var out []GraphCluster
	for k, c := range cs {
		type kv struct {
			k string
			v int
		}
		var all []kv
		for sName, v := range symCount[k] {
			all = append(all, kv{sName, v})
		}
		sort.Slice(all, func(i, j int) bool { return all[i].v > all[j].v || (all[i].v == all[j].v && all[i].k < all[j].k) })
		for i := 0; i < len(all) && i < 5; i++ {
			c.Top = append(c.Top, all[i].k)
		}
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Inbound+out[i].Internal > out[j].Inbound+out[j].Internal })
	return out
}

func matchAll(re *regexp.Regexp, src string) []string {
	var out []string
	for _, m := range re.FindAllStringSubmatch(src, -1) {
		if v := firstNonEmpty(m[1:]); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func firstNonEmpty(ss []string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func uniq(ss []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func head[T any](xs []T, n int) []T {
	if len(xs) > n {
		return xs[:n]
	}
	return xs
}

var tsExts = []string{"", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", "/index.ts", "/index.tsx", "/index.js"}

// resolveTS แปลง specifier เป็น path ไฟล์ในกราฟ: relative, alias จาก tsconfig paths (เช่น @/ → src/), ไม่งั้น = package ข้ามไป
func resolveTS(root, from, spec string, aliases map[string]string, nodes map[string]*GraphNode) string {
	var base string
	switch {
	case strings.HasPrefix(spec, "."):
		base = filepath.ToSlash(filepath.Join(filepath.Dir(from), spec))
	default:
		for prefix, target := range aliases {
			if strings.HasPrefix(spec, prefix) {
				base = target + strings.TrimPrefix(spec, prefix)
				break
			}
		}
	}
	if base == "" {
		return ""
	}
	base = strings.TrimPrefix(filepath.ToSlash(filepath.Clean(base)), "./")
	for _, ext := range tsExts {
		if _, ok := nodes[base+ext]; ok {
			return base + ext
		}
	}
	return ""
}

// tsAliases อ่าน compilerOptions.paths จาก tsconfig.json (ตัด comment) → {"@/": "src/"}
func tsAliases(root string) map[string]string {
	out := map[string]string{}
	raw, err := os.ReadFile(filepath.Join(root, "tsconfig.json"))
	if err != nil {
		return out
	}
	clean := regexp.MustCompile(`(?m)//.*$|/\*[\s\S]*?\*/`).ReplaceAllString(string(raw), "")
	clean = regexp.MustCompile(`,\s*([}\]])`).ReplaceAllString(clean, "$1")
	var cfg struct {
		CompilerOptions struct {
			Paths map[string][]string `json:"paths"`
		} `json:"compilerOptions"`
	}
	if json.Unmarshal([]byte(clean), &cfg) != nil {
		return out
	}
	for k, v := range cfg.CompilerOptions.Paths {
		if len(v) == 0 {
			continue
		}
		out[strings.TrimSuffix(k, "*")] = strings.TrimPrefix(strings.TrimSuffix(v[0], "*"), "./")
	}
	return out
}

// goModules หา go.mod ทุกตัว → {module: dir} เพื่อแปลง import path เป็นโฟลเดอร์ (package = โฟลเดอร์ → เชื่อมทุกไฟล์ .go ในนั้น? เลือกไฟล์แรกพอ)
func goModules(root string) map[string]string {
	out := map[string]string{}
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() && (d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".")) {
			if d != nil && d.IsDir() && p != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() && d.Name() == "go.mod" {
			f, err := os.Open(p)
			if err == nil {
				sc := bufio.NewScanner(f)
				for sc.Scan() {
					if strings.HasPrefix(sc.Text(), "module ") {
						rel, _ := filepath.Rel(root, filepath.Dir(p))
						out[strings.TrimSpace(strings.TrimPrefix(sc.Text(), "module "))] = filepath.ToSlash(rel)
						break
					}
				}
				f.Close()
			}
		}
		return nil
	})
	return out
}

func resolveGo(from, imp string, mods map[string]string, nodes map[string]*GraphNode) string {
	for mod, dir := range mods {
		if imp == mod || strings.HasPrefix(imp, mod+"/") {
			pkgDir := strings.Trim(filepath.ToSlash(filepath.Join(dir, strings.TrimPrefix(imp, mod))), "/")
			pkgDir = strings.TrimPrefix(pkgDir, "./")
			// package = โฟลเดอร์: ชี้ไปไฟล์ .go ตัวแรกในโฟลเดอร์ (พอสำหรับกราฟระดับโมดูล)
			var first string
			for p := range nodes {
				if filepath.ToSlash(filepath.Dir(p)) == pkgDir && strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") && (first == "" || p < first) {
					first = p
				}
			}
			return first
		}
	}
	return ""
}

func resolvePy(from, mod string, nodes map[string]*GraphNode) string {
	if mod == "" {
		return ""
	}
	var base string
	if strings.HasPrefix(mod, ".") {
		dots := len(mod) - len(strings.TrimLeft(mod, "."))
		dir := filepath.Dir(from)
		for i := 1; i < dots; i++ {
			dir = filepath.Dir(dir)
		}
		base = filepath.ToSlash(filepath.Join(dir, strings.ReplaceAll(strings.TrimLeft(mod, "."), ".", "/")))
	} else {
		base = strings.ReplaceAll(mod, ".", "/")
	}
	for _, cand := range []string{base + ".py", base + "/__init__.py"} {
		if _, ok := nodes[cand]; ok {
			return cand
		}
	}
	return ""
}

func resolveMD(from, target string, nodes map[string]*GraphNode) string {
	if strings.Contains(target, "://") {
		return ""
	}
	if strings.HasSuffix(target, ".md") && (strings.HasPrefix(target, ".") || strings.Contains(target, "/")) {
		p := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(from), target)))
		if _, ok := nodes[p]; ok {
			return p
		}
		return ""
	}
	// [[wiki-link]] = ชื่อไฟล์ .md ที่ไหนก็ได้ (โน้ต memory)
	name := strings.TrimSuffix(target, ".md")
	for p := range nodes {
		if strings.HasSuffix(p, "/"+name+".md") || p == name+".md" {
			return p
		}
	}
	return ""
}

func engineLabel(g Graph) string {
	if g.Engine == "ast-grep" {
		return Green("ast-grep (tree-sitter)") + "  imports + definitions + cross-file calls"
	}
	return Red("regex (coarse)") + "  imports only — install tree-sitter for real AST: blm tools install tree-sitter"
}

// ClustersHead ตัดรายการ cluster (ให้ response ไม่บวม)
func ClustersHead(cs []GraphCluster, n int) []GraphCluster { return head(cs, n) }

// RenderGraph สรุปสำหรับ terminal
func RenderGraph(g Graph) string {
	b := &strings.Builder{}
	b.WriteString(Title("blm graph — "+g.Root) + "\n\n")
	b.WriteString(KV([][2]string{
		{"Built", shortTime(g.BuiltAt)},
		{"Engine", engineLabel(g)},
		{"Files", fmt.Sprintf("%d source/doc files · %d edges (%d calls) · %d unlinked", g.Files, len(g.Edges), g.Calls, g.Unlinked)},
	}) + "\n")
	b.WriteString(Section("Hubs  (most imported/called code → core modules)") + "\n")
	var rows [][]string
	for _, h := range g.Hubs {
		rows = append(rows, []string{Cyan(h.Path), fmt.Sprintf("%d", h.In), fmt.Sprintf("%d", h.Out), strings.Join(h.Symbols, " ")})
	}
	if len(rows) == 0 {
		b.WriteString("none — no resolvable imports found\n")
	} else {
		b.WriteString(Table([]string{"File", "In", "Out", "Exports"}, rows) + "\n")
	}
	if len(g.DocHubs) > 0 {
		b.WriteString(Section("Doc hubs  (most linked notes/docs)") + "\n")
		rows = nil
		for _, h := range g.DocHubs {
			title := ""
			if len(h.Symbols) > 0 {
				title = h.Symbols[0]
			}
			rows = append(rows, []string{h.Path, fmt.Sprintf("%d", h.In), fmt.Sprintf("%d", h.Out), title})
		}
		b.WriteString(Table([]string{"File", "In", "Out", "Title"}, rows) + "\n")
	}
	b.WriteString(Section("Clusters  (by folder → candidate topics)") + "\n")
	rows = nil
	for i, c := range g.Clusters {
		if i >= 25 {
			break
		}
		top := strings.Join(c.Top, " · ")
		if DisplayWidth(top) > 60 {
			top = string([]rune(top)[:57]) + "…" // หัวข้อ markdown ยาว ตัดให้ตารางไม่ล้น
		}
		rows = append(rows, []string{c.Dir, fmt.Sprintf("%d", c.Files), fmt.Sprintf("%d", c.Symbols), fmt.Sprintf("%d/%d/%d", c.Internal, c.Inbound, c.Outbound), top})
	}
	b.WriteString(Table([]string{"Dir", "Files", "Syms", "Int/In/Out", "Top symbols"}, rows) + "\n")
	return b.String()
}
