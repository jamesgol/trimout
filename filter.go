package main

import (
	"fmt"
	"regexp"
	"strings"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b\[\?[0-9;]*[hl]`)

// FilterDedup removes consecutive duplicate lines (like uniq).
func FilterDedup(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	out := []string{lines[0]}
	for i := 1; i < len(lines); i++ {
		if lines[i] != lines[i-1] {
			out = append(out, lines[i])
		}
	}
	return out
}

// FilterDedupAll removes all duplicate lines, keeping first occurrence.
func FilterDedupAll(lines []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, l := range lines {
		if _, ok := seen[l]; !ok {
			seen[l] = struct{}{}
			out = append(out, l)
		}
	}
	return out
}

// FilterHead keeps first n lines.
func FilterHead(lines []string, n int) []string {
	if n >= len(lines) {
		return lines
	}
	return lines[:n]
}

// FilterTail keeps last n lines.
func FilterTail(lines []string, n int) []string {
	if n >= len(lines) {
		return lines
	}
	return lines[len(lines)-n:]
}

// FilterEnds keeps the first n and last n lines with an omission marker.
func FilterEnds(lines []string, n int) []string {
	if 2*n >= len(lines) {
		return lines
	}
	omitted := len(lines) - 2*n
	out := make([]string, 0, 2*n+1)
	out = append(out, lines[:n]...)
	out = append(out, fmt.Sprintf("[... %d lines omitted]", omitted))
	out = append(out, lines[len(lines)-n:]...)
	return out
}

// FilterMid keeps the middle of the output by dropping the first n and last n lines.
func FilterMid(lines []string, n int) []string {
	if 2*n >= len(lines) {
		return nil
	}
	return lines[n : len(lines)-n]
}

// FilterGrep keeps only lines matching pattern.
func FilterGrep(lines []string, pattern string) ([]string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid grep pattern: %w", err)
	}
	var out []string
	for _, l := range lines {
		if re.MatchString(l) {
			out = append(out, l)
		}
	}
	return out, nil
}

// FilterGrepV removes lines matching pattern.
func FilterGrepV(lines []string, pattern string) ([]string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid grep-v pattern: %w", err)
	}
	var out []string
	for _, l := range lines {
		if !re.MatchString(l) {
			out = append(out, l)
		}
	}
	return out, nil
}

// FilterStripAnsi removes ANSI escape codes.
func FilterStripAnsi(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ansiRe.ReplaceAllString(l, "")
	}
	return out
}

// FilterCompressBlank collapses multiple consecutive blank lines into one.
func FilterCompressBlank(lines []string) []string {
	var out []string
	prevBlank := false
	for _, l := range lines {
		blank := strings.TrimSpace(l) == ""
		if blank && prevBlank {
			continue
		}
		out = append(out, l)
		prevBlank = blank
	}
	return out
}

// FilterMaxLineLen truncates lines longer than n characters.
func FilterMaxLineLen(lines []string, n int) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		if len(l) > n {
			out[i] = l[:n] + "..."
		} else {
			out[i] = l
		}
	}
	return out
}

// StatsLine returns a summary line describing the filtering.
func StatsLine(originalLines int, originalBytes int, filteredLines int, filteredBytes int) string {
	if originalLines == filteredLines {
		return fmt.Sprintf("# %d lines, %d bytes", originalLines, originalBytes)
	}
	return fmt.Sprintf("# %d lines, %d bytes (filtered from %d lines, %d bytes)",
		filteredLines, filteredBytes, originalLines, originalBytes)
}
