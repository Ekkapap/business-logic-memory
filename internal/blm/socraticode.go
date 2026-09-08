package blm

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// SocratiCode graph ใน Qdrant (เจ้าของสั่ง 2026-09-09: มีข้อมูลอยู่แล้ว ดึงมาทำกราฟเอง ดีกว่า parse ซ้ำ)
// โครงจากซอร์ส socraticode 1.13.1 (src/config.ts, services/qdrant.ts, types.ts):
//   projectId              = sha256(abs project path)[:12]   (ไม่มี .socraticode.json / SOCRATICODE_PROJECT_ID)
//   กราฟไฟล์ (import)       = payload.graphData (JSON string ของ {nodes[{relativePath, imports, language}], edges[{source,target,type}]})
//                            ใน collection `socraticode_metadata` point id = uuid จาก sha256("codegraph_<projectId>")[:32]
//   symbol + call ต่อไฟล์   = collection `<projectId>_symgraph_file` 1 point/ไฟล์ payload {file, language, symbols[], outgoingCalls[]}
//                            calleeCandidates 1 ตัว = resolve แน่ → edge call ไปไฟล์ของ candidate (id = "<file>::<name>#<line>")
// ทั้งหมดผ่าน REST ของ Qdrant (QDRANT_URL หรือ 127.0.0.1:6333 / 16333) ไม่ต้องมี MCP ของ SocratiCode

func qdrantBase() string {
	if u := os.Getenv("QDRANT_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	if home, _ := os.UserHomeDir(); home != "" {
		if raw, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json")); err == nil {
			var s struct {
				Env map[string]string `json:"env"`
			}
			if json.Unmarshal(raw, &s) == nil && s.Env["QDRANT_URL"] != "" {
				return strings.TrimRight(s.Env["QDRANT_URL"], "/")
			}
		}
	}
	for _, u := range []string{"http://127.0.0.1:6333", "http://127.0.0.1:16333"} {
		if httpUp(u + "/collections") {
			return u
		}
	}
	return ""
}

