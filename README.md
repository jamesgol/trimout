# recap

Unix pipe preprocessor for LLMs. Sits between command output and your LLM, caching the full output while providing smart filtering to minimize tokens sent.

```
command | recap [filters] --> filtered output to stdout
                           \-> full output cached to ~/.cache/recap/
                           \-> metadata indexed in SQLite
```

Stdout and stderr are captured separately in an annotated log format that preserves interleave order. On playback, `show` defaults to stdout-only on success and auto-includes stderr when the command failed — giving the LLM exactly what it needs with minimal tokens.

## Install

```bash
go install github.com/james/recap@latest
```

Or build from source:

```bash
go build -o recap .
```

## Usage

```bash
# Run a command (preferred — captures exit code, duration, cwd, separate streams)
recap run [OPTIONS] -- COMMAND [ARGS...]

# Pipe mode — reads stdin, applies filters
command | recap [OPTIONS]

# Re-query cached output with different filters
recap show <id> [OPTIONS]

# List cached entries (scoped to current session if .recap-session exists)
recap list [--last N] [--all]

# Manage sessions
recap session init    # create .recap-session in cwd
recap session         # print current session ID

# Clear cache
recap clear [--older-than DURATION]
```

## Filter Options

| Flag | Description |
|------|-------------|
| `--dedup` | Remove consecutive duplicate lines (like `uniq`) |
| `--dedup-all` | Remove all duplicate lines, keeping first occurrence |
| `--head N` | Keep first N lines |
| `--tail N` | Keep last N lines |
| `--mid N` | Keep first N/2 and last N/2 lines with omission marker |
| `--grep PATTERN` | Keep only lines matching pattern |
| `--grep-v PATTERN` | Remove lines matching pattern |
| `--strip-ansi` | Remove ANSI escape codes |
| `--compress-blank` | Collapse multiple blank lines into one |
| `--max-line-len N` | Truncate lines longer than N chars |
| `--stats` | Prepend a summary line |
| `-t, --tag TAG` | Tag this capture for later retrieval |
| `--no-cache` | Skip caching the output |
| `-v, --verbose` | Print cache ID to stderr |
| `--session ID` | Override session ID (default: auto-detect from .recap-session) |
| `--stderr` | Show stderr instead of stdout |
| `--combined` | Show both stdout and stderr |
| `-q, --quiet` | Suppress output entirely (run mode: cache only) |

## Stream Selection

When using `recap run` or `recap show`:

- **Success (exit 0)**: shows stdout only (default)
- **Failure (exit > 0)**: auto-shows combined stdout + stderr
- `--stderr`: show only stderr
- `--combined`: show both streams regardless of exit code

This saves tokens on success (stderr is usually noise) while automatically including error output on failure.

## Examples

```bash
# Run a build, dedup and trim for LLM context
recap run --dedup --mid 200 --strip-ansi -- make

# Run tests, keep failures only
recap run --grep "FAIL|ERROR" --stats -- pytest

# Re-examine last capture — stdout only
recap show last --head 50

# See just stderr from last capture
recap show last --stderr

# See combined output preserving interleave order
recap show last --combined

# Pipe mode still works (captures as stdout, no exit code)
cargo test 2>&1 | recap -t tests

# List recent entries (shows exit code, stdout/stderr line counts, duration)
recap list --last 5
```

## Sessions

Sessions prevent concurrent Claude Code instances from stepping on each other. When a `.recap-session` file exists, `show last` and `list` are automatically scoped to that session.

```bash
recap session init        # creates .recap-session in cwd
recap list                # shows only this session's entries
recap list --all          # shows entries from all sessions
```

The session file is auto-detected by walking up from the current directory.

## Configuration

| Environment Variable | Description |
|---------------------|-------------|
| `RECAP_CACHE_DIR` | Override cache directory (default: `~/.cache/recap` or `$XDG_CACHE_HOME/recap`) |

## Claude Code Integration

Add the following to your `~/.claude/CLAUDE.md` (global) or project-level `CLAUDE.md` to teach Claude Code to use recap:

~~~markdown
## recap — always use for command execution

At the start of each session, run `recap session init` to create a session file.
This ensures `show last` and `list` are scoped to your session and won't collide
with other concurrent Claude Code sessions.

Always use `recap run` to execute commands instead of running them directly.
recap caches the full unfiltered output (stdout and stderr separately) and
records exit code, duration, and working directory. Use `--mid` to see only the
beginning and end of output — the full output is always cached and you can
retrieve more with `recap show last` if needed.

```bash
# Default for most commands — see first 30 + last 30 lines
recap run --mid 60 --strip-ansi -- COMMAND

# Examples
recap run --mid 60 --strip-ansi -- make
recap run --mid 60 --strip-ansi -- pytest
recap run --mid 60 --strip-ansi -- cargo build
```

If you need to see more of the output, NEVER re-run the command. Use:
  recap show last --mid 200
  recap show last --grep "error|warning"
  recap show last --stderr
  recap show last --tail 100

On failure (exit > 0), output auto-includes stderr. On success, only stdout
is shown — stderr is cached but hidden to save tokens.

Use `recap list` to see recent captures with exit codes and durations.
~~~
