package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
)

// shellDefaults are applied when no trailing filter is detected.
// No filtering by default — full output is shown. The value is the caching.
var shellDefaults []string

// parseShellArgs extracts the command from shell-style arguments.
// Handles various invocation styles:
//   - trimout -c "cmd"
//   - trimout -ic "cmd"
//   - trimout -c -l "cmd"     (Claude CLI snapshot call)
//   - trimout -c -l -i "cmd"
//
// Returns the command string and true if -c was found.
func parseShellArgs(args []string) (string, bool) {
	if len(args) == 0 {
		return "", false
	}

	// First, check if any arg contains -c (combined or standalone)
	foundC := false
	for i, arg := range args {
		if !strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "--") {
			// Not a short flag — this is the command if we already found -c
			if foundC {
				return arg, true
			}
			return "", false
		}

		letters := arg[1:]

		// Validate all letters are known shell flags
		valid := true
		for _, ch := range letters {
			if ch != 'c' && ch != 'i' && ch != 'l' && ch != 's' {
				valid = false
				break
			}
		}
		if !valid {
			if foundC {
				// Unknown flag after -c — treat as command
				return arg, true
			}
			return "", false
		}

		if strings.ContainsRune(letters, 'c') {
			foundC = true
		}

		// If this is the last arg and we found -c, there's no command
		if i == len(args)-1 && foundC {
			fmt.Fprintf(os.Stderr, "trimout: -c requires a command\n")
			os.Exit(1)
		}
	}

	return "", false
}

// isShellLoginFlag returns true for flags shells receive when probed
// (e.g., -l, -i, -il, --login).
func isShellLoginFlag(arg string) bool {
	if arg == "--login" {
		return true
	}
	if !strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "--") {
		return false
	}
	for _, ch := range arg[1:] {
		if ch != 'i' && ch != 'l' {
			return false
		}
	}
	return len(arg) > 1
}

// runShell handles -c "command" invocations when trimout is used as SHELL.
func runShell(command string) {
	command = strings.TrimSpace(command)
	if command == "" {
		os.Exit(0)
	}

	// Pass through trimout's own commands to avoid recursion
	if isTrimoutCommand(command) {
		execBash(command)
		return
	}

	// Try to rewrite the command for caching
	if rewritten, ok := rewriteCommand(command); ok {
		execBash(rewritten)
		return
	}

	// Default: wrap with trimout run
	args := []string{"run"}
	args = append(args, shellDefaults...)
	args = append(args, "--", "bash", "-c", command)
	runSelf(args)
}

// rewriteCommand tries to rewrite a command for caching.
// It handles both top-level pipes and pipes inside eval '...'.
func rewriteCommand(command string) (string, bool) {
	// Check for eval 'USER_CMD' pattern (Claude CLI wraps commands this way)
	if inner, prefix, suffix, ok := extractEval(command); ok {
		if rewritten, rok := rewriteInnerCommand(inner); rok {
			// Put the rewritten command back inside eval
			return prefix + "eval '" + escapeEvalSingle(rewritten) + "'" + suffix, true
		}
		// Eval found but no rewritable pipe — wrap the inner command with trimout run
		wrapped := "trimout run -- bash -c " + shellQuote(inner)
		return prefix + "eval '" + escapeEvalSingle(wrapped) + "'" + suffix, true
	}

	// Top-level pipe rewriting (no eval wrapper)
	if baseCmd, flags, ok := detectTrailingFilter(command); ok {
		self, err := os.Executable()
		if err != nil {
			return "", false
		}
		rewritten := self + " run"
		for _, f := range flags {
			rewritten += " " + f
		}
		rewritten += " -- bash -c " + shellQuote(baseCmd)
		return rewritten, true
	}

	return "", false
}

// rewriteInnerCommand rewrites a user command extracted from eval.
// Returns the rewritten command and true if a pipe was detected and rewritten.
func rewriteInnerCommand(inner string) (string, bool) {
	baseCmd, flags, ok := detectTrailingFilter(inner)
	if !ok {
		return "", false
	}
	self, err := os.Executable()
	if err != nil {
		return "", false
	}
	rewritten := self + " run"
	for _, f := range flags {
		rewritten += " " + f
	}
	rewritten += " -- bash -c " + shellQuote(baseCmd)
	return rewritten, true
}

// extractEval finds eval '...' in a command string and returns the inner command,
// the prefix before eval, and the suffix after it.
func extractEval(command string) (inner, prefix, suffix string, ok bool) {
	// Find "eval '" — the start of the eval block
	evalIdx := strings.Index(command, "eval '")
	if evalIdx < 0 {
		return "", "", "", false
	}

	contentStart := evalIdx + len("eval '")

	// Find the matching closing single quote.
	// Inside single quotes, there are no escapes in bash — the only way to
	// include a literal ' is to end the quote, add \', and reopen: 'can'\''t'
	// But for Claude CLI's use case, the inner command doesn't contain single quotes.
	closeIdx := strings.Index(command[contentStart:], "'")
	if closeIdx < 0 {
		return "", "", "", false
	}
	closeIdx += contentStart

	inner = command[contentStart:closeIdx]
	prefix = command[:evalIdx]
	suffix = command[closeIdx+1:]
	return inner, prefix, suffix, true
}

