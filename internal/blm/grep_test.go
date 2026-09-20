package blm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrepWindowBoundaries(t *testing.T) {
	// Create temp dir with test files
	tmpDir, _ := os.MkdirTemp("", "blm-grep-test-")
	defer os.RemoveAll(tmpDir)

	// Set cache dir to temp for test isolation
	cacheDir := filepath.Join(tmpDir, "cache")
	os.MkdirAll(cacheDir, 0755)
	t.Setenv("BLM_CACHE_DIR", cacheDir)

	// Write test file with empty line boundaries
	testFile := filepath.Join(tmpDir, "test.md")
	testContent := `# Header 1

Line 1
Line 2
Line 3: SEARCH_ME
Line 5
Line 6

# Header 2

Next section
More text
Yet more
`
	os.WriteFile(testFile, []byte(testContent), 0644)

	// Test grep with window clamping
	res, err := Grep(tmpDir, []string{"SEARCH_ME"}, testFile, "", 4, 10, ".md", false, false)
	if err != nil {
		t.Fatalf("grep failed: %v", err)
	}

	if len(res.Hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(res.Hits))
	}

	hit := res.Hits[0]
	if hit.Term != "SEARCH_ME" {
		t.Errorf("term: got %q, want SEARCH_ME", hit.Term)
	}
	if hit.Line != 5 {
		t.Errorf("line: got %d, want 5", hit.Line)
	}
	// Snippet should be bounded by empty lines or maxLine clamp
	if hit.Lines > 4 {
		t.Errorf("lines: got %d, want ≤4", hit.Lines)
	}
}

func TestGrepAllowlist(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "blm-grep-ext-")
	defer os.RemoveAll(tmpDir)

	// Set cache dir to temp for test isolation
	cacheDir := filepath.Join(tmpDir, "cache")
	os.MkdirAll(cacheDir, 0755)
	t.Setenv("BLM_CACHE_DIR", cacheDir)

	// Create files with different extensions
	os.WriteFile(filepath.Join(tmpDir, "keep.md"), []byte("TERM"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "keep.ts"), []byte("TERM"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "skip.bin"), []byte("TERM"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "skip.pdf"), []byte("TERM"), 0644)

	// Add .go and .sh to allowlist
	res, err := Grep(tmpDir, []string{"TERM"}, "", tmpDir, 10, 10, ".md,.ts,.go,.sh", false, false)
	if err != nil {
		t.Fatalf("grep failed: %v", err)
	}

	// Should have hits only from .md and .ts
	for _, h := range res.Hits {
		ext := filepath.Ext(h.Path)
		if ext != ".md" && ext != ".ts" {
			t.Errorf("unexpected extension %s in results", ext)
		}
	}
}

func TestGrepSkipDirs(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "blm-grep-skip-")
	defer os.RemoveAll(tmpDir)

	// Set cache dir to temp for test isolation
	cacheDir := filepath.Join(tmpDir, "cache")
	os.MkdirAll(cacheDir, 0755)
	t.Setenv("BLM_CACHE_DIR", cacheDir)

	// Create files in skip dirs
	os.MkdirAll(filepath.Join(tmpDir, "node_modules"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "src"), 0755)

	os.WriteFile(filepath.Join(tmpDir, "node_modules", "lib.ts"), []byte("TERM"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "src", "main.ts"), []byte("TERM"), 0644)

	res, err := Grep(tmpDir, []string{"TERM"}, "", tmpDir, 10, 10, ".ts", false, false)
	if err != nil {
		t.Fatalf("grep failed: %v", err)
	}

	// Should only find src/main.ts, not node_modules/lib.ts
	for _, h := range res.Hits {
		if strings.Contains(h.Path, "node_modules") {
			t.Errorf("should skip node_modules, but found %s", h.Path)
		}
	}
}

func TestGrepMaxResult(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "blm-grep-maxresult-")
	defer os.RemoveAll(tmpDir)

	// Set cache dir to temp for test isolation
	cacheDir := filepath.Join(tmpDir, "cache")
	os.MkdirAll(cacheDir, 0755)
	t.Setenv("BLM_CACHE_DIR", cacheDir)

	// Create file with multiple matches
	content := "TERM\nTERM\nTERM\nTERM\nTERM\n"
	os.WriteFile(filepath.Join(tmpDir, "many.txt"), []byte(content), 0644)

	res, err := Grep(tmpDir, []string{"TERM"}, "", tmpDir, 10, 2, ".txt", false, false)
	if err != nil {
		t.Fatalf("grep failed: %v", err)
	}

	if len(res.Hits) > 2 {
		t.Errorf("maxResult=2 but got %d hits", len(res.Hits))
	}
}

func TestGrepSkipImports(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "blm-grep-imports-")
	defer os.RemoveAll(tmpDir)

	// Set cache dir to temp for test isolation
	cacheDir := filepath.Join(tmpDir, "cache")
	os.MkdirAll(cacheDir, 0755)
	t.Setenv("BLM_CACHE_DIR", cacheDir)

	// Create file with imports and a search term in imports
	content := `import { getUser } from "@/lib/user"
import { getSubjectRecipient } from "@/lib/notify"
const x = require("helper")

function getSubjectRecipient(id) {
  return "recipient"
}`
	os.WriteFile(filepath.Join(tmpDir, "test.ts"), []byte(content), 0644)

	// Without --imports flag, should not match the import line
	res, err := Grep(tmpDir, []string{"getSubjectRecipient"}, "", tmpDir, 10, 10, ".ts", false, false)
	if err != nil {
		t.Fatalf("grep failed: %v", err)
	}
	if len(res.Hits) != 1 {
		t.Errorf("expected 1 hit (function def), got %d", len(res.Hits))
	}
	if res.Hits[0].Line != 5 {
		t.Errorf("expected line 5 (function def), got %d", res.Hits[0].Line)
	}

	// With --imports flag, should match both
	res, err = Grep(tmpDir, []string{"getSubjectRecipient"}, "", tmpDir, 10, 10, ".ts", false, true)
	if err != nil {
		t.Fatalf("grep failed: %v", err)
	}
	if len(res.Hits) != 2 {
		t.Errorf("expected 2 hits (import + function def), got %d", len(res.Hits))
	}
}
