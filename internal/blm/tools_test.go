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
	if !strings.Contains(out, "model bge-m3 present") || !strings.Contains(out, "config: socraticode section saved") {
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

	// รอบสอง: --remote เฉย ๆ ใช้ค่าที่จำไว้ · ไม่ระบุ flag เลยก็ไป remote เพราะ config มี
	for _, o := range []ToolOpts{{Remote: true}, {}} {
		if _, err := Tools(root, Load(root), "install", "socraticode", o, nil); err != nil {
			t.Fatalf("reuse saved config %+v: %v", o, err)
		}
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
	if err == nil || !strings.Contains(err.Error(), "NOT found") {
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
