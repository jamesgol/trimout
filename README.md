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
# Basic — reads stdin, applies filters, outputs to stdout
command | pipesum [OPTIONS]

# Re-query cached output with different filters
pipesum show <id> [OPTIONS]

# List cached entries
pipesum list [--last N]

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

## Examples

```bash
# Build output — dedup and trim for LLM context
make 2>&1 | pipesum --dedup --mid 200 --strip-ansi

# Test output — keep failures only
pytest 2>&1 | pipesum --grep "FAIL|ERROR" --stats

# Re-examine last capture with different filters
pipesum show last --head 50

# Tag a capture and list recent entries
cargo test 2>&1 | pipesum -t tests
pipesum list --last 5
```
