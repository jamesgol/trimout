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
var shellDefaults = []string{"--ends", "30", "--strip-ansi"}

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

	// Try to detect and rewrite trailing pipe to tail/head/grep
	if baseCmd, flags, ok := detectTrailingFilter(command); ok {
		args := []string{"run"}
		args = append(args, flags...)
		args = append(args, "--", "bash", "-c", baseCmd)
		runSelf(args)
		return
	}

	// Default: wrap with sensible defaults
	args := []string{"run"}
	args = append(args, shellDefaults...)
	args = append(args, "--", "bash", "-c", command)
	runSelf(args)
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
	grepRe = regexp.MustCompile(`^grep\s+(.+)$`)
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
		pattern := strings.TrimSpace(m[1])
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

// findLastPipe returns the index of the last unquoted, non-|| pipe in command.
// Returns -1 if not found.
func findLastPipe(command string) int {
	lastPipe := -1
	inSingle := false
	inDouble := false
	escaped := false

	for i := 0; i < len(command); i++ {
		ch := command[i]

		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if inSingle || inDouble {
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
