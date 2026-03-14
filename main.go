package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

func main() {
	// Handle shell-style invocations: -c, -ic, -lc, etc.
	// Shells are invoked with -c "command" but tools may combine flags
	// (e.g., bash -ic "command", bash -lc "command").
	if cmd, ok := parseShellArgs(os.Args[1:]); ok {
		runShell(cmd)
		return
	}

	// Handle login/interactive flags without -c (shell probe)
	if len(os.Args) > 1 && isShellLoginFlag(os.Args[1]) {
		return
	}

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "run":
			cmdRun(os.Args[2:])
			return
		case "show":
			cmdShow(os.Args[2:])
			return
		case "list":
			cmdList(os.Args[2:])
			return
		case "clear":
			cmdClear(os.Args[2:])
			return
		case "session":
			cmdSession(os.Args[2:])
			return
		case "-h", "--help", "help":
			printHelp()
			return
		}
	}

	// No args and stdin is a terminal — not pipe mode, just exit
	if len(os.Args) <= 1 {
		fi, _ := os.Stdin.Stat()
		if fi != nil && fi.Mode()&os.ModeCharDevice != 0 {
			fmt.Fprintf(os.Stderr, "trimout: no input. Run 'trimout --help' for usage.\n")
			os.Exit(0)
		}
	}

	cmdPipe(os.Args[1:])
}

type filterOpts struct {
	dedup        bool
	dedupAll     bool
	head         int
	tail         int
	ends         int
	mid          int
	grep         string
	grepV        string
	stripAnsi    bool
	compressBlnk bool
	maxLineLen   int
	stats        bool
	tag          string
	noCache      bool
	verbose      bool
	session      string
	showStderr   bool
	showCombined bool
	quiet        bool
	noHint       bool
	inputJSONL  string
	inputText   string
	outputJSONL string
}

func addFilterFlags(fs *flag.FlagSet, opts *filterOpts) {
	fs.BoolVar(&opts.dedup, "dedup", false, "Remove consecutive duplicate lines")
	fs.BoolVar(&opts.dedupAll, "dedup-all", false, "Remove all duplicate lines, keeping first")
	fs.IntVar(&opts.head, "head", 0, "Keep first N lines")
	fs.IntVar(&opts.tail, "tail", 0, "Keep last N lines")
	fs.IntVar(&opts.ends, "ends", 0, "Keep first N and last N lines")
	fs.IntVar(&opts.mid, "mid", 0, "Drop first N and last N lines, keep the middle")
	fs.StringVar(&opts.grep, "grep", "", "Keep lines matching pattern")
	fs.StringVar(&opts.grepV, "grep-v", "", "Remove lines matching pattern")
	fs.BoolVar(&opts.stripAnsi, "strip-ansi", false, "Remove ANSI escape codes")
	fs.BoolVar(&opts.compressBlnk, "compress-blank", false, "Collapse multiple blank lines")
	fs.IntVar(&opts.maxLineLen, "max-line-len", 0, "Truncate lines longer than N chars")
	fs.BoolVar(&opts.stats, "stats", false, "Prepend summary line")
	fs.StringVar(&opts.tag, "tag", "", "Tag this capture")
	fs.StringVar(&opts.tag, "t", "", "Tag this capture (shorthand)")
	fs.BoolVar(&opts.noCache, "no-cache", false, "Skip caching the output")
	fs.BoolVar(&opts.verbose, "verbose", false, "Print cache ID to stderr")
	fs.BoolVar(&opts.verbose, "v", false, "Print cache ID to stderr (shorthand)")
	fs.StringVar(&opts.session, "session", "", "Session ID (default: auto-detect from .trimout-session)")
	fs.BoolVar(&opts.showStderr, "stderr", false, "Show stderr instead of stdout")
	fs.BoolVar(&opts.showCombined, "combined", false, "Show both stdout and stderr")
	fs.BoolVar(&opts.quiet, "quiet", false, "Suppress output (run mode: cache only, check exit code)")
	fs.BoolVar(&opts.quiet, "q", false, "Suppress output (shorthand)")
	fs.BoolVar(&opts.noHint, "no-hint", false, "Suppress the stderr hint when output is filtered")
	fs.StringVar(&opts.inputJSONL, "input-jsonl", "", "JSONL pattern file to match against")
	fs.StringVar(&opts.inputText, "input-text", "", "Plain text pattern file (one literal per line)")
	fs.StringVar(&opts.outputJSONL, "output-jsonl", "", "Write pattern matches as JSONL to file (use - for stdout)")
}

