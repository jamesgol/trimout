# trimout

Run commands once, cache the full output, and send only the useful parts to your LLM.

`trimout` cuts token usage by **80–96%** by filtering noisy command output down to the lines that matter — while keeping the full original output in a local cache so you can re-query it later without rerunning the command.

```bash
trimout run --ends 30 --strip-ansi -- make
```

```
SCENARIO                         RAW_TOK FILT_TOK   SAVED
build log (--ends 30)               3844      470     88%
test output (--ends 30)             2842      562     81%
test output (--grep FAIL)           2842       41     99%
dpkg -l (--ends 10)               140712      787    100%
```

## Typical workflow

```bash
# Run a noisy command, but only show useful edges
trimout run --ends 30 --strip-ansi -- make

# Need more context? Re-query the cached output
trimout show last --grep "error|warning"
trimout show last --stderr
trimout show last --tail 100
```

Unlike `tail` or `grep`, `trimout` **never throws information away**. The full original output is always cached unless `--no-cache` is used.

---

## How it works

```
trimout run [...] -- COMMAND    full output cached to ~/.cache/trimout/
                                metadata indexed in SQLite
                                filtered output to stdout
```

Key behavior:

- Full stdout and stderr are captured and cached
- Interleaved order is preserved
- On success (`exit 0`): filtered stdout is shown
- On failure (`exit > 0`): stderr is automatically included
- Cached output can be re-filtered anytime with `trimout show`

This makes `trimout` ideal for:

- build logs
- test output
- LLM-assisted debugging
- expensive commands you do not want to rerun

---

## Install

```bash
go install github.com/james/trimout@latest
```

Or build from source:

```bash
go build -o trimout .
```

---

## Usage

```bash
# Run a command (preferred — captures exit code, duration, cwd, separate streams)
trimout run [OPTIONS] -- COMMAND [ARGS...]

# Pipe mode — reads stdin and applies filters
command | trimout [OPTIONS]

# Re-query cached output with different filters
trimout show <id|last> [OPTIONS]

# List cached entries
trimout list [--last N] [--all]

# Manage sessions
trimout session init    # create .trimout-session in cwd
trimout session         # print current session ID

# Clear cache
trimout clear [--older-than DURATION]
```

---

## Filter Options

