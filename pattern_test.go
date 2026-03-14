package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func writePatternFile(t *testing.T, lines ...string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "patterns.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range lines {
		f.WriteString(l + "\n")
	}
	f.Close()
	return path
}

func TestLoadPatternsJSONL(t *testing.T) {
	path := writePatternFile(t,
		`# this is a comment`,
		``,
		`{"type":"literal","pattern":"PHPSESSID","id":"php","confidence":"confirmed","meta":"cookie"}`,
		`{"type":"regex","pattern":"Server: Apache/[0-9]","id":"apache","confidence":"confirmed","meta":"header"}`,
	)

	patterns, err := LoadPatternsJSONL(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(patterns) != 2 {
		t.Fatalf("expected 2 patterns, got %d", len(patterns))
	}

	if patterns[0].Entry.ID != "php" {
		t.Errorf("expected id 'php', got %q", patterns[0].Entry.ID)
	}
	if patterns[0].Re != nil {
		t.Error("literal pattern should not have compiled regex")
	}

	if patterns[1].Entry.ID != "apache" {
		t.Errorf("expected id 'apache', got %q", patterns[1].Entry.ID)
	}
	if patterns[1].Re == nil {
		t.Error("regex pattern should have compiled regex")
	}
}

func TestLoadPatternsJSONLInvalidRegex(t *testing.T) {
	path := writePatternFile(t,
		`{"type":"regex","pattern":"[invalid","id":"bad","confidence":"weak","meta":""}`,
	)
	_, err := LoadPatternsJSONL(path)
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestLoadPatternsJSONLInvalidJSON(t *testing.T) {
	path := writePatternFile(t, `not json`)
	_, err := LoadPatternsJSONL(path)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLoadPatternsText(t *testing.T) {
	path := writePatternFile(t,
		`# comment line`,
		``,
		`PHPSESSID`,
		`wp-content`,
	)

	patterns, err := LoadPatternsText(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(patterns) != 2 {
		t.Fatalf("expected 2 patterns, got %d", len(patterns))
	}

	if patterns[0].Entry.Pattern != "PHPSESSID" {
		t.Errorf("expected pattern 'PHPSESSID', got %q", patterns[0].Entry.Pattern)
	}
	if patterns[0].Entry.ID != "PHPSESSID" {
		t.Errorf("expected id to match pattern, got %q", patterns[0].Entry.ID)
	}
	if patterns[0].Entry.Type != "literal" {
		t.Errorf("expected type 'literal', got %q", patterns[0].Entry.Type)
	}
	if patterns[0].Entry.Confidence != "confirmed" {
		t.Errorf("expected confidence 'confirmed', got %q", patterns[0].Entry.Confidence)
	}
	if patterns[1].Entry.Pattern != "wp-content" {
		t.Errorf("expected pattern 'wp-content', got %q", patterns[1].Entry.Pattern)
	}
}

func TestMatchLine(t *testing.T) {
	patterns := []CompiledPattern{
		{Entry: PatternEntry{Type: "literal", Pattern: "PHPSESSID", ID: "php"}},
		{Entry: PatternEntry{Type: "literal", Pattern: "wp-content", ID: "wordpress"}},
	}
	// Add a regex pattern
	re := mustCompilePattern(t, `Server: Apache/[0-9]`)
	patterns = append(patterns, CompiledPattern{
		Entry: PatternEntry{Type: "regex", Pattern: `Server: Apache/[0-9]`, ID: "apache"},
		Re:    re,
	})

	tests := []struct {
		name    string
		line    string
		wantIDs []string
	}{
		{"no match", "nothing here", nil},
		{"literal match", "Set-Cookie: PHPSESSID=abc123", []string{"php"}},
		{"regex match", "Server: Apache/2.4.41", []string{"apache"}},
		{"multiple matches", "PHPSESSID in wp-content/themes", []string{"php", "wordpress"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := MatchLine(tt.line, patterns)
			var gotIDs []string
			for _, m := range matches {
				gotIDs = append(gotIDs, m.Entry.ID)
			}
			if len(gotIDs) != len(tt.wantIDs) {
				t.Fatalf("want %v, got %v", tt.wantIDs, gotIDs)
			}
			for i := range tt.wantIDs {
				if gotIDs[i] != tt.wantIDs[i] {
					t.Errorf("match %d: want %q, got %q", i, tt.wantIDs[i], gotIDs[i])
				}
			}
		})
	}
}

func TestFilterPatterns(t *testing.T) {
	patterns := []CompiledPattern{
		{Entry: PatternEntry{Type: "literal", Pattern: "error", ID: "err"}},
	}

	lines := []string{"all good", "error: something broke", "fine", "another error here"}
	got := FilterPatterns(lines, patterns)
	assertLines(t, []string{"error: something broke", "another error here"}, got)
}

func TestMatchAll(t *testing.T) {
	patterns := []CompiledPattern{
		{Entry: PatternEntry{Type: "literal", Pattern: "SESS", ID: "session", Confidence: "confirmed", Meta: "cookie"}},
	}

	lines := []string{"no match", "SESS=abc", "also no", "SESS=xyz"}
	matches := MatchAll(lines, patterns)
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}

	if matches[0].Line != 2 {
		t.Errorf("first match line: want 2, got %d", matches[0].Line)
	}
	if matches[0].Content != "SESS=abc" {
		t.Errorf("first match content: want %q, got %q", "SESS=abc", matches[0].Content)
	}
	if matches[0].PatternID != "session" {
		t.Errorf("first match pattern_id: want %q, got %q", "session", matches[0].PatternID)
	}

	if matches[1].Line != 4 {
		t.Errorf("second match line: want 4, got %d", matches[1].Line)
	}

	// Verify JSON serialization
	b, err := json.Marshal(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var decoded PatternMatch
	json.Unmarshal(b, &decoded)
	if decoded.PatternID != "session" {
		t.Errorf("JSON round-trip: want pattern_id %q, got %q", "session", decoded.PatternID)
	}
}

func mustCompilePattern(t *testing.T, pattern string) *regexp.Regexp {
	t.Helper()
	re, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatalf("failed to compile %q: %v", pattern, err)
	}
	return re
}