// resolveSession returns the session ID from the flag or auto-detection.
func resolveSession(opts *filterOpts) string {
	if opts.session != "" {
		return opts.session
	}
	return detectSession()
}

// resolveStream returns the stream filter byte based on flags and exit code.
func resolveStream(opts *filterOpts, exitCode int) byte {
	if opts.showCombined {
		return 0
	}
	if opts.showStderr {
		return streamErr
	}
	// Auto-include stderr on failure
	if exitCode > 0 {
		return 0
	}
	return streamOut
}

func loadPatterns(opts *filterOpts) ([]CompiledPattern, error) {
	if opts.inputJSONL != "" {
		return LoadPatternsJSONL(opts.inputJSONL)
	}
	if opts.inputText != "" {
		return LoadPatternsText(opts.inputText)
	}
	return nil, nil
}

func applyFilters(lines []string, opts *filterOpts, compiled []CompiledPattern) ([]string, error) {
	if opts.stripAnsi {
		lines = FilterStripAnsi(lines)
	}
	if opts.dedup {
		lines = FilterDedup(lines)
	}
	if opts.dedupAll {
		lines = FilterDedupAll(lines)
	}
	if opts.grep != "" {
		var err error
		lines, err = FilterGrep(lines, opts.grep)
		if err != nil {
			return nil, err
		}
	}
	if opts.grepV != "" {
		var err error
		lines, err = FilterGrepV(lines, opts.grepV)
		if err != nil {
			return nil, err
		}
	}
	if compiled != nil {
		lines = FilterPatterns(lines, compiled)
	}
	if opts.compressBlnk {
		lines = FilterCompressBlank(lines)
	}
	if opts.maxLineLen > 0 {
		lines = FilterMaxLineLen(lines, opts.maxLineLen)
	}
	if opts.ends > 0 {
		lines = FilterEnds(lines, opts.ends)
	}
	if opts.mid > 0 {
		lines = FilterMid(lines, opts.mid)
	}
	if opts.head > 0 {
		lines = FilterHead(lines, opts.head)
	}
	if opts.tail > 0 {
		lines = FilterTail(lines, opts.tail)
	}
	return lines, nil
}

func outputFiltered(lines []string, opts *filterOpts, originalLines, originalBytes int, cacheID string) {
	compiled, err := loadPatterns(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "trimout: %v\n", err)
		os.Exit(1)
	}

	if opts.outputJSONL != "" && compiled != nil {
		matches := MatchAll(lines, compiled)
		var w io.Writer
		if opts.outputJSONL == "-" {
			w = os.Stdout
		} else {
			f, ferr := os.Create(opts.outputJSONL)
			if ferr != nil {
				fmt.Fprintf(os.Stderr, "trimout: %v\n", ferr)
				os.Exit(1)
			}
			defer f.Close()
			w = f
		}
		enc := json.NewEncoder(w)
		for _, m := range matches {
			enc.Encode(m)
		}
		if opts.outputJSONL == "-" {
			return
		}
	}

	filtered, err := applyFilters(lines, opts, compiled)
	if err != nil {
		fmt.Fprintf(os.Stderr, "trimout: %v\n", err)
		os.Exit(1)
	}

	output := strings.Join(filtered, "\n") + "\n"

	if opts.stats {
		statsLine := StatsLine(originalLines, originalBytes, len(filtered), len(output))
		fmt.Println(statsLine)
	}

	fmt.Print(output)

	if !opts.noHint && len(filtered) < originalLines {
		hint := fmt.Sprintf("[trimout] %d of %d lines shown.", len(filtered), originalLines)
		if cacheID != "" {
			hint += fmt.Sprintf(" Full output: trimout show %s", cacheID)
		}
		fmt.Fprintln(os.Stderr, hint)
	}
}

// canStream returns true when all active filters can operate line-by-line.
func canStream(opts *filterOpts) bool {
	if opts.tail > 0 || opts.ends > 0 || opts.mid > 0 {
		return false
	}
	if opts.dedupAll || opts.stats {
		return false
	}
	if opts.outputJSONL != "" {
		return false
	}
	return true
}

func cmdPipe(args []string) {
	fs := flag.NewFlagSet("trimout", flag.ExitOnError)
	var opts filterOpts
	addFilterFlags(fs, &opts)
	fs.Parse(args)

	if canStream(&opts) {
		cmdPipeStreaming(&opts)
	} else {
		cmdPipeBuffered(&opts)
	}
}

