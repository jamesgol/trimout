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

func TestCacheWriteAndRead(t *testing.T) {
	setupTestCache(t)

	content := []byte("line1\nline2\nline3\n")
	id, err := CacheWrite(content, 3, CacheMeta{Command: "test-cmd", Tag: "mytag", ExitCode: -1})
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("expected non-empty ID")
	}

	// Read back by ID
	got, err := CacheRead(id)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch: got %q", got)
	}

	// Read back by "last"
	got, err = CacheRead("last")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Errorf("last content mismatch: got %q", got)
	}
}

func TestCacheList(t *testing.T) {
	setupTestCache(t)

	// Empty list
	entries, err := CacheList(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}

	// Add entries
	CacheWrite([]byte("first\n"), 1, CacheMeta{Tag: "tag1", ExitCode: -1})
	CacheWrite([]byte("second\n"), 1, CacheMeta{Tag: "tag2", ExitCode: -1})

	entries, err = CacheList(10)
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
	entries, err = CacheList(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

func TestCacheClearAll(t *testing.T) {
	setupTestCache(t)

	CacheWrite([]byte("a\n"), 1, CacheMeta{ExitCode: -1})
	CacheWrite([]byte("b\n"), 1, CacheMeta{ExitCode: -1})

	n, err := CacheClear(0)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("expected 2 cleared, got %d", n)
	}

	entries, _ := CacheList(10)
	if len(entries) != 0 {
		t.Errorf("expected 0 entries after clear, got %d", len(entries))
	}

	// Raw files should be removed
	files, _ := os.ReadDir(cacheDir())
	for _, f := range files {
		if f.Name() != "pipesum.db" {
			t.Errorf("unexpected file after clear: %s", f.Name())
		}
	}
}

func TestCacheReadNotFound(t *testing.T) {
	setupTestCache(t)

	_, err := CacheRead("nonexistent-id")
	if err == nil {
		t.Error("expected error for nonexistent ID")
	}
}

func TestCacheReadLastEmpty(t *testing.T) {
	setupTestCache(t)

	_, err := CacheRead("last")
	if err == nil {
		t.Error("expected error when no entries exist")
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
