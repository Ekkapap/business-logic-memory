package blm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// CatResult represents the result of a cat operation
type CatResult struct {
	GrepID  string               `json:"grepId"`
	Results map[int]*HitContent `json:"results"`
}

// HitContent represents the file content for a hit with context
type HitContent struct {
	ResultID int      `json:"resultId"`
	Path     string   `json:"path"`
	Line     int      `json:"line"`
	Context  int      `json:"context"`
	Lines    []string `json:"lines"`    // actual file lines
	Hit      int      `json:"hitLine"` // 1-indexed position in Lines
}

// Cat reads a saved grep result and displays file content for selected hits
func Cat(root, grepID string, resultIDs []int, context int) (*CatResult, error) {
	// Find the result file
	resultFile, err := findGrepResultFile(root, grepID)
	if err != nil {
		return nil, err
	}

	// Read and parse result file
	data, err := os.ReadFile(resultFile)
	if err != nil {
		return nil, fmt.Errorf("cannot read result file: %v", err)
	}

	var grepResult GrepResult
	if err := json.Unmarshal(data, &grepResult); err != nil {
		return nil, fmt.Errorf("cannot parse result file: %v", err)
	}

	// If no specific resultIds given, use all
	if len(resultIDs) == 0 {
		for _, h := range grepResult.Hits {
			resultIDs = append(resultIDs, h.ResultID)
		}
	}

	// Build a map of resultId -> hit for quick lookup
	hitMap := make(map[int]*GrepHit)
	for i := range grepResult.Hits {
		hitMap[grepResult.Hits[i].ResultID] = &grepResult.Hits[i]
	}

	// Group hits by file for efficient reading
	hitsByFile := make(map[string][]int)
	for _, rid := range resultIDs {
		if h, ok := hitMap[rid]; ok {
			hitsByFile[h.Path] = append(hitsByFile[h.Path], rid)
		}
	}

	cat := &CatResult{
		GrepID:  grepID,
		Results: make(map[int]*HitContent),
	}

	// Read each file and extract context
	for filePath, rids := range hitsByFile {
		// Resolve relative path using the stored cwd
		absPath := filePath
		if !filepath.IsAbs(filePath) && grepResult.CWD != "" {
			absPath = filepath.Join(grepResult.CWD, filePath)
		}

		content, err := os.ReadFile(absPath)
		if err != nil {
			continue // skip if file not found
		}

		lines := strings.Split(string(content), "\n")

		for _, rid := range rids {
			hit := hitMap[rid]
			start := hit.Line - 1 - context
			if start < 0 {
				start = 0
			}
			end := hit.Line - 1 + context
			if end >= len(lines) {
				end = len(lines) - 1
			}

			contextLines := lines[start : end+1]
			hitLine := hit.Line - 1 - start + 1 // 1-indexed in the context window

			cat.Results[rid] = &HitContent{
				ResultID: rid,
				Path:     filePath,
				Line:     hit.Line,
				Context:  context,
				Lines:    contextLines,
				Hit:      hitLine,
			}
		}
	}

	return cat, nil
}

// findGrepResultFile searches for a grep result file by ID
func findGrepResultFile(root, grepID string) (string, error) {
	// Try blm project store first
	projectStore := filepath.Join(root, ".agentsroom", "blm", "tmp")
	filename := fmt.Sprintf("grep-result-%s.json", grepID)
	filePath := filepath.Join(projectStore, filename)
	if _, err := os.Stat(filePath); err == nil {
		return filePath, nil
	}

	// Try cache dir (check BLM_CACHE_DIR env for tests)
	cacheDir := os.Getenv("BLM_CACHE_DIR")
	if cacheDir == "" {
		var err error
		cacheDir, err = os.UserCacheDir()
		if err == nil {
			cachePath := filepath.Join(cacheDir, "blm", "grep", filename)
			if _, err := os.Stat(cachePath); err == nil {
				return cachePath, nil
			}
		}
	} else {
		cachePath := filepath.Join(cacheDir, "blm", "grep", filename)
		if _, err := os.Stat(cachePath); err == nil {
			return cachePath, nil
		}
	}

	return "", fmt.Errorf("grep result not found: %s", grepID)
}

// ParseResultIDs parses a comma-separated list of result IDs
func ParseResultIDs(s string) ([]int, error) {
	if s == "" {
		return nil, nil
	}

	var ids []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid result ID: %s", part)
		}
		ids = append(ids, id)
	}

	sort.Ints(ids)
	return ids, nil
}