func cmdPipeBuffered(opts *filterOpts) {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "trimout: error reading stdin: %v\n", err)
		os.Exit(1)
	}

	stdoutLines := countLines(raw)
	annotated := EncodeAnnotated(raw, nil)

	// Cache
	var cacheID string
	if !opts.noCache {
		meta := CacheMeta{Tag: opts.tag, ExitCode: -1, Session: resolveSession(opts)}
		id, cacheErr := CacheWrite(annotated, len(raw), 0, stdoutLines, 0, meta)
		if cacheErr != nil {
			fmt.Fprintf(os.Stderr, "trimout: cache warning: %v\n", cacheErr)
		} else {
			cacheID = id
			if opts.verbose {
				fmt.Fprintf(os.Stderr, "trimout: cached as %s (%d lines, %d bytes)\n", id, stdoutLines, len(raw))
			}
		}
	}

	lines := splitLines(raw)
	outputFiltered(lines, opts, stdoutLines, len(raw), cacheID)
}

func cmdPipeStreaming(opts *filterOpts) {
	compiled, err := loadPatterns(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "trimout: %v\n", err)
		os.Exit(1)
	}

	filters, err := buildStreamFilters(opts, compiled)
	if err != nil {
		fmt.Fprintf(os.Stderr, "trimout: %v\n", err)
		os.Exit(1)
	}

	// Start incremental cache
	var cw *CacheWriter
	if !opts.noCache {
		meta := CacheMeta{Tag: opts.tag, ExitCode: -1, Session: resolveSession(opts)}
		cw, err = CacheBegin(meta)
		if err != nil {
			fmt.Fprintf(os.Stderr, "trimout: cache warning: %v\n", err)
		}
	}

	// On signal, finalize cache before exiting
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		if cw != nil {
			cw.Finalize()
		}
		os.Exit(130)
	}()

	writer := bufio.NewWriter(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	displayedLines := 0
	for scanner.Scan() {
		line := scanner.Text()

		// Write unfiltered line to cache
		if cw != nil {
			cw.WriteLine(line)
		}

		if !opts.quiet {
			out, emit, done := runStreamFilters(filters, line)
			if emit {
				writer.WriteString(out)
				writer.WriteByte('\n')
				displayedLines++
			}
			if done {
				break
			}
		}
	}

	writer.Flush()

	// If --head stopped us early, drain remaining stdin into cache
	if cw != nil {
		for scanner.Scan() {
			cw.WriteLine(scanner.Text())
		}
	}

	// Finalize cache
	if cw != nil {
		id, cacheErr := cw.Finalize()
		if cacheErr != nil {
			fmt.Fprintf(os.Stderr, "trimout: cache warning: %v\n", cacheErr)
		} else {
			if opts.verbose {
				fmt.Fprintf(os.Stderr, "trimout: cached as %s (%d lines, %d bytes)\n",
					id, cw.StdoutLines(), cw.StdoutBytes())
			}
			if !opts.noHint && displayedLines < cw.StdoutLines() {
				fmt.Fprintf(os.Stderr, "[trimout] %d of %d lines shown. Full output: trimout show %s\n",
					displayedLines, cw.StdoutLines(), id)
			}
		}
	}
}

