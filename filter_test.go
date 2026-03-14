package main

import (
	"strings"
	"testing"
)

func TestFilterDedup(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{"empty", nil, nil},
		{"no dups", []string{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"consecutive dups", []string{"a", "a", "b", "b", "a"}, []string{"a", "b", "a"}},
		{"all same", []string{"x", "x", "x"}, []string{"x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterDedup(tt.input)
			assertLines(t, tt.want, got)
		})
	}
}

func TestFilterDedupAll(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{"empty", nil, nil},
		{"no dups", []string{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"non-consecutive dups", []string{"a", "b", "a", "c", "b"}, []string{"a", "b", "c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterDedupAll(tt.input)
			assertLines(t, tt.want, got)
		})
	}
}

func TestFilterHead(t *testing.T) {
	lines := []string{"a", "b", "c", "d", "e"}
	got := FilterHead(lines, 3)
	assertLines(t, []string{"a", "b", "c"}, got)

	// n >= len should return all
	got = FilterHead(lines, 10)
	assertLines(t, lines, got)
}

func TestFilterTail(t *testing.T) {
	lines := []string{"a", "b", "c", "d", "e"}
	got := FilterTail(lines, 2)
	assertLines(t, []string{"d", "e"}, got)

	got = FilterTail(lines, 10)
	assertLines(t, lines, got)
}

func TestFilterEnds(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = strings.Repeat("x", i)
	}

	got := FilterEnds(lines, 10)
	if len(got) != 21 { // 10 top + 1 marker + 10 bottom
		t.Fatalf("expected 21 lines, got %d", len(got))
	}
	if got[0] != lines[0] {
		t.Errorf("first line mismatch")
	}
	if got[9] != lines[9] {
		t.Errorf("10th line mismatch")
	}
	if !strings.Contains(got[10], "80 lines omitted") {
		t.Errorf("expected omission marker, got %q", got[10])
	}
	if got[11] != lines[90] {
		t.Errorf("first bottom line mismatch")
	}
	if got[20] != lines[99] {
		t.Errorf("last line mismatch")
	}

	// 2*n >= len should return all
	got = FilterEnds(lines, 200)
	assertLines(t, lines, got)
}

func TestFilterMid(t *testing.T) {
	lines := []string{"a", "b", "c", "d", "e", "f", "g"}

	got := FilterMid(lines, 2)
	assertLines(t, []string{"c", "d", "e"}, got)

	// Drop 1 from each end
	got = FilterMid(lines, 1)
	assertLines(t, []string{"b", "c", "d", "e", "f"}, got)

	// n too large — nothing left
	got = FilterMid(lines, 4)
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}

	// n == 0 should not be called but returns all
	got = FilterMid(lines, 0)
	assertLines(t, lines, got)
}

func TestFilterGrep(t *testing.T) {
	lines := []string{"PASS test1", "FAIL test2", "ERROR test3", "PASS test4"}

	got, err := FilterGrep(lines, "FAIL|ERROR")
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, []string{"FAIL test2", "ERROR test3"}, got)

	_, err = FilterGrep(lines, "[invalid")
	if err == nil {
		t.Error("expected error for invalid pattern")
	}
}

func TestFilterGrepV(t *testing.T) {
	lines := []string{"PASS test1", "FAIL test2", "PASS test3"}

	got, err := FilterGrepV(lines, "PASS")
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, []string{"FAIL test2"}, got)
}

func TestFilterStripAnsi(t *testing.T) {
	lines := []string{
		"\x1b[31mred\x1b[0m",
		"\x1b[1;32mbold green\x1b[0m text",
		"no escape",
	}
	got := FilterStripAnsi(lines)
	assertLines(t, []string{"red", "bold green text", "no escape"}, got)
}

func TestFilterCompressBlank(t *testing.T) {
	lines := []string{"a", "", "", "", "b", "", "c"}
	got := FilterCompressBlank(lines)
	assertLines(t, []string{"a", "", "b", "", "c"}, got)
}

func TestFilterMaxLineLen(t *testing.T) {
	lines := []string{"short", "this is a long line that exceeds the limit"}
	got := FilterMaxLineLen(lines, 10)
	if got[0] != "short" {
		t.Errorf("short line modified: %q", got[0])
	}
	if got[1] != "this is a ..." {
		t.Errorf("expected truncation, got %q", got[1])
	}
}

func TestStatsLine(t *testing.T) {
	s := StatsLine(100, 5000, 100, 5000)
	if !strings.Contains(s, "100 lines") {
		t.Errorf("unexpected stats: %q", s)
	}
	if strings.Contains(s, "filtered") {
		t.Errorf("should not say filtered when counts match: %q", s)
	}

	s = StatsLine(100, 5000, 50, 2500)
	if !strings.Contains(s, "filtered from") {
		t.Errorf("should say filtered: %q", s)
	}
}

func assertLines(t *testing.T, want, got []string) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("length mismatch: want %d, got %d\nwant: %v\ngot:  %v", len(want), len(got), want, got)
	}
	for i := range want {
		if want[i] != got[i] {
			t.Errorf("line %d: want %q, got %q", i, want[i], got[i])
		}
	}
}