| Flag | Description |
|-----|-------------|
| `--ends N` | Keep first N and last N lines with omission marker |
| `--head N` | Keep first N lines |
| `--tail N` | Keep last N lines |
| `--grep PATTERN` | Keep only lines matching pattern |
| `--grep-v PATTERN` | Remove lines matching pattern |
| `--dedup` | Remove consecutive duplicate lines (like `uniq`) |
| `--dedup-all` | Remove all duplicate lines, keeping first occurrence |
| `--strip-ansi` | Remove ANSI escape codes |
| `--compress-blank` | Collapse multiple blank lines into one |
| `--max-line-len N` | Truncate lines longer than N chars |
| `--stats` | Prepend a summary line |
| `-t, --tag TAG` | Tag this capture for later retrieval |
| `--no-cache` | Skip caching the output |
| `-v, --verbose` | Print cache ID to stderr |
| `--stderr` | Show stderr instead of stdout |
| `--combined` | Show both stdout and stderr |
| `-q, --quiet` | Suppress output entirely (run mode: cache only) |
| `--input-jsonl FILE` | JSONL pattern file to match against (see [Pattern Matching](#pattern-matching)) |
| `--input-text FILE` | Plain text pattern file, one literal per line (see [Pattern Matching](#pattern-matching)) |
| `--output-jsonl FILE` | Write pattern matches as JSONL to file (use `-` for stdout) |
| `--session ID` | Override session ID (default: auto-detect from `.trimout-session`) |

---

## Examples

```bash
# Build with smart filtering — see beginning and end, skip the noise
trimout run --ends 30 --strip-ansi -- make

# Run tests, only show failures (99% token savings)
trimout run --grep "FAIL|ERROR" --stats -- pytest

# Need more context? Re-query the cache — never re-run the command
trimout show last --ends 100
trimout show last --grep "warning"
trimout show last --stderr
trimout show last --tail 50

# Pipe mode works too
cargo test 2>&1 | trimout -t tests

# List recent captures with exit codes and durations
trimout list --last 5
```

---

## Pattern Matching

Two input formats are supported:

### Plain text (`--input-text`)

One literal pattern per line. Lines starting with `#` are comments. Simple and quick:

```
PHPSESSID
wp-content
X-Powered-By
```

Each pattern is matched using `strings.Contains`. The pattern string is used as the id in structured output.

### JSONL (`--input-jsonl`)

One JSON object per line, with full metadata:

```jsonl
{"type":"literal","pattern":"PHPSESSID","id":"php","confidence":"confirmed","meta":"cookie"}
{"type":"regex","pattern":"Server: Apache/[0-9]","id":"apache-httpd","confidence":"confirmed","meta":"header"}
```

| Field | Description |
|-------|-------------|
| `type` | `"literal"` (uses `strings.Contains`) or `"regex"` (compiled once at startup) |
| `pattern` | The string or regex to match |
| `id` | Identifier for what matched (opaque string, up to the consumer) |
| `confidence` | Signal strength: `weak`, `possible`, `likely`, `confirmed` |
| `meta` | Freeform tag for context (e.g., where the pattern is typically seen) |

Lines starting with `#` are comments. Blank lines are skipped.

### Output

Literals are checked first (fast path), then regexes. By default, matching lines are printed as plain text.

Use `--output-jsonl` to write structured matches to a file while still displaying filtered output on stdout:

```bash
curl -s example.com | trimout --input-jsonl fingerprints.jsonl --output-jsonl matches.jsonl
```

Use `-` to write JSONL to stdout instead (replaces the normal display output):

```bash
curl -s example.com | trimout --input-jsonl fingerprints.jsonl --output-jsonl -
```

```jsonl
{"line":2,"content":"Server: Apache/2.4.41","pattern_id":"apache-httpd","confidence":"confirmed","meta":"header"}
{"line":5,"content":"Set-Cookie: PHPSESSID=abc123","pattern_id":"php","confidence":"confirmed","meta":"cookie"}
```

Without `--output-jsonl`, matching lines are printed as plain text (like `--grep`, but multi-pattern with metadata). Pattern matching composes with all other filters.

---

## Sessions

Sessions prevent concurrent LLM coding sessions or terminal workflows from stepping on each other.

When a `.trimout-session` file exists, `show last` and `list` automatically scope results to that session.

```bash
trimout session init        # creates .trimout-session in cwd
trimout list                # shows only this session's entries
trimout list --all          # shows entries from all sessions
```

The session file is auto-detected by walking up from the current directory.

---

## Configuration

| Environment Variable | Description |
|---------------------|-------------|
| `TRIMOUT_CACHE_DIR` | Override cache directory (default: `~/.cache/trimout` or `$XDG_CACHE_HOME/trimout`) |

---

## Cache contents

`trimout` stores full command output locally in the cache directory.

This includes both stdout and stderr. Avoid using it with commands that may print secrets unless local caching is acceptable for your workflow.

---

## Claude Code Integration

Add the following to your `~/.claude/CLAUDE.md` (global) or project-level `CLAUDE.md`.

~~~markdown
## trimout — always use for command execution

At the start of each session, run:

```bash
trimout session init
```

Always use `trimout run` to execute commands instead of running them directly.

```bash
# Default for most commands — show first 30 + last 30 lines
trimout run --ends 30 --strip-ansi -- COMMAND
```

Examples:

```bash
trimout run --ends 30 --strip-ansi -- make
trimout run --ends 30 --strip-ansi -- pytest
trimout run --ends 30 --strip-ansi -- cargo build
```

If you need to see more of the output, **do not rerun the command**. Re-query the cache instead:

```bash
trimout show last --ends 100
trimout show last --grep "error|warning"
trimout show last --stderr
trimout show last --tail 50
```

On success, only stdout is shown. On failure, stderr is automatically included. Full stdout and stderr are always cached.

Use `trimout list` to see recent captures with exit codes and durations.
~~~
