package main

import (
	"os"
	"testing"
)

func setupTestCache(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	cacheDirOverride = dir
	t.Cleanup(func() { cacheDirOverride = "" })
}

func writeTestEntry(t *testing.T, stdout, stderr string, meta CacheMeta) string {
	t.Helper()
	stdoutBytes := []byte(stdout)
	stderrBytes := []byte(stderr)
	annotated := EncodeAnnotated(stdoutBytes, stderrBytes)
	id, err := CacheWrite(annotated, len(stdoutBytes), len(stderrBytes), countLines(stdoutBytes), countLines(stderrBytes), meta)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestCacheWriteAndRead(t *testing.T) {
	setupTestCache(t)

	id := writeTestEntry(t, "line1\nline2\nline3\n", "", CacheMeta{Command: "test-cmd", Tag: "mytag", ExitCode: -1})
	if id == "" {
		t.Fatal("expected non-empty ID")
	}

	// Read back by ID
	got, err := CacheReadLog(id, "")
	if err != nil {
		t.Fatal(err)
	}
	lines := DecodeAnnotated(got, streamOut)
	if len(lines) != 3 {
		t.Fatalf("expected 3 stdout lines, got %d", len(lines))
	}
	if lines[0] != "line1" || lines[2] != "line3" {
		t.Errorf("unexpected content: %v", lines)
	}

	// Read back by "last"
	got, err = CacheReadLog("last", "")
	if err != nil {
		t.Fatal(err)
	}
	lines = DecodeAnnotated(got, streamOut)
	if len(lines) != 3 {
		t.Errorf("last read: expected 3 lines, got %d", len(lines))
	}
}

func TestAnnotatedStreams(t *testing.T) {
	stdout := []byte("out1\nout2\n")
	stderr := []byte("err1\n")
	annotated := EncodeAnnotated(stdout, stderr)

	// Decode stdout only
	outLines := DecodeAnnotated(annotated, streamOut)
	if len(outLines) != 2 || outLines[0] != "out1" || outLines[1] != "out2" {
		t.Errorf("stdout decode: %v", outLines)
	}

	// Decode stderr only
	errLines := DecodeAnnotated(annotated, streamErr)
	if len(errLines) != 1 || errLines[0] != "err1" {
		t.Errorf("stderr decode: %v", errLines)
	}

	// Decode combined
	allLines := DecodeAnnotated(annotated, 0)
	if len(allLines) != 3 {
		t.Errorf("combined decode: expected 3 lines, got %d: %v", len(allLines), allLines)
	}
}

func TestAnnotatedInterleavedOrder(t *testing.T) {
	lines := []AnnotatedLine{
		{Stream: streamOut, Text: "out1"},
		{Stream: streamErr, Text: "err1"},
		{Stream: streamOut, Text: "out2"},
		{Stream: streamErr, Text: "err2"},
	}
	annotated := EncodeAnnotatedLines(lines)

	// Combined should preserve order
	combined := DecodeAnnotated(annotated, 0)
	expected := []string{"out1", "err1", "out2", "err2"}
	if len(combined) != len(expected) {
		t.Fatalf("expected %d lines, got %d", len(expected), len(combined))
	}
	for i, want := range expected {
		if combined[i] != want {
			t.Errorf("line %d: want %q, got %q", i, want, combined[i])
		}
	}

	// Stdout only
	out := DecodeAnnotated(annotated, streamOut)
	if len(out) != 2 || out[0] != "out1" || out[1] != "out2" {
		t.Errorf("stdout filter: %v", out)
	}
}

func TestCacheList(t *testing.T) {
	setupTestCache(t)

	// Empty list
	entries, err := CacheList(10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}

	// Add entries
	writeTestEntry(t, "first\n", "", CacheMeta{Tag: "tag1", ExitCode: -1})
	writeTestEntry(t, "second\n", "", CacheMeta{Tag: "tag2", ExitCode: -1})

	entries, err = CacheList(10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	// Check both tags are present
	tags := map[string]bool{}
	for _, e := range entries {
		tags[e.Tags] = true
	}
	if !tags["tag1"] || !tags["tag2"] {
		t.Errorf("expected both tags, got entries: %+v", entries)
	}

	// Limit
	entries, err = CacheList(1, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

func TestCacheClearAll(t *testing.T) {
	setupTestCache(t)

	writeTestEntry(t, "a\n", "", CacheMeta{ExitCode: -1})
	writeTestEntry(t, "b\n", "", CacheMeta{ExitCode: -1})

	n, err := CacheClear(0)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("expected 2 cleared, got %d", n)
	}

	entries, _ := CacheList(10, "")
	if len(entries) != 0 {
		t.Errorf("expected 0 entries after clear, got %d", len(entries))
	}

	// Log files should be removed
	files, _ := os.ReadDir(cacheDir())
	for _, f := range files {
		if f.Name() != "recap.db" {
			t.Errorf("unexpected file after clear: %s", f.Name())
		}
	}
}

func TestCacheReadNotFound(t *testing.T) {
	setupTestCache(t)

	_, err := CacheReadLog("nonexistent-id", "")
	if err == nil {
		t.Error("expected error for nonexistent ID")
	}
}

func TestCacheReadLastEmpty(t *testing.T) {
	setupTestCache(t)

	_, err := CacheReadLog("last", "")
	if err == nil {
		t.Error("expected error when no entries exist")
	}
}

func TestSessionScoping(t *testing.T) {
	setupTestCache(t)

	// Write entries to different sessions
	writeTestEntry(t, "session-a\n", "", CacheMeta{ExitCode: 0, Session: "sess-a"})
	writeTestEntry(t, "session-b\n", "", CacheMeta{ExitCode: 0, Session: "sess-b"})
	writeTestEntry(t, "no-session\n", "", CacheMeta{ExitCode: -1})

	// List all (no session filter)
	all, err := CacheList(10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(all))
	}

	// List scoped to sess-a
	a, err := CacheList(10, "sess-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 1 {
		t.Fatalf("expected 1 entry for sess-a, got %d", len(a))
	}

	// "last" scoped to sess-a
	data, err := CacheReadLog("last", "sess-a")
	if err != nil {
		t.Fatal(err)
	}
	lines := DecodeAnnotated(data, streamOut)
	if len(lines) != 1 || lines[0] != "session-a" {
		t.Errorf("expected session-a content, got %v", lines)
	}

	// "last" scoped to sess-b
	data, err = CacheReadLog("last", "sess-b")
	if err != nil {
		t.Fatal(err)
	}
	lines = DecodeAnnotated(data, streamOut)
	if len(lines) != 1 || lines[0] != "session-b" {
		t.Errorf("expected session-b content, got %v", lines)
	}
}

func TestCacheGetEntry(t *testing.T) {
	setupTestCache(t)

	writeTestEntry(t, "out\n", "err\n", CacheMeta{Command: "test-cmd", ExitCode: 1, Session: "s1"})

	entry, err := CacheGetEntry("last", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Command != "test-cmd" {
		t.Errorf("expected command 'test-cmd', got %q", entry.Command)
	}
	if entry.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", entry.ExitCode)
	}
	if entry.StdoutLines != 1 {
		t.Errorf("expected 1 stdout line, got %d", entry.StdoutLines)
	}
	if entry.StderrLines != 1 {
		t.Errorf("expected 1 stderr line, got %d", entry.StderrLines)
	}
}

func TestHashContent(t *testing.T) {
	h1 := hashContent([]byte("hello"))
	h2 := hashContent([]byte("world"))
	h3 := hashContent([]byte("hello"))

	if h1 == h2 {
		t.Error("different content should have different hashes")
	}
	if h1 != h3 {
		t.Error("same content should have same hash")
	}
	if len(h1) != 16 { // 8 bytes = 16 hex chars
		t.Errorf("expected 16 char hash, got %d: %q", len(h1), h1)
	}
}
