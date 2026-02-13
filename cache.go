package main

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type CacheEntry struct {
	ID        string
	Timestamp time.Time
	Command   string
	Size      int64
	LineCount int
	Hash      string
	Tags      string
}

func cacheDir() string {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "pipesum")
}

func ensureCacheDir() error {
	return os.MkdirAll(cacheDir(), 0o755)
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
		tags TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func hashContent(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:8])
}

// CacheWrite saves raw content to disk and records metadata in SQLite.
// Returns the generated ID.
func CacheWrite(raw []byte, lineCount int, command string, tag string) (string, error) {
	db, err := openDB()
	if err != nil {
		return "", err
	}
	defer db.Close()

	h := hashContent(raw)
	ts := time.Now().UTC()
	id := ts.Format("20060102-150405") + "-" + h[:8]

	rawPath := filepath.Join(cacheDir(), id+".raw")
	if err := os.WriteFile(rawPath, raw, 0o644); err != nil {
		return "", err
	}

	_, err = db.Exec(`INSERT INTO captures (id, timestamp, command, size, line_count, hash, tags)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, ts.Format(time.RFC3339), command, len(raw), lineCount, h, tag)
	if err != nil {
		os.Remove(rawPath)
		return "", err
	}
	return id, nil
}

// CacheRead reads the raw content for a given ID (or "last" for the most recent).
func CacheRead(id string) ([]byte, error) {
	if id == "last" {
		var err error
		id, err = lastID()
		if err != nil {
			return nil, err
		}
	}
	return os.ReadFile(filepath.Join(cacheDir(), id+".raw"))
}

func lastID() (string, error) {
	db, err := openDB()
	if err != nil {
		return "", err
	}
	defer db.Close()

	var id string
	err = db.QueryRow(`SELECT id FROM captures ORDER BY timestamp DESC LIMIT 1`).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("no cached entries found")
	}
	return id, nil
}

// CacheList returns the most recent entries.
func CacheList(limit int) ([]CacheEntry, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT id, timestamp, command, size, line_count, hash, tags
		FROM captures ORDER BY timestamp DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []CacheEntry
	for rows.Next() {
		var e CacheEntry
		var ts string
		if err := rows.Scan(&e.ID, &ts, &e.Command, &e.Size, &e.LineCount, &e.Hash, &e.Tags); err != nil {
			return nil, err
		}
		e.Timestamp, _ = time.Parse(time.RFC3339, ts)
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
