package blm

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCatBasic(t *testing.T) {
	// Create a temp directory for results
	tmpdir := t.TempDir()

	// Set cache dir to temp for test isolation
	cacheDir := filepath.Join(tmpdir, "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BLM_CACHE_DIR", cacheDir)

	projectStore := filepath.Join(tmpdir, ".agentsroom", "blm", "tmp")
	if err := os.MkdirAll(projectStore, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a test result file
	result := GrepResult{
		ID:    "test1234",
		RanAt: "2026-09-16T10:00:00Z",
		CWD:   "/test",
		Hits: []GrepHit{
			{
				ResultID: 1,
				Term:     "search",
				Path:     "test.txt",
				Line:     5,
				Start:    3,
				End:      7,
				Lines:    5,
				Chars:    50,
				Text:     "search term found here",
			},
			{
				ResultID: 2,
				Term:     "search",
				Path:     "test.txt",
				Line:     10,
				Start:    8,
				End:      12,
				Lines:    5,
				Chars:    45,
				Text:     "another search result",
			},
		},
	}

	// Save result file
	filename := filepath.Join(projectStore, "grep-result-test1234.json")
	data, _ := json.MarshalIndent(result, "", "  ")
	if err := os.WriteFile(filename, data, 0644); err != nil {
		t.Fatal(err)
	}

	// Create a test source file
	testFile := filepath.Join(tmpdir, "test.txt")
	content := bytes.Join([][]byte{
		[]byte("line 1"),
		[]byte("line 2"),
		[]byte("line 3"),
		[]byte("line 4"),
		[]byte("search term found here"),
		[]byte("line 6"),
		[]byte("line 7"),
		[]byte("line 8"),
		[]byte("line 9"),
		[]byte("another search result"),
	}, []byte("\n"))
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	// Update the hit paths to point to our test file
	result.Hits[0].Path = testFile
	result.Hits[1].Path = testFile
	data, _ = json.MarshalIndent(result, "", "  ")
	if err := os.WriteFile(filename, data, 0644); err != nil {
		t.Fatal(err)
	}

	// Test cat with specific result ID
	cat, err := Cat(tmpdir, "test1234", []int{1}, 1)
	if err != nil {
		t.Fatalf("Cat failed: %v", err)
	}

	if len(cat.Results) != 1 {
		t.Errorf("expected 1 result, got %d", len(cat.Results))
	}

	hit1 := cat.Results[1]
	if hit1 == nil {
		t.Fatalf("result 1 not found")
	}

	if hit1.Line != 5 {
		t.Errorf("expected line 5, got %d", hit1.Line)
	}

	if hit1.Hit != 2 {
		t.Errorf("expected hit at position 2 in context, got %d", hit1.Hit)
	}

	if len(hit1.Lines) != 3 {
		t.Errorf("expected 3 lines in context (1 before + hit + 1 after), got %d", len(hit1.Lines))
	}
}

func TestParseResultIDs(t *testing.T) {
	tests := []struct {
		input    string
		expected []int
		wantErr  bool
	}{
		{"1,2,3", []int{1, 2, 3}, false},
		{"5", []int{5}, false},
		{"3,1,2", []int{1, 2, 3}, false}, // Should be sorted
		{"", nil, false},                  // Empty input
		{"1, 2, 3", []int{1, 2, 3}, false}, // With spaces
		{"abc", nil, true},                // Invalid input
	}

	for _, tt := range tests {
		ids, err := ParseResultIDs(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseResultIDs(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}

		if len(ids) != len(tt.expected) {
			t.Errorf("ParseResultIDs(%q) len = %d, want %d", tt.input, len(ids), len(tt.expected))
			continue
		}

		for i, id := range ids {
			if id != tt.expected[i] {
				t.Errorf("ParseResultIDs(%q)[%d] = %d, want %d", tt.input, i, id, tt.expected[i])
			}
		}
	}
}