func qdrantPost(base, path string, body any, out any) error {
	raw, _ := json.Marshal(body)
	cl := http.Client{Timeout: 20 * time.Second}
	resp, err := cl.Post(base+path, "application/json", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("qdrant %s → %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// SocratiCodeProjectID = sha256(abs path)[:12] เหมือน coreProjectId ของ SocratiCode (override ด้วย .socraticode.json.projectId ถ้ามี)
func SocratiCodeProjectID(root string) string {
	if raw, err := os.ReadFile(filepath.Join(root, ".socraticode.json")); err == nil {
		var c struct {
			ProjectID string `json:"projectId"`
		}
		if json.Unmarshal(raw, &c) == nil && c.ProjectID != "" {
			return c.ProjectID
		}
	}
	abs, _ := filepath.Abs(root)
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:])[:12]
}

func metadataPointID(collName string) string {
	sum := sha256.Sum256([]byte(collName))
	h := hex.EncodeToString(sum[:])[:32]
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

type scFileGraph struct {
	Nodes []struct {
		RelativePath string   `json:"relativePath"`
		Language     string   `json:"language"`
		Imports      []string `json:"imports"`
	} `json:"nodes"`
	Edges []struct {
		Source, Target, Type string
	} `json:"edges"`
}

type scFilePayload struct {
	File     string `json:"file"`
	Language string `json:"language"`
	Symbols  []struct {
		Name string `json:"name"`
		Kind string `json:"kind"`
		Line int    `json:"line"`
	} `json:"symbols"`
	OutgoingCalls []struct {
		CalleeName       string   `json:"calleeName"`
		CalleeCandidates []string `json:"calleeCandidates"`
		Kind             string   `json:"kind"` // "import" (แค่ import ชื่อมา) | "call" (เรียกจริง)
	} `json:"outgoingCalls"`
}

// scFilePoint = payload จริงใน Qdrant ห่อ filePayload อีกชั้น (เห็นจากข้อมูลจริง 2026-09-09 ไม่ตรงกับ type ในซอร์ส)
type scFilePoint struct {
	FilePayload scFilePayload `json:"filePayload"`
}

// FetchSocraticodeGraph ดึงกราฟจาก Qdrant · คืน error เมื่อไม่มี Qdrant/ไม่มีกราฟของโปรเจ็คนี้ (caller ถอยไป ast-grep)
func FetchSocraticodeGraph(root string) (Graph, error) {
	base := qdrantBase()
	if base == "" {
		return Graph{}, fmt.Errorf("qdrant not reachable")
	}
	prefix := os.Getenv("QDRANT_COLLECTION_PREFIX")
	pid := SocratiCodeProjectID(root)
	// ดึงสองอย่างพร้อมกัน: กราฟไฟล์ (metadata point เดียว) และ symbol/call ต่อไฟล์ (scroll ตาม cursor จึงต่อเนื่องภายในตัวเอง)
	files := map[string]*scFilePayload{}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		var offset any
		for {
			var page struct {
				Result struct {
					Points []struct {
						Payload scFilePoint `json:"payload"`
					} `json:"points"`
					NextPageOffset any `json:"next_page_offset"`
				} `json:"result"`
			}
			body := map[string]any{"limit": 500, "with_payload": true, "with_vector": false}
			if offset != nil {
				body["offset"] = offset
			}
			if err := qdrantPost(base, "/collections/"+prefix+pid+"_symgraph_file/points/scroll", body, &page); err != nil {
				return // ไม่มี symbol graph ก็ยังได้กราฟไฟล์
			}
			for _, p := range page.Result.Points {
				pl := p.Payload.FilePayload
				if pl.File == "" {
					continue
				}
				files[filepath.ToSlash(pl.File)] = &pl
			}
			if page.Result.NextPageOffset == nil || len(page.Result.Points) == 0 {
				return
			}
			offset = page.Result.NextPageOffset
		}
	}()
	// 1) กราฟไฟล์จาก metadata
	var meta struct {
		Result []struct {
			Payload struct {
				GraphData string `json:"graphData"`
			} `json:"payload"`
		} `json:"result"`
	}
	collGraph := prefix + "codegraph_" + pid
	err := qdrantPost(base, "/collections/"+prefix+"socraticode_metadata/points", map[string]any{"ids": []string{metadataPointID(collGraph)}, "with_payload": true}, &meta)
	wg.Wait()
	if err != nil {
		return Graph{}, fmt.Errorf("socraticode metadata: %v", err)
	}
	if len(meta.Result) == 0 || meta.Result[0].Payload.GraphData == "" {
		return Graph{}, fmt.Errorf("no socraticode graph for project id %s (run codebase_index / codebase_graph_build first)", pid)
	}
	var fg scFileGraph
	if err := json.Unmarshal([]byte(meta.Result[0].Payload.GraphData), &fg); err != nil {
		return Graph{}, err
	}
	return buildFromSocraticode(root, fg, files), nil
}

// buildFromSocraticode แปลงข้อมูล SocratiCode เป็น Graph ของ blm (แยกไว้ให้ test ได้โดยไม่มี Qdrant)
func buildFromSocraticode(root string, fg scFileGraph, files map[string]*scFilePayload) Graph {
	nodes := map[string]*GraphNode{}
	add := func(rel, lang string) *GraphNode {
		rel = filepath.ToSlash(rel)
		if n := nodes[rel]; n != nil {
			return n
		}
		l := langOf(rel)
		if l == "" {
			l = strings.ToLower(lang)
		}
		nodes[rel] = &GraphNode{Path: rel, Lang: l}
		return nodes[rel]
	}
	for _, n := range fg.Nodes {
		add(n.RelativePath, n.Language)
	}
	edgeSet := map[string]GraphEdge{}
	addEdge := func(from, to, kind string) {
		from, to = filepath.ToSlash(from), filepath.ToSlash(to)
		if from == to || nodes[from] == nil || nodes[to] == nil {
			return
		}
		k := from + "->" + to
		if _, ok := edgeSet[k]; ok {
			return
		}
		edgeSet[k] = GraphEdge{From: from, To: to, Kind: kind}
		nodes[from].Out++
		nodes[to].In++
	}
	for _, e := range fg.Edges {
		addEdge(e.Source, e.Target, "import")
	}
	calls := 0
	for rel, f := range files {
		n := add(rel, f.Language)
		var syms []string
		for _, s := range f.Symbols {
			if s.Kind == "variable" || s.Kind == "property" || s.Kind == "module" {
				continue
			}
			syms = append(syms, s.Name)
		}
		n.Symbols = uniq(syms)
		for _, c := range f.OutgoingCalls {
			if len(c.CalleeCandidates) != 1 {
				continue // 0 = external · >1 = กำกวม ตามนิยามของ SocratiCode
			}
			target := c.CalleeCandidates[0]
			if i := strings.Index(target, "::"); i >= 0 {
				target = target[:i]
			}
			kind := c.Kind
			if kind != "call" {
				kind = "import"
			}
			addEdge(rel, target, kind)
			// call ไปไฟล์ที่ import อยู่แล้ว → edge เดิมมีอยู่ก่อน (kind import) ยกระดับเป็น call และนับ (ไม่งั้น calls เป็น 0 ทั้งกราฟ เห็นบน NPM-PORTAL 2026-09-09: 2,869 call entries นับได้ 0)
			k := filepath.ToSlash(rel) + "->" + filepath.ToSlash(target)
			if e, ok := edgeSet[k]; ok && kind == "call" && e.Kind != "call" {
				e.Kind = "call"
				edgeSet[k] = e
				calls++
			}
		}
	}
	g := Graph{Root: root, BuiltAt: time.Now().UTC().Format(time.RFC3339), Engine: "socraticode", Calls: calls, Files: len(nodes)}
	var paths []string
	for p := range nodes {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		g.Nodes = append(g.Nodes, *nodes[p])
		if nodes[p].In == 0 && nodes[p].Out == 0 {
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
	return g
}