func cmdRun(args []string) {
	// Split args at "--" into filter flags and command
	var filterArgs, cmdArgs []string
	for i, a := range args {
		if a == "--" {
			filterArgs = args[:i]
			cmdArgs = args[i+1:]
			break
		}
	}
	if cmdArgs == nil {
		fmt.Fprintf(os.Stderr, "trimout run: missing command after --\n")
		fmt.Fprintf(os.Stderr, "usage: trimout run [OPTIONS] -- COMMAND [ARGS...]\n")
		os.Exit(1)
	}

	fs := flag.NewFlagSet("trimout run", flag.ExitOnError)
	var opts filterOpts
	addFilterFlags(fs, &opts)
	fs.Parse(filterArgs)

	workDir, _ := os.Getwd()

	// Set up the command with separate stdout/stderr pipes
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Stdin = os.Stdin

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "trimout run: %v\n", err)
		os.Exit(1)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "trimout run: %v\n", err)
		os.Exit(1)
	}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "trimout run: %v\n", err)
		os.Exit(1)
	}

	// Read both streams concurrently, capturing all output
	var mu sync.Mutex
	var annotatedLines []AnnotatedLine
	var stdoutBuf, stderrBuf []byte

	var wg sync.WaitGroup
	wg.Add(2)

	readStream := func(r io.Reader, stream byte, dest *[]byte) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			*dest = append(*dest, line...)
			*dest = append(*dest, '\n')
			mu.Lock()
			annotatedLines = append(annotatedLines, AnnotatedLine{Stream: stream, Text: line})
			mu.Unlock()
		}
	}

	go readStream(stdoutPipe, streamOut, &stdoutBuf)
	go readStream(stderrPipe, streamErr, &stderrBuf)

	wg.Wait()
	cmdErr := cmd.Wait()
	duration := time.Since(start)

	exitCode := 0
	if cmdErr != nil {
		if exitErr, ok := cmdErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			fmt.Fprintf(os.Stderr, "trimout run: %v\n", cmdErr)
			os.Exit(1)
		}
	}

	stdoutLineCount := countLines(stdoutBuf)
	stderrLineCount := countLines(stderrBuf)

	// Cache with full metadata
	var cacheID string
	if !opts.noCache {
		annotated := EncodeAnnotatedLines(annotatedLines)
		meta := CacheMeta{
			Command:  strings.Join(cmdArgs, " "),
			Tag:      opts.tag,
			ExitCode: exitCode,
			Duration: duration,
			WorkDir:  workDir,
			Session:  resolveSession(&opts),
		}
		id, cacheErr := CacheWrite(annotated, len(stdoutBuf), len(stderrBuf), stdoutLineCount, stderrLineCount, meta)
		if cacheErr != nil {
			fmt.Fprintf(os.Stderr, "trimout: cache warning: %v\n", cacheErr)
		} else {
			cacheID = id
			if opts.verbose {
				fmt.Fprintf(os.Stderr, "trimout: cached as %s (stdout: %d lines, stderr: %d lines)\n", id, stdoutLineCount, stderrLineCount)
			}
		}
	}

	// Output filtered results (unless --quiet)
	if !opts.quiet {
		stream := resolveStream(&opts, exitCode)
		var lines []string
		switch stream {
		case streamOut:
			lines = splitLines(stdoutBuf)
		case streamErr:
			lines = splitLines(stderrBuf)
		default: // combined
			lines = DecodeAnnotated(EncodeAnnotatedLines(annotatedLines), 0)
		}

		totalLines := len(lines)
		totalBytes := 0
		for _, l := range lines {
			totalBytes += len(l) + 1
		}
		outputFiltered(lines, &opts, totalLines, totalBytes, cacheID)
	}

	os.Exit(exitCode)
}

func cmdShow(args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "trimout show: missing ID (use 'last' for most recent)\n")
		os.Exit(1)
	}

	id := args[0]
	remainingArgs := args[1:]

	fs := flag.NewFlagSet("trimout show", flag.ExitOnError)
	var opts filterOpts
	addFilterFlags(fs, &opts)
	fs.Parse(remainingArgs)

	session := resolveSession(&opts)

	// Get entry metadata to know exit code for auto-stderr
	entry, entryErr := CacheGetEntry(id, session)
	exitCode := -1
	if entryErr == nil {
		exitCode = entry.ExitCode
	}

	raw, err := CacheReadLog(id, session)
	if err != nil {
		fmt.Fprintf(os.Stderr, "trimout show: %v\n", err)
		os.Exit(1)
	}

	// Use resolved ID for the hint (entry.ID if available, otherwise the input)
	showID := id
	if entryErr == nil {
		showID = entry.ID
	}

	stream := resolveStream(&opts, exitCode)
	lines := DecodeAnnotated(raw, stream)

	originalLines := len(lines)
	originalBytes := 0
	for _, l := range lines {
		originalBytes += len(l) + 1
	}

	outputFiltered(lines, &opts, originalLines, originalBytes, showID)
}

