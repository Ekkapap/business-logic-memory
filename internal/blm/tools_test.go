package blm

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeStack — Ollama (/api/tags) + Qdrant (/collections) บน server เดียว พอสำหรับ toolsSocratiCodeRemote
func fakeStack(t *testing.T, models ...string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, _ *http.Request) {
		var parts []string
		for _, m := range models {
			parts = append(parts, `{"name":"`+m+`"}`)
		}
		_, _ = w.Write([]byte(`{"models":[` + strings.Join(parts, ",") + `]}`))
	})
	mux.HandleFunc("/collections", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"result":{"collections":[]}}`)) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestSocratiCodeRemoteInstall(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home) // applyEnv เขียน ~/.claude/settings.json — ต้องไม่แตะของจริง
	srv := fakeStack(t, "bge-m3:latest")
	c := Config{Backend: BackendNone, Store: ".claude/blm"}

	opts := ToolOpts{Remote: true, SC: SocratiCodeConfig{OllamaURL: srv.URL, QdrantURL: srv.URL, EmbeddingModel: "bge-m3", EmbeddingDimensions: "1024", EmbeddingContextLength: "8192"}}
	out, err := Tools(root, c, "install", "socraticode", opts, nil)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if !strings.Contains(out, "bge-m3 ✔") || !strings.Contains(out, "socraticode section saved") {
		t.Fatalf("unexpected output:\n%s", out)
	}
	// config จำค่าไว้ + prefix ว่างเพราะโมเดลไม่ใช่ nomic
	saved := Load(root)
	if saved.SocratiCode == nil || saved.SocratiCode.QdrantURL != srv.URL || saved.SocratiCode.EmbeddingQueryPrefix != "" || !containsStr(saved.Tools, "socraticode") {
		t.Fatalf("config not saved as expected: %+v", saved)
	}
	// env: ทั้ง 9 ตัว รวม prefix ว่างที่ต้องเขียนจริง ๆ ไม่ใช่หาย
	raw, _ := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	for _, want := range []string{`"OLLAMA_MODE": "external"`, `"QDRANT_URL": "` + srv.URL + `"`, `"EMBEDDING_MODEL": "bge-m3"`, `"EMBEDDING_DIMENSIONS": "1024"`, `"EMBEDDING_CONTEXT_LENGTH": "8192"`, `"EMBEDDING_QUERY_PREFIX": ""`, `"EMBEDDING_DOCUMENT_PREFIX": ""`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("settings.json missing %s:\n%s", want, raw)
		}
	}

	// รอบสอง: --remote เฉย ๆ ใช้ค่าที่จำไว้ · ไม่ระบุ flag เลย = หยุดพร้อมคำแนะนำ ไม่ทำอะไร
	if _, err := Tools(root, Load(root), "install", "socraticode", ToolOpts{Remote: true}, nil); err != nil {
		t.Fatalf("reuse saved config: %v", err)
	}
	if _, err := Tools(root, Load(root), "install", "socraticode", ToolOpts{}, nil); err == nil || !strings.Contains(err.Error(), "--remote") || !strings.Contains(err.Error(), "--local") || !strings.Contains(err.Error(), "Nothing changed") {
		t.Fatalf("no-flag install with a remote section must stop and advise, got %v", err)
	}
	// get/start บอกว่าไม่มีอะไรทำบนเครื่องนี้
	if out, _ := Tools(root, Load(root), "start", "socraticode", ToolOpts{}, nil); !strings.Contains(out, "nothing to do") {
		t.Fatalf("start remote: %s", out)
	}
}

func TestSocratiCodeRemoteRefusesBadHost(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	srv := fakeStack(t, "nomic-embed-text:latest")
	// โมเดลไม่มีบน host → ไม่บันทึกอะไร
	_, err := Tools(root, Config{Backend: BackendNone, Store: ".claude/blm"}, "install", "socraticode",
		ToolOpts{Remote: true, SC: SocratiCodeConfig{OllamaURL: srv.URL, QdrantURL: srv.URL, EmbeddingModel: "bge-m3", EmbeddingDimensions: "1024", EmbeddingContextLength: "8192"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected missing-model error, got %v", err)
	}
	if Load(root).SocratiCode != nil {
		t.Fatal("config must stay untouched after a failed check")
	}
	// ไม่มี host เลย
	if _, err := Tools(root, Config{Backend: BackendNone, Store: ".claude/blm"}, "install", "socraticode", ToolOpts{Remote: true}, nil); err == nil || !strings.Contains(err.Error(), "needs a host") {
		t.Fatalf("expected needs-a-host error, got %v", err)
	}
}

func TestSocratiCodeEnvLines(t *testing.T) {
	// nomic ที่ prefix ตั้งเอง — ค่ามีช่องว่าง ต้องรอด applyEnv ทั้งบรรทัด
	sc := SocratiCodeConfig{OllamaURL: "http://h:11434", QdrantURL: "http://h:6333", EmbeddingModel: "nomic-embed-text", EmbeddingDimensions: "768", EmbeddingContextLength: "2048", EmbeddingQueryPrefix: "search_query: ", EmbeddingDocumentPrefix: "search_document: "}
	home := t.TempDir()
	t.Setenv("HOME", home)
	if _, err := applyEnv(sc.Env()); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if !strings.Contains(string(raw), `"EMBEDDING_QUERY_PREFIX": "search_query: "`) {
		t.Fatalf("prefix with a space was split:\n%s", raw)
	}
	// ไม่มีโมเดล = ลบ EMBEDDING_* (กลับไป local/nomic default)
	if _, err := applyEnv(SocratiCodeConfig{OllamaURL: "http://127.0.0.1:11434", QdrantURL: "http://127.0.0.1:6333"}.Env()); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if strings.Contains(string(raw), "EMBEDDING_") {
		t.Fatalf("EMBEDDING_* should be removed:\n%s", raw)
	}
}

func TestWireClaudeIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// ไม่มีอะไรเลย → สร้างสคริปต์ค่าเริ่มต้น + hooks + statusLine
	out := strings.Join(WireClaude(), "\n")
	if !strings.Contains(out, "wrote default") || !strings.Contains(out, "statusLine → ~/.claude/statusline-command.sh") {
		t.Fatalf("first wire: %s", out)
	}
	script, _ := os.ReadFile(filepath.Join(home, ".claude", "statusline-command.sh"))
	if strings.Count(string(script), slBlockBegin) != 1 || !strings.Contains(string(script), "blm statusline --top-only") || !strings.HasPrefix(string(script), "#!/bin/bash") {
		t.Fatalf("default script wrong:\n%s", script)
	}
	// รอบสอง + hook เก่าของเรา (socraticode-hooks.cjs) หาย · ของคนอื่น (rtk, graft) อยู่ครบ + เตือนเรื่อง graft · ของเราไม่ซ้ำ
	settingsFile := filepath.Join(home, ".claude", "settings.json")
	_ = os.WriteFile(settingsFile, []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"rtk hook claude"}]}],
	  "SessionStart":[{"hooks":[{"type":"command","command":"node \"$HOME/.claude/helpers/socraticode-hooks.cjs\" session-start"}]}],
	  "PostToolUse":[{"matcher":"Write|Edit","hooks":[{"type":"command","command":"node graft-hooks.cjs post-edit"}]},{"matcher":"Grep|Glob","hooks":[{"type":"command","command":"blm hook grep-nudge"}]}]},
	  "statusLine":{"type":"command","command":"~/.claude/statusline-command.sh"}}`), 0o644)
	// สคริปต์ที่เจ้าของเคยวาง block มือ (ไม่มี marker) → ต้องถูกแทนด้วย block ใหม่ block เดียว
	_ = os.WriteFile(filepath.Join(home, ".claude", "statusline-command.sh"), []byte("#!/bin/bash\ninput=$(cat)\nsc=$(echo \"$input\" | node socraticode-statusline.cjs)\nprintf x\n[ -n \"$sc\" ] && printf '\\n%s' \"$sc\"\n"), 0o755)
	out = strings.Join(WireClaude(), "\n")
	raw, _ := os.ReadFile(settingsFile)
	s := string(raw)
	if strings.Contains(s, "socraticode-hooks.cjs") {
		t.Fatalf("our old .cjs hook still present:\n%s", s)
	}
	if !strings.Contains(s, "graft-hooks.cjs") || !strings.Contains(out, "graft uninstall -y") {
		t.Fatalf("graft hook must stay, with advice:\n%s\n%s", s, out)
	}
	if !strings.Contains(s, "rtk hook claude") || strings.Count(s, "blm hook grep-nudge") != 1 || strings.Count(s, "blm hook prompt") != 1 || strings.Count(s, "blm hook session-start") != 1 {
		t.Fatalf("hooks wrong:\n%s", s)
	}
	script, _ = os.ReadFile(filepath.Join(home, ".claude", "statusline-command.sh"))
	if strings.Count(string(script), slBlockBegin) != 1 || strings.Contains(string(script), "socraticode-statusline.cjs") || !strings.Contains(string(script), "printf x") {
		t.Fatalf("script not rewritten cleanly:\n%s", script)
	}
	WireClaude()
	script2, _ := os.ReadFile(filepath.Join(home, ".claude", "statusline-command.sh"))
	if string(script2) != string(script) {
		t.Fatal("third wire changed the script (not idempotent)")
	}
}