// shellQuote wraps a string in single quotes for safe shell embedding.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// escapeEvalSingle escapes a string for embedding inside eval '...'.
// Since single quotes can't contain single quotes in bash, we use the
// end-quote-escape-reopen pattern: ' → '\''
func escapeEvalSingle(s string) string {
	return strings.ReplaceAll(s, "'", "'\\''")
}

// isTrimoutCommand returns true if the command starts with trimout subcommands.
func isTrimoutCommand(command string) bool {
	words := strings.Fields(command)
	if len(words) == 0 {
		return false
	}
	// Direct "trimout" invocation
	base := words[0]
	if base == "trimout" || strings.HasSuffix(base, "/trimout") {
		return true
	}
	return false
}

// detectTrailingFilter looks for a trailing "| tail -N", "| head -N",
// or "| grep PATTERN" at the end of a command and converts it to trimout flags.
func detectTrailingFilter(command string) (baseCmd string, flags []string, ok bool) {
	pipeIdx := findLastPipe(command)
	if pipeIdx < 0 {
		return "", nil, false
	}

	base := strings.TrimSpace(command[:pipeIdx])
	trailer := strings.TrimSpace(command[pipeIdx+1:])
	if base == "" || trailer == "" {
		return "", nil, false
	}

	flags = parseTrailerFlags(trailer)
	if flags == nil {
		return "", nil, false
	}

	return base, flags, true
}

var (
	tailRe = regexp.MustCompile(`^tail\s+-(\d+)$|^tail\s+-n\s+(\d+)$`)
	headRe = regexp.MustCompile(`^head\s+-(\d+)$|^head\s+-n\s+(\d+)$`)
	grepRe = regexp.MustCompile(`^grep\s+([^-]\S*)$|^grep\s+('[^']*')$|^grep\s+("[^"]*")$`)
)

// parseTrailerFlags converts a trailing command like "tail -50" into trimout flags.
func parseTrailerFlags(trailer string) []string {
	if m := tailRe.FindStringSubmatch(trailer); m != nil {
		n := m[1]
		if n == "" {
			n = m[2]
		}
		return []string{"--tail", n}
	}
	if m := headRe.FindStringSubmatch(trailer); m != nil {
		n := m[1]
		if n == "" {
			n = m[2]
		}
		return []string{"--head", n}
	}
	if m := grepRe.FindStringSubmatch(trailer); m != nil {
		// Find first non-empty capture group
		pattern := ""
		for _, g := range m[1:] {
			if g != "" {
				pattern = g
				break
			}
		}
		pattern = strings.TrimSpace(pattern)
		// Strip surrounding quotes if present
		if len(pattern) >= 2 {
			if (pattern[0] == '"' && pattern[len(pattern)-1] == '"') ||
				(pattern[0] == '\'' && pattern[len(pattern)-1] == '\'') {
				pattern = pattern[1 : len(pattern)-1]
			}
		}
		return []string{"--grep", pattern}
	}
	return nil
}

// findLastPipe returns the index of the last unquoted, non-||, non-subshell
// pipe in command. Returns -1 if not found.
func findLastPipe(command string) int {
	lastPipe := -1
	inSingle := false
	inDouble := false
	escaped := false
	depth := 0     // nesting depth for $() and ()
	inBacktick := false

	for i := 0; i < len(command); i++ {
		ch := command[i]

		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && !inSingle {
			escaped = true
			continue
		}
		if ch == '\'' && !inDouble && !inBacktick {
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if inSingle {
			continue
		}
		if ch == '`' {
			inBacktick = !inBacktick
			continue
		}
		if inBacktick {
			continue
		}
		// Track $() and () nesting
		if ch == '(' {
			depth++
			continue
		}
		if ch == ')' && depth > 0 {
			depth--
			continue
		}
		if depth > 0 {
			continue
		}
		if inDouble {
			continue
		}
		if ch == '|' {
			// Skip || (logical or) and |& (pipe stderr)
			if i+1 < len(command) && (command[i+1] == '|' || command[i+1] == '&') {
				i++ // skip next char
				continue
			}
			lastPipe = i
		}
	}
	return lastPipe
}

// execBash replaces the current process with bash -c "command".
func execBash(command string) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		fmt.Fprintf(os.Stderr, "trimout: bash not found: %v\n", err)
		os.Exit(1)
	}
	syscall.Exec(bash, []string{"bash", "-c", command}, os.Environ())
}

// runSelf re-invokes trimout with the given args (e.g., ["run", "--tail", "5", "--", "bash", "-c", "make"]).
func runSelf(args []string) {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "trimout: cannot find self: %v\n", err)
		os.Exit(1)
	}
	fullArgs := append([]string{"trimout"}, args...)
	syscall.Exec(self, fullArgs, os.Environ())
}
