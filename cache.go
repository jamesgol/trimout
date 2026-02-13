package main

import (
	"bufio"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const sessionFileName = ".recap-session"

// Stream prefixes in the annotated log file.
const (
	streamOut = '1' // stdout
	streamErr = '2' // stderr
)

type CacheEntry struct {
	ID          string
	Timestamp   time.Time
	Command     string
	StdoutSize  int64
	StderrSize  int64
	StdoutLines int
	StderrLines int
	Hash        string
	Tags        string
	ExitCode    int
	Duration    time.Duration
	WorkDir     string
	Session     string
}

// detectSession reads the session ID from .recap-session in cwd,
// walking up to the root. Returns empty string if not found.
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

// initSession creates a .recap-session file in the current directory.
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
	if dir := os.Getenv("RECAP_CACHE_DIR"); dir != "" {
		return dir
	}
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "recap")
}

func ensureCacheDir() error {
	return os.MkdirAll(cacheDir(), 0o700)
}

func dbPath() string {
	return filepath.Join(cacheDir(), "recap.db")
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
		stdout_size INTEGER NOT NULL DEFAULT 0,
		stderr_size INTEGER NOT NULL DEFAULT 0,
		stdout_lines INTEGER NOT NULL DEFAULT 0,
		stderr_lines INTEGER NOT NULL DEFAULT 0,
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

// EncodeAnnotated builds the annotated log from separate stdout/stderr content.
// Each line is prefixed with '1\t' (stdout) or '2\t' (stderr).
// For pipe mode, all lines are tagged as stdout.
func EncodeAnnotated(stdout, stderr []byte) []byte {
	var b strings.Builder
	for _, line := range splitLines(stdout) {
		b.WriteByte(streamOut)
		b.WriteByte('\t')
		b.WriteString(line)
		b.WriteByte('\n')
	}
	for _, line := range splitLines(stderr) {
		b.WriteByte(streamErr)
		b.WriteByte('\t')
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// EncodeAnnotatedInterleaved writes lines to the annotated log preserving order.
// Each line is a (stream, text) pair.
type AnnotatedLine struct {
	Stream byte // streamOut or streamErr
	Text   string
}

func EncodeAnnotatedLines(lines []AnnotatedLine) []byte {
	var b strings.Builder
	for _, l := range lines {
		b.WriteByte(l.Stream)
		b.WriteByte('\t')
		b.WriteString(l.Text)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// DecodeAnnotated extracts lines from an annotated log file, filtered by stream.
// If stream is 0, returns all lines (combined). Otherwise filters to the given stream.
func DecodeAnnotated(data []byte, stream byte) []string {
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 2 || line[1] != '\t' {
			// Malformed or legacy .raw line — include as stdout
			if stream == 0 || stream == streamOut {
				lines = append(lines, line)
			}
			continue
		}
		prefix := line[0]
		content := line[2:]
		if stream == 0 || prefix == stream {
			lines = append(lines, content)
		}
	}
	return lines
}

func splitLines(data []byte) []string {
	s := string(data)
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func countLines(data []byte) int {
	return len(splitLines(data))
}

// CacheWrite saves annotated content to disk and records metadata in SQLite.
// Returns the generated ID.
func CacheWrite(annotated []byte, stdoutBytes, stderrBytes int, stdoutLines, stderrLines int, meta CacheMeta) (string, error) {
	db, err := openDB()
	if err != nil {
		return "", err
	}
	defer db.Close()

	h := hashContent(annotated)
	ts := time.Now().UTC()
	id := fmt.Sprintf("%s-%s%04d", ts.Format("20060102-150405"), h[:8], ts.Nanosecond()/100000)

	logPath := filepath.Join(cacheDir(), id+".log")
	if err := os.WriteFile(logPath, annotated, 0o600); err != nil {
		return "", err
	}

	_, err = db.Exec(`INSERT INTO captures (id, timestamp, command, stdout_size, stderr_size, stdout_lines, stderr_lines, hash, tags, exit_code, duration_ms, work_dir, session)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, ts.Format(time.RFC3339), meta.Command, stdoutBytes, stderrBytes, stdoutLines, stderrLines, h, meta.Tag,
		meta.ExitCode, meta.Duration.Milliseconds(), meta.WorkDir, meta.Session)
	if err != nil {
		os.Remove(logPath)
		return "", err
	}
	return id, nil
}

// CacheReadLog reads the annotated log for a given ID (or "last" for the most recent).
// If session is non-empty, "last" is scoped to that session.
func CacheReadLog(id string, session string) ([]byte, error) {
	if id == "last" {
		var err error
		id, err = lastID(session)
		if err != nil {
			return nil, err
		}
	}
	// Try new .log format first, fall back to legacy .raw
	data, err := os.ReadFile(filepath.Join(cacheDir(), id+".log"))
	if err != nil {
		data, err = os.ReadFile(filepath.Join(cacheDir(), id+".raw"))
	}
	return data, err
}

// CacheGetEntry returns the metadata for a given ID.
func CacheGetEntry(id string, session string) (*CacheEntry, error) {
	if id == "last" {
		var err error
		id, err = lastID(session)
		if err != nil {
			return nil, err
		}
	}
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var e CacheEntry
	var ts string
	var durMs int64
	err = db.QueryRow(`SELECT id, timestamp, command, stdout_size, stderr_size, stdout_lines, stderr_lines, hash, tags, exit_code, duration_ms, work_dir, session
		FROM captures WHERE id = ?`, id).Scan(
		&e.ID, &ts, &e.Command, &e.StdoutSize, &e.StderrSize, &e.StdoutLines, &e.StderrLines,
		&e.Hash, &e.Tags, &e.ExitCode, &durMs, &e.WorkDir, &e.Session)
	if err != nil {
		return nil, fmt.Errorf("entry not found: %s", id)
	}
	e.Timestamp, _ = time.Parse(time.RFC3339, ts)
	e.Duration = time.Duration(durMs) * time.Millisecond
	return &e, nil
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
		rows, err = db.Query(`SELECT id, timestamp, command, stdout_size, stderr_size, stdout_lines, stderr_lines, hash, tags, exit_code, duration_ms, work_dir, session
			FROM captures WHERE session = ? ORDER BY timestamp DESC LIMIT ?`, session, limit)
	} else {
		rows, err = db.Query(`SELECT id, timestamp, command, stdout_size, stderr_size, stdout_lines, stderr_lines, hash, tags, exit_code, duration_ms, work_dir, session
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
		if err := rows.Scan(&e.ID, &ts, &e.Command, &e.StdoutSize, &e.StderrSize, &e.StdoutLines, &e.StderrLines,
			&e.Hash, &e.Tags, &e.ExitCode, &durMs, &e.WorkDir, &e.Session); err != nil {
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
		os.Remove(filepath.Join(cacheDir(), id+".log"))
		os.Remove(filepath.Join(cacheDir(), id+".raw")) // legacy cleanup
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
