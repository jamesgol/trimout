# pipesum

Unix pipe preprocessor for LLMs. Sits between command output and your LLM, caching the full output while providing smart filtering to minimize tokens sent.

```
command | pipesum [filters] --> filtered output to stdout
                           \-> full output cached to ~/.cache/pipesum/
                           \-> metadata indexed in SQLite
```

## Install

```bash
go install github.com/james/pipesum@latest
```

Or build from source:

```bash
go build -o pipesum .
```

## Usage

```bash
# Run a command, cache output, apply filters (preferred — captures exit code, duration, cwd)
pipesum run [OPTIONS] -- COMMAND [ARGS...]

# Pipe mode — reads stdin, applies filters
command | pipesum [OPTIONS]

# Re-query cached output with different filters
pipesum show <id> [OPTIONS]

# List cached entries (scoped to current session if .pipesum-session exists)
pipesum list [--last N] [--all]

# Manage sessions
pipesum session init    # create .pipesum-session in cwd
pipesum session         # print current session ID

# Clear cache
pipesum clear [--older-than DURATION]
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
| `--session ID` | Override session ID (default: auto-detect from .pipesum-session) |

## Examples

```bash
# Run a build, dedup and trim for LLM context
pipesum run --dedup --mid 200 --strip-ansi -- make

# Run tests, keep failures only
pipesum run --grep "FAIL|ERROR" --stats -- pytest

# Re-examine last capture with different filters
pipesum show last --head 50

# Pipe mode still works
cargo test 2>&1 | pipesum -t tests

# List recent entries (shows exit code, duration, command)
pipesum list --last 5
```

## Claude Code Integration

Add the following to your `~/.claude/CLAUDE.md` (global) or project-level `CLAUDE.md` to teach Claude Code to use pipesum:

~~~markdown
## pipesum — always use for command execution

At the start of each session, run `pipesum session init` to create a session file.
This ensures `show last` and `list` are scoped to your session and won't collide
with other concurrent Claude Code sessions.

Always use `pipesum run` to execute commands instead of running them directly.
pipesum caches the full unfiltered output and captures exit code, duration, and
working directory. You can re-query cached output with different filters without
re-running the command.

```bash
# Instead of: make
# Use:        pipesum run --mid 200 --strip-ansi -- make

# Instead of: pytest | tail -50
# Use:        pipesum run --tail 50 --strip-ansi -- pytest

# Instead of: grep ERROR in command output
# Use:        pipesum run --grep ERROR --strip-ansi -- cmd
```

Recommended default for most commands:
  pipesum run --mid 200 --strip-ansi --compress-blank -- COMMAND

If you need to see more of the output, don't re-run the command. Use:
  pipesum show last [--head N | --tail N | --mid N | --grep PATTERN]

Use `pipesum list` to see recent captures with exit codes and durations.
~~~
