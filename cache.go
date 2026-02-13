package main

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const sessionFileName = ".pipesum-session"

type CacheEntry struct {
	ID        string
	Timestamp time.Time
	Command   string
	Size      int64
	LineCount int
	Hash      string
	Tags      string
	ExitCode  int
	Duration  time.Duration
	WorkDir   string
	Session   string
}

// detectSession reads the session ID from .pipesum-session in cwd,
// walking up to the git root. Returns empty string if not found.
func detectSession() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, sessionFileName))
		if err == nil {
			return strings.TrimSpace(string(data))
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// initSession creates a .pipesum-session file in the current directory.
// Returns the session ID (existing or newly created).
func initSession() (string, error) {
	existing := detectSession()
	if existing != "" {
		return existing, nil
	}
	id := fmt.Sprintf("%s-%s", time.Now().UTC().Format("20060102-150405"),
		hashContent([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))[:8])
	err := os.WriteFile(sessionFileName, []byte(id+"\n"), 0o600)
	if err != nil {
		return "", err
	}
	return id, nil
}

// cacheDirOverride allows tests to redirect cache to a temp directory.
var cacheDirOverride string

func cacheDir() string {
	if cacheDirOverride != "" {
		return cacheDirOverride
	}
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "pipesum")
}

func ensureCacheDir() error {
	return os.MkdirAll(cacheDir(), 0o700)
}

func dbPath() string {
	return filepath.Join(cacheDir(), "pipesum.db")
}

func openDB() (*sql.DB, error) {
	if err := ensureCacheDir(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath())
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS captures (
		id TEXT PRIMARY KEY,
		timestamp TEXT NOT NULL,
		command TEXT NOT NULL DEFAULT '',
		size INTEGER NOT NULL,
		line_count INTEGER NOT NULL,
		hash TEXT NOT NULL,
		tags TEXT NOT NULL DEFAULT '',
		exit_code INTEGER NOT NULL DEFAULT -1,
		duration_ms INTEGER NOT NULL DEFAULT 0,
		work_dir TEXT NOT NULL DEFAULT '',
		session TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		db.Close()
		return nil, err
	}
	// Migrate older schemas
	for _, col := range []string{
		"exit_code INTEGER NOT NULL DEFAULT -1",
		"duration_ms INTEGER NOT NULL DEFAULT 0",
		"work_dir TEXT NOT NULL DEFAULT ''",
		"session TEXT NOT NULL DEFAULT ''",
	} {
		db.Exec("ALTER TABLE captures ADD COLUMN " + col)
	}
	return db, nil
}

func hashContent(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:8])
}

// CacheMeta holds optional metadata for a cache write.
type CacheMeta struct {
	Command  string
	Tag      string
	ExitCode int
	Duration time.Duration
	WorkDir  string
	Session  string
}

// CacheWrite saves raw content to disk and records metadata in SQLite.
// Returns the generated ID.
func CacheWrite(raw []byte, lineCount int, meta CacheMeta) (string, error) {
	db, err := openDB()
	if err != nil {
		return "", err
	}
	defer db.Close()

	h := hashContent(raw)
	ts := time.Now().UTC()
	id := fmt.Sprintf("%s-%s%04d", ts.Format("20060102-150405"), h[:8], ts.Nanosecond()/100000)

	rawPath := filepath.Join(cacheDir(), id+".raw")
	if err := os.WriteFile(rawPath, raw, 0o600); err != nil {
		return "", err
	}

	_, err = db.Exec(`INSERT INTO captures (id, timestamp, command, size, line_count, hash, tags, exit_code, duration_ms, work_dir, session)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, ts.Format(time.RFC3339), meta.Command, len(raw), lineCount, h, meta.Tag,
		meta.ExitCode, meta.Duration.Milliseconds(), meta.WorkDir, meta.Session)
	if err != nil {
		os.Remove(rawPath)
		return "", err
	}
	return id, nil
}

// CacheRead reads the raw content for a given ID (or "last" for the most recent).
// If session is non-empty, "last" is scoped to that session.
func CacheRead(id string, session string) ([]byte, error) {
	if id == "last" {
		var err error
		id, err = lastID(session)
		if err != nil {
			return nil, err
		}
	}
	return os.ReadFile(filepath.Join(cacheDir(), id+".raw"))
}

func lastID(session string) (string, error) {
	db, err := openDB()
	if err != nil {
		return "", err
	}
	defer db.Close()

	var id string
	if session != "" {
		err = db.QueryRow(`SELECT id FROM captures WHERE session = ? ORDER BY timestamp DESC LIMIT 1`, session).Scan(&id)
	} else {
		err = db.QueryRow(`SELECT id FROM captures ORDER BY timestamp DESC LIMIT 1`).Scan(&id)
	}
	if err != nil {
		return "", fmt.Errorf("no cached entries found")
	}
	return id, nil
}

// CacheList returns the most recent entries. If session is non-empty, filters to that session.
func CacheList(limit int, session string) ([]CacheEntry, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var rows *sql.Rows
	if session != "" {
		rows, err = db.Query(`SELECT id, timestamp, command, size, line_count, hash, tags, exit_code, duration_ms, work_dir, session
			FROM captures WHERE session = ? ORDER BY timestamp DESC LIMIT ?`, session, limit)
	} else {
		rows, err = db.Query(`SELECT id, timestamp, command, size, line_count, hash, tags, exit_code, duration_ms, work_dir, session
			FROM captures ORDER BY timestamp DESC LIMIT ?`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []CacheEntry
	for rows.Next() {
		var e CacheEntry
		var ts string
		var durMs int64
		if err := rows.Scan(&e.ID, &ts, &e.Command, &e.Size, &e.LineCount, &e.Hash, &e.Tags, &e.ExitCode, &durMs, &e.WorkDir, &e.Session); err != nil {
			return nil, err
		}
		e.Timestamp, _ = time.Parse(time.RFC3339, ts)
		e.Duration = time.Duration(durMs) * time.Millisecond
		entries = append(entries, e)
	}
	return entries, nil
}

// CacheClear removes entries older than the given duration. If d is 0, removes all.
func CacheClear(d time.Duration) (int, error) {
	db, err := openDB()
	if err != nil {
		return 0, err
	}
	defer db.Close()

	var ids []string
	var rows *sql.Rows

	if d == 0 {
		rows, err = db.Query(`SELECT id FROM captures`)
	} else {
		cutoff := time.Now().UTC().Add(-d).Format(time.RFC3339)
		rows, err = db.Query(`SELECT id FROM captures WHERE timestamp < ?`, cutoff)
	}
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}

	for _, id := range ids {
		os.Remove(filepath.Join(cacheDir(), id+".raw"))
	}

	var res sql.Result
	if d == 0 {
		res, err = db.Exec(`DELETE FROM captures`)
	} else {
		cutoff := time.Now().UTC().Add(-d).Format(time.RFC3339)
		res, err = db.Exec(`DELETE FROM captures WHERE timestamp < ?`, cutoff)
	}
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
