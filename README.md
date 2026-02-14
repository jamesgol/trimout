# recap

Cut 80-96% of tokens when piping command output to LLMs.

recap caches the full unfiltered output of every command, then sends only the parts that matter — first and last N lines, grep matches, or stripped ANSI codes. Need more? Re-query the cache instead of re-running the command.

```
recap run --ends 30 --strip-ansi -- make
```

```
SCENARIO                         RAW_TOK FILT_TOK   SAVED
build log (--ends 30)               3844      470     88%
test output (--ends 30)             2842      562     81%
test output (--grep FAIL)           2842       41     99%
dpkg -l (--ends 10)               140712      787    100%
```

## How it works

```
recap run [...] -- COMMAND    full output cached to ~/.cache/recap/
                              metadata indexed in SQLite
                              filtered output to stdout
```

- Stdout and stderr captured separately, interleave order preserved
- On success (exit 0): shows stdout only
- On failure (exit > 0): auto-includes stderr
- Full output always cached — re-query with different filters anytime

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

# List cached entries
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
| `--session ID` | Override session ID (default: auto-detect from .recap-session) |

## Examples

```bash
# Build with smart filtering — see beginning and end, skip the noise
recap run --ends 30 --strip-ansi -- make

# Run tests, only show failures (99% token savings)
recap run --grep "FAIL|ERROR" --stats -- pytest

# Need more context? Re-query the cache — never re-run the command
recap show last --ends 100
recap show last --grep "warning"
recap show last --stderr
recap show last --tail 50

# Pipe mode works too
cargo test 2>&1 | recap -t tests

# List recent captures with exit codes and durations
recap list --last 5
```

## Sessions

Sessions prevent concurrent LLM coding sessions from stepping on each other. When a `.recap-session` file exists, `show last` and `list` are automatically scoped to that session.

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

Add the following to your `~/.claude/CLAUDE.md` (global) or project-level `CLAUDE.md`:

~~~markdown
## recap — always use for command execution

At the start of each session, run `recap session init` to create a session file.

Always use `recap run` to execute commands instead of running them directly.
recap caches the full unfiltered output (stdout and stderr separately) and
records exit code, duration, and working directory.

```bash
# Default for most commands — show first 30 + last 30 lines
recap run --ends 30 --strip-ansi -- COMMAND

# Examples
recap run --ends 30 --strip-ansi -- make
recap run --ends 30 --strip-ansi -- pytest
recap run --ends 30 --strip-ansi -- cargo build
```

If you need to see more of the output, NEVER re-run the command. Use:
  recap show last --ends 100
  recap show last --grep "error|warning"
  recap show last --stderr
  recap show last --tail 50

On failure (exit > 0), output auto-includes stderr. On success, only stdout
is shown — stderr is cached but hidden to save tokens.

Use `recap list` to see recent captures with exit codes and durations.
~~~