func cmdList(args []string) {
	fs := flag.NewFlagSet("trimout list", flag.ExitOnError)
	last := fs.Int("last", 20, "Number of entries to show")
	allSessions := fs.Bool("all", false, "Show entries from all sessions")
	session := fs.String("session", "", "Filter to specific session")
	fs.Parse(args)

	sess := *session
	if sess == "" && !*allSessions {
		sess = detectSession()
	}

	entries, err := CacheList(*last, sess)
	if err != nil {
		fmt.Fprintf(os.Stderr, "trimout list: %v\n", err)
		os.Exit(1)
	}

	if len(entries) == 0 {
		fmt.Println("No cached entries.")
		return
	}

	fmt.Printf("%-32s %5s %6s %6s %8s %s\n", "ID", "EXIT", "OUT", "ERR", "DURATION", "COMMAND")
	for _, e := range entries {
		exit := "-"
		if e.ExitCode >= 0 {
			exit = fmt.Sprintf("%d", e.ExitCode)
		}
		dur := "-"
		if e.Duration > 0 {
			dur = e.Duration.Truncate(time.Millisecond).String()
		}
		cmd := e.Command
		if cmd == "" {
			cmd = "(pipe)"
		}
		if len(cmd) > 40 {
			cmd = cmd[:37] + "..."
		}
		fmt.Printf("%-32s %5s %6d %6d %8s %s\n", e.ID, exit, e.StdoutLines, e.StderrLines, dur, cmd)
	}
}

func cmdSession(args []string) {
	if len(args) > 0 && args[0] == "init" {
		id, err := initSession()
		if err != nil {
			fmt.Fprintf(os.Stderr, "trimout session: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(id)
		return
	}

	// Default: print current session
	id := detectSession()
	if id == "" {
		fmt.Println("No active session. Run 'trimout session init' to create one.")
		os.Exit(1)
	}
	fmt.Println(id)
}

func cmdClear(args []string) {
	fs := flag.NewFlagSet("trimout clear", flag.ExitOnError)
	olderThan := fs.String("older-than", "", "Remove entries older than duration (e.g. 24h, 7d)")
	fs.Parse(args)

	var d time.Duration
	if *olderThan != "" {
		s := *olderThan
		// Support "d" suffix for days
		if strings.HasSuffix(s, "d") {
			s = strings.TrimSuffix(s, "d")
			var days int
			fmt.Sscanf(s, "%d", &days)
			d = time.Duration(days) * 24 * time.Hour
		} else {
			var err error
			d, err = time.ParseDuration(s)
			if err != nil {
				fmt.Fprintf(os.Stderr, "trimout clear: invalid duration %q: %v\n", *olderThan, err)
				os.Exit(1)
			}
		}
	}

	n, err := CacheClear(d)
	if err != nil {
		fmt.Fprintf(os.Stderr, "trimout clear: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Cleared %d entries.\n", n)
}

func printHelp() {
	fmt.Print(`trimout — capture and cache stdout/stderr with smart filtering

Usage:
  trimout run [OPTIONS] -- COMMAND [ARGS...]   Run a command with caching
  command | trimout [OPTIONS]                  Filter piped input
  trimout show <id|last|tag> [OPTIONS]         Re-query cached output
  trimout list [--last N] [--all]              List cached entries
  trimout session init                         Create .trimout-session in cwd
  trimout clear [--older-than DURATION]        Clear cache entries
  trimout -c "command"                         Shell mode (for CLAUDE_CODE_SHELL)

Filter options:
  --ends N            Keep first N and last N lines
  --mid N             Drop first N and last N lines, keep middle
  --head N            Keep first N lines
  --tail N            Keep last N lines
  --grep PATTERN      Keep lines matching pattern
  --grep-v PATTERN    Remove lines matching pattern
  --dedup             Remove consecutive duplicate lines
  --dedup-all         Remove all duplicate lines
  --strip-ansi        Remove ANSI escape codes
  --compress-blank    Collapse multiple blank lines
  --max-line-len N    Truncate lines longer than N chars
  --stats             Prepend a summary line

Pattern matching:
  --input-jsonl FILE  JSONL pattern file to match against
  --input-text FILE   Plain text pattern file (one literal per line)
  --output-jsonl FILE Write matches as JSONL (use - for stdout)

Other options:
  -t, --tag TAG       Tag this capture for later retrieval
  --no-cache          Skip caching the output
  --no-hint           Suppress the stderr hint when output is filtered
  -v, --verbose       Print cache ID to stderr
  --stderr            Show stderr instead of stdout
  --combined          Show both stdout and stderr
  -q, --quiet         Suppress output
  --session ID        Override session ID

Shell mode:
  Set CLAUDE_CODE_SHELL=/path/to/trimout to use as your shell.
  All commands are cached automatically. Trailing | tail/head/grep
  are rewritten to native trimout flags.
`)
}
