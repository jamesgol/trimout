package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// PatternEntry represents one pattern from a JSONL pattern file.
type PatternEntry struct {
	Type       string `json:"type"`
	Pattern    string `json:"pattern"`
	ID         string `json:"id"`
	Confidence string `json:"confidence"`
	Meta       string `json:"meta"`
}

// CompiledPattern is a parsed and ready-to-match pattern.
type CompiledPattern struct {
	Entry PatternEntry
	Re    *regexp.Regexp // non-nil for regex type
}

// PatternMatch records a single match of a pattern against an input line.
type PatternMatch struct {
	Line       int    `json:"line"`
	Content    string `json:"content"`
	PatternID  string `json:"pattern_id"`
	Confidence string `json:"confidence"`
	Meta       string `json:"meta"`
}

// LoadPatterns reads a JSONL pattern file and compiles all entries.
func LoadPatterns(path string) ([]CompiledPattern, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open pattern file: %w", err)
	}
	defer f.Close()

	var patterns []CompiledPattern
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		var entry PatternEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, fmt.Errorf("pattern file line %d: %w", lineNum, err)
		}

		cp := CompiledPattern{Entry: entry}
		if entry.Type == "regex" {
			re, err := regexp.Compile(entry.Pattern)
			if err != nil {
				return nil, fmt.Errorf("pattern file line %d: invalid regex %q: %w", lineNum, entry.Pattern, err)
			}
			cp.Re = re
		}
		patterns = append(patterns, cp)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading pattern file: %w", err)
	}
	return patterns, nil
}

// MatchLine checks a single line against all patterns, literals first then regexes.
// Returns all matches found.
func MatchLine(line string, patterns []CompiledPattern) []CompiledPattern {
	var matches []CompiledPattern
	// Literals first (fast)
	for i := range patterns {
		if patterns[i].Entry.Type == "literal" {
			if strings.Contains(line, patterns[i].Entry.Pattern) {
				matches = append(matches, patterns[i])
			}
		}
	}
	// Then regexes
	for i := range patterns {
		if patterns[i].Entry.Type == "regex" {
			if patterns[i].Re.MatchString(line) {
				matches = append(matches, patterns[i])
			}
		}
	}
	return matches
}

// FilterPatterns keeps only lines that match at least one pattern.
func FilterPatterns(lines []string, patterns []CompiledPattern) []string {
	var out []string
	for _, line := range lines {
		if len(MatchLine(line, patterns)) > 0 {
			out = append(out, line)
		}
	}
	return out
}

// MatchAll runs all patterns against all lines and returns structured matches.
func MatchAll(lines []string, patterns []CompiledPattern) []PatternMatch {
	var matches []PatternMatch
	for i, line := range lines {
		for _, cp := range MatchLine(line, patterns) {
			matches = append(matches, PatternMatch{
				Line:       i + 1,
				Content:    line,
				PatternID:  cp.Entry.ID,
				Confidence: cp.Entry.Confidence,
				Meta:       cp.Entry.Meta,
			})
		}
	}
	return matches
}
