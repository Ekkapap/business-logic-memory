package blm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeIndex — Ollama /api/embed + Qdrant /points/query ที่คืน hit ชุดคงที่ (รวม .md ของ blm store / mirror / โฟลเดอร์ปกติ)
func fakeIndex(t *testing.T) (*httptest.Server, *map[string]any) {
	t.Helper()
	var lastQuery map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"bge-m3:latest"}]}`))
	})
	mux.HandleFunc("/api/embed", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "bge-m3" {
			http.Error(w, "wrong model", 400)
			return
		}
		_, _ = w.Write([]byte(`{"embeddings":[[0.1,0.2,0.3]]}`))
	})
	mux.HandleFunc("/collections", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"result":{"collections":[]}}`)) })
	mux.HandleFunc("/collections/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/points/query") {
			_ = json.NewDecoder(r.Body).Decode(&lastQuery)
			hit := func(path, lang string, score float64) string {
				return `{"score":` + jsonFloat(score) + `,"payload":{"relativePath":"` + path + `","startLine":1,"endLine":9,"language":"` + lang + `","content":"x"}}`
			}
			_, _ = w.Write([]byte(`{"result":{"points":[` + strings.Join([]string{
				hit("src/a.ts", "typescript", 1.0),
				hit(".agentsroom/memory/notes/n.md", "markdown", 0.9),
				hit(".agentsroom/blm/blm.md", "markdown", 0.8),
				hit("docs/guide.md", "markdown", 0.7),
				hit("src/b.ts", "typescript", 0.05),
			}, ",") + `]}}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/points/scroll") {
			_, _ = w.Write([]byte(`{"result":{"points":[{"payload":{"meta":{"symbolCount":10,"edgeCount":20,"fileCount":3,"builtAt":1}}}]}}`))
			return
		}
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &lastQuery
}

func jsonFloat(f float64) string { b, _ := json.Marshal(f); return string(b) }

func remoteProject(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	root := t.TempDir()
	c := Config{Backend: BackendAgentsRoom, Store: ".agentsroom/blm", Mirror: ".agentsroom/memory", SocratiCode: &SocratiCodeConfig{OllamaURL: srv.URL, QdrantURL: srv.URL, EmbeddingModel: "bge-m3", EmbeddingDimensions: "1024", EmbeddingContextLength: "8192"}}
	if err := c.Save(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestSearchFiltersAndScores(t *testing.T) {
	srv, last := fakeIndex(t)
	root := remoteProject(t, srv)

	t.Setenv("BLM_CACHE_DIR", t.TempDir())
	res, err := Search(root, Load(root), SearchOpts{Query: "cookie consent", Lang: "typescript", Full: true})
	if err != nil {
		t.Fatal(err)
	}
	// ค่าเริ่มต้น: .md ของ mirror + store หาย · docs/guide.md อยู่ · b.ts ต่ำกว่า 0.10 หาย
	var paths []string
	for _, h := range res.Hits {
		paths = append(paths, h.Path)
	}
	if strings.Join(paths, ",") != "src/a.ts,docs/guide.md" || res.Excluded != 2 {
		t.Fatalf("default filter wrong: %v excluded=%d", paths, res.Excluded)
	}
	if res.Hits[0].Score != 1.0 || res.Hits[0].Content == "" || res.Model != "bge-m3" {
		t.Fatalf("hit shape wrong: %+v", res.Hits[0])
	}
	// filter ภาษาไปถึง Qdrant ทั้ง prefetch และ query
	pf := (*last)["prefetch"].([]any)[0].(map[string]any)["filter"].(map[string]any)["must"].([]any)[0].(map[string]any)
	if pf["key"] != "language" {
		t.Fatalf("language filter not sent: %v", (*last)["prefetch"])
	}
	if !strings.Contains(res.Terminal, "src/a.ts:L1-L9") || !strings.Contains(res.Terminal, "1.00") || !strings.Contains(res.Terminal, "2 .md excluded") || !strings.Contains(res.Terminal, "search-id: "+res.SearchID) {
		t.Fatalf("terminal:\n%s", res.Terminal)
	}
	// สารบัญ (ไม่ full): ไม่มี content ใน response แต่ไฟล์ผลมี → Get เปิดได้ (ไฟล์จริงไม่มี → ใช้ content ที่เก็บ)
	toc, err := Search(root, Load(root), SearchOpts{Query: "cookie consent"})
	if err != nil {
		t.Fatal(err)
	}
	if toc.Hits[0].Content != "" || toc.Hits[0].Preview != "x" || toc.SearchID == "" {
		t.Fatalf("toc shape wrong: %+v", toc.Hits[0])
	}
	got, err := SearchGet(root, toc.SearchID, []int{1}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Hits) != 1 || got.Hits[0].Content != "x" || !strings.Contains(got.Terminal, "#1") {
		t.Fatalf("get wrong: %+v\n%s", got.Hits, got.Terminal)
	}
	if _, err := SearchGet(root, toc.SearchID, []int{99}, 0); err == nil {
		t.Fatal("unknown id must error")
	}

	// --exclude md: ทุก .md หาย · --brief: ไม่มี content
	res, err = Search(root, Load(root), SearchOpts{Query: "cookie consent", ExcludeMD: true, Brief: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Path != "src/a.ts" || res.Hits[0].Content != "" || res.Excluded != 3 {
		t.Fatalf("exclude md wrong: %+v excluded=%d", res.Hits, res.Excluded)
	}
}

func TestSearchNeedsQueryAndStack(t *testing.T) {
	if _, err := Search(t.TempDir(), Config{}, SearchOpts{}); err == nil {
		t.Fatal("empty query must fail")
	}
	root := t.TempDir()
	_ = (Config{Backend: BackendNone, Store: ".claude/blm", SocratiCode: &SocratiCodeConfig{OllamaURL: "http://127.0.0.1:1", QdrantURL: "http://127.0.0.1:1"}}).Save(root)
	if _, err := Search(root, Load(root), SearchOpts{Query: "anything"}); err == nil || !strings.Contains(err.Error(), "blm tools status") {
		t.Fatalf("offline stack must say so, got %v", err)
	}
}

func TestHookAndStatusline(t *testing.T) {
	srv, _ := fakeIndex(t)
	root := remoteProject(t, srv)
	t.Setenv("TMPDIR", t.TempDir()) // แคช stats แยกจากเครื่องจริง

	var out strings.Builder
	_ = Hook("prompt", HookInput{Prompt: "how does cookie consent work"}, root, &out)
	if !strings.Contains(out.String(), `"hookEventName":"UserPromptSubmit"`) || !strings.Contains(out.String(), "src/a.ts:L1-L9 (1.00)") || strings.Contains(out.String(), "blm.md") {
		t.Fatalf("prompt hook: %s", out.String())
	}
	out.Reset()
	_ = Hook("prompt", HookInput{Prompt: "short"}, root, &out)
	if out.Len() != 0 {
		t.Fatalf("short prompt must be silent: %s", out.String())
	}
	out.Reset()
	_ = Hook("session-start", HookInput{}, root, &out)
	if !strings.Contains(out.String(), "3 files · 10 symbols / 20 call edges") {
		t.Fatalf("session-start: %s", out.String())
	}
	out.Reset()
	gin := HookInput{ToolName: "Grep"}
	gin.ToolInput.Pattern = "sealToken"
	_ = Hook("grep-nudge", gin, root, &out)
	if !strings.Contains(out.String(), "prefer blm_search over Grep") {
		t.Fatalf("grep-nudge: %s", out.String())
	}

	pct := 21.6
	in := HookInput{SessionID: "878f733d-1234"}
	in.ContextWindow.UsedPercentage = &pct
	line := stripANSI(Statusline(in, root, false))
	if !strings.Contains(line, "socraticode : online · 10 nodes / 20 edges") || !strings.Contains(line, "▸ ctx 22% · session: 878f733d") {
		t.Fatalf("statusline: %q", line)
	}
	if top := stripANSI(Statusline(in, root, true)); strings.Contains(top, "ctx") {
		t.Fatalf("top-only must drop the second line: %q", top)
	}
	// ไฟล์ที่ index ไม่เห็น (.claude/*) ไม่ทำให้ pending — ต้องมี git repo + .socraticodeignore
	_ = os.MkdirAll(filepath.Join(root, ".claude"), 0o755)
	_ = os.WriteFile(filepath.Join(root, ".socraticodeignore"), []byte(".claude/*\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, ".claude", "x.json"), []byte("{}"), 0o644)
	if newestDirtyMtime(root) != 0 { // ไม่ใช่ git repo → 0 (ไม่รู้ = synced)
		t.Fatal("non-git root must report 0")
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
