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
go install github.com/jamesgol/trimout@latest
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

# Pipe mode — reads stdin and applies filters (streams when possible)
command | trimout [OPTIONS]

# Re-query cached output with different filters
trimout show <id|last|tag> [OPTIONS]

# List cached entries
trimout list [--last N] [--all]

# Manage sessions
trimout session init    # create .trimout-session in cwd
trimout session         # print current session ID

# Clear cache
trimout clear [--older-than DURATION]
```

### Shell mode

trimout can act as a shell, automatically caching all command output:

```bash
# Use as shell directly
trimout -c "make"
trimout -c "pytest | tail -50"

# Set as Claude Code's shell for automatic caching
export CLAUDE_CODE_SHELL=/path/to/trimout
```

In shell mode:
- All commands are wrapped with `trimout run` for automatic caching
- Trailing `| tail -N`, `| head -N`, and `| grep PATTERN` are rewritten to native trimout flags, so the full pre-pipe output is cached
- Commands inside `eval '...'` (used by Claude Code) are detected and rewritten too
- Shell flags like `-c`, `-ic`, `-lc`, and `-c -l` are handled (matching how tools invoke shells)
- Full output is shown by default — no silent filtering
- When output is filtered, a stderr hint shows the cache ID:
  `[trimout] 5 of 100 lines shown. Full output: trimout show <id>`
- trimout's own commands (`trimout show`, `trimout list`, etc.) are passed through to bash

---

## Filter Options

| Flag | Description |
|-----|-------------|
| `--ends N` | Keep first N and last N lines with omission marker |
| `--mid N` | Drop first N and last N lines, keep the middle |
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
| `--no-hint` | Suppress the stderr hint when output is filtered |
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

# Tag a capture for easy retrieval later
trimout run -t build --ends 30 -- make
trimout show build --grep "warning"

# Need more context? Re-query the cache — never re-run the command
trimout show last --ends 100
trimout show last --grep "warning"
trimout show last --stderr
trimout show last --tail 50

# Pipe mode works too (streams when possible)
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

### Option 1: Shell mode (recommended)

Set trimout as Claude Code's shell. All commands are automatically cached — no CLAUDE.md instructions needed. When output is filtered, trimout prints a hint to stderr that Claude sees in the tool result:

```
[trimout] 50 of 397 lines shown. Full output: trimout show 20260314-191224-abc123
```

Claude picks up the cache ID from this hint and uses `trimout show` to retrieve more output without rerunning the command.

**Setup:**

Add to your `~/.claude/settings.json`:

```json
{
  "env": {
    "CLAUDE_CODE_SHELL": "/path/to/trimout"
  }
}
```

That's it. No CLAUDE.md changes are strictly required — the stderr hints teach Claude how to use trimout at runtime. But you can optionally add guidance:

~~~markdown
## trimout — automatic command caching

All commands are automatically cached by trimout. When output is filtered,
stderr shows a hint with the cache ID. Use it to retrieve full output:

```bash
trimout show <id>                    # full output
trimout show <id> --grep "error"     # search cached output
trimout show <id> --stderr           # see stderr
```
~~~

### Option 2: Explicit `trimout run`

If you prefer not to change the shell, instruct Claude to use `trimout run` directly.

Add to your `CLAUDE.md`:

~~~markdown
## trimout — always use for command execution

Always use `trimout run` to execute commands instead of running them directly.

```bash
trimout run --ends 30 --strip-ansi -- COMMAND
```

If the output says `[trimout] N of M lines shown`, **do not rerun the command**.
Use the cache ID from that message to retrieve more output:

```bash
trimout show <id> --ends 100
trimout show <id> --grep "error|warning"
trimout show <id> --stderr
```

Use `trimout list` to see recent captures with exit codes and durations.
~~~
