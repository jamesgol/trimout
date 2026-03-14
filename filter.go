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

// LineFilter processes a single line for streaming mode.
// Returns the (possibly modified) line, whether to emit it, and whether to stop.
type LineFilter interface {
	Filter(line string) (result string, emit bool, done bool)
}

type stripAnsiStreamFilter struct{}

func (f *stripAnsiStreamFilter) Filter(line string) (string, bool, bool) {
	return ansiRe.ReplaceAllString(line, ""), true, false
}

type dedupStreamFilter struct {
	prev    string
	hasPrev bool
}

func (f *dedupStreamFilter) Filter(line string) (string, bool, bool) {
	if f.hasPrev && line == f.prev {
		return "", false, false
	}
	f.prev = line
	f.hasPrev = true
	return line, true, false
}

type grepStreamFilter struct {
	re *regexp.Regexp
}

func (f *grepStreamFilter) Filter(line string) (string, bool, bool) {
	return line, f.re.MatchString(line), false
}

type grepVStreamFilter struct {
	re *regexp.Regexp
}

func (f *grepVStreamFilter) Filter(line string) (string, bool, bool) {
	return line, !f.re.MatchString(line), false
}

type patternStreamFilter struct {
	patterns []CompiledPattern
}

func (f *patternStreamFilter) Filter(line string) (string, bool, bool) {
	return line, len(MatchLine(line, f.patterns)) > 0, false
}

type compressBlankStreamFilter struct {
	prevBlank bool
}

func (f *compressBlankStreamFilter) Filter(line string) (string, bool, bool) {
	blank := strings.TrimSpace(line) == ""
	if blank && f.prevBlank {
		return "", false, false
	}
	f.prevBlank = blank
	return line, true, false
}

type maxLineLenStreamFilter struct {
	n int
}

func (f *maxLineLenStreamFilter) Filter(line string) (string, bool, bool) {
	if len(line) > f.n {
		return line[:f.n] + "...", true, false
	}
	return line, true, false
}

type headStreamFilter struct {
	limit int
	count int
}

func (f *headStreamFilter) Filter(line string) (string, bool, bool) {
	f.count++
	if f.count <= f.limit {
		return line, true, f.count == f.limit
	}
	return "", false, true
}

// buildStreamFilters constructs the streaming filter pipeline.
// Order matches applyFilters: strip-ansi, dedup, grep, grep-v, patterns,
// compress-blank, max-line-len, head.
func buildStreamFilters(opts *filterOpts, compiled []CompiledPattern) ([]LineFilter, error) {
	var filters []LineFilter
	if opts.stripAnsi {
		filters = append(filters, &stripAnsiStreamFilter{})
	}
	if opts.dedup {
		filters = append(filters, &dedupStreamFilter{})
	}
	if opts.grep != "" {
		re, err := regexp.Compile(opts.grep)
		if err != nil {
			return nil, fmt.Errorf("invalid grep pattern: %w", err)
		}
		filters = append(filters, &grepStreamFilter{re: re})
	}
	if opts.grepV != "" {
		re, err := regexp.Compile(opts.grepV)
		if err != nil {
			return nil, fmt.Errorf("invalid grep-v pattern: %w", err)
		}
		filters = append(filters, &grepVStreamFilter{re: re})
	}
	if compiled != nil {
		filters = append(filters, &patternStreamFilter{patterns: compiled})
	}
	if opts.compressBlnk {
		filters = append(filters, &compressBlankStreamFilter{})
	}
	if opts.maxLineLen > 0 {
		filters = append(filters, &maxLineLenStreamFilter{n: opts.maxLineLen})
	}
	if opts.head > 0 {
		filters = append(filters, &headStreamFilter{limit: opts.head})
	}
	return filters, nil
}

// runStreamFilters runs a line through all filters in sequence.
func runStreamFilters(filters []LineFilter, line string) (string, bool, bool) {
	emit := true
	done := false
	for _, f := range filters {
		line, emit, done = f.Filter(line)
		if !emit || done {
			return line, emit, done
		}
	}
	return line, emit, done
}

// StatsLine returns a summary line describing the filtering.
func StatsLine(originalLines int, originalBytes int, filteredLines int, filteredBytes int) string {
	if originalLines == filteredLines {
		return fmt.Sprintf("# %d lines, %d bytes", originalLines, originalBytes)
	}
	return fmt.Sprintf("# %d lines, %d bytes (filtered from %d lines, %d bytes)",
		filteredLines, filteredBytes, originalLines, originalBytes)
}
