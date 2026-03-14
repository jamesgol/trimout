# trimout

Cut 80-96% of tokens when piping command output to LLMs.

trimout caches the full unfiltered output of every command, then sends only the parts that matter — first and last N lines, grep matches, or stripped ANSI codes. Need more? Re-query the cache instead of re-running the command.

```
trimout run --ends 30 --strip-ansi -- make
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
trimout run [...] -- COMMAND    full output cached to ~/.cache/trimout/
                              metadata indexed in SQLite
                              filtered output to stdout
```

- Stdout and stderr captured separately, interleave order preserved
- On success (exit 0): shows stdout only
- On failure (exit > 0): auto-includes stderr
- Full output always cached — re-query with different filters anytime

## Install

```bash
go install github.com/james/trimout@latest
```

Or build from source:

```bash
go build -o trimout .
```

## Usage

```bash
# Run a command (preferred — captures exit code, duration, cwd, separate streams)
trimout run [OPTIONS] -- COMMAND [ARGS...]

# Pipe mode — reads stdin, applies filters
command | trimout [OPTIONS]

# Re-query cached output with different filters
trimout show <id> [OPTIONS]

# List cached entries
trimout list [--last N] [--all]

# Manage sessions
trimout session init    # create .trimout-session in cwd
trimout session         # print current session ID

# Clear cache
trimout clear [--older-than DURATION]
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
| `--session ID` | Override session ID (default: auto-detect from .trimout-session) |

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

## Sessions

Sessions prevent concurrent LLM coding sessions from stepping on each other. When a `.trimout-session` file exists, `show last` and `list` are automatically scoped to that session.

```bash
trimout session init        # creates .trimout-session in cwd
trimout list                # shows only this session's entries
trimout list --all          # shows entries from all sessions
```

The session file is auto-detected by walking up from the current directory.

## Configuration

| Environment Variable | Description |
|---------------------|-------------|
| `TRIMOUT_CACHE_DIR` | Override cache directory (default: `~/.cache/trimout` or `$XDG_CACHE_HOME/trimout`) |

## Claude Code Integration

Add the following to your `~/.claude/CLAUDE.md` (global) or project-level `CLAUDE.md`:

~~~markdown
## trimout — always use for command execution

At the start of each session, run `trimout session init` to create a session file.

Always use `trimout run` to execute commands instead of running them directly.
trimout caches the full unfiltered output (stdout and stderr separately) and
records exit code, duration, and working directory.

```bash
# Default for most commands — show first 30 + last 30 lines
trimout run --ends 30 --strip-ansi -- COMMAND

# Examples
trimout run --ends 30 --strip-ansi -- make
trimout run --ends 30 --strip-ansi -- pytest
trimout run --ends 30 --strip-ansi -- cargo build
```

If you need to see more of the output, NEVER re-run the command. Use:
  trimout show last --ends 100
  trimout show last --grep "error|warning"
  trimout show last --stderr
  trimout show last --tail 50

On failure (exit > 0), output auto-includes stderr. On success, only stdout
is shown — stderr is cached but hidden to save tokens.

Use `trimout list` to see recent captures with exit codes and durations.
~~~
