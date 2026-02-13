package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

func main() {
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
		}
	}
	cmdPipe(os.Args[1:])
}

type filterOpts struct {
	dedup        bool
	dedupAll     bool
	head         int
	tail         int
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
}

func addFilterFlags(fs *flag.FlagSet, opts *filterOpts) {
	fs.BoolVar(&opts.dedup, "dedup", false, "Remove consecutive duplicate lines")
	fs.BoolVar(&opts.dedupAll, "dedup-all", false, "Remove all duplicate lines, keeping first")
	fs.IntVar(&opts.head, "head", 0, "Keep first N lines")
	fs.IntVar(&opts.tail, "tail", 0, "Keep last N lines")
	fs.IntVar(&opts.mid, "mid", 0, "Keep first N/2 and last N/2 lines")
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
	fs.StringVar(&opts.session, "session", "", "Session ID (default: auto-detect from .pipesum-session)")
}

// resolveSession returns the session ID from the flag or auto-detection.
func resolveSession(opts *filterOpts) string {
	if opts.session != "" {
		return opts.session
	}
	return detectSession()
}

func applyFilters(lines []string, opts *filterOpts) ([]string, error) {
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
	if opts.compressBlnk {
		lines = FilterCompressBlank(lines)
	}
	if opts.maxLineLen > 0 {
		lines = FilterMaxLineLen(lines, opts.maxLineLen)
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

func cmdPipe(args []string) {
	fs := flag.NewFlagSet("pipesum", flag.ExitOnError)
	var opts filterOpts
	addFilterFlags(fs, &opts)
	fs.Parse(args)

	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pipesum: error reading stdin: %v\n", err)
		os.Exit(1)
	}

	content := string(raw)
	// Remove trailing newline for splitting, we'll add it back on output
	content = strings.TrimSuffix(content, "\n")
	lines := strings.Split(content, "\n")

	originalLines := len(lines)
	originalBytes := len(raw)

	// Cache the raw output
	if !opts.noCache {
		meta := CacheMeta{Tag: opts.tag, ExitCode: -1, Session: resolveSession(&opts)}
		id, cacheErr := CacheWrite(raw, originalLines, meta)
		if cacheErr != nil {
			fmt.Fprintf(os.Stderr, "pipesum: cache warning: %v\n", cacheErr)
		} else if opts.verbose {
			fmt.Fprintf(os.Stderr, "pipesum: cached as %s (%d lines, %d bytes)\n", id, originalLines, originalBytes)
		}
	}

	filtered, err := applyFilters(lines, &opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pipesum: %v\n", err)
		os.Exit(1)
	}

	output := strings.Join(filtered, "\n") + "\n"

	if opts.stats {
		statsLine := StatsLine(originalLines, originalBytes, len(filtered), len(output))
		fmt.Println(statsLine)
	}

	fmt.Print(output)
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
		fmt.Fprintf(os.Stderr, "pipesum run: missing command after --\n")
		fmt.Fprintf(os.Stderr, "usage: pipesum run [OPTIONS] -- COMMAND [ARGS...]\n")
		os.Exit(1)
	}

	fs := flag.NewFlagSet("pipesum run", flag.ExitOnError)
	var opts filterOpts
	addFilterFlags(fs, &opts)
	fs.Parse(filterArgs)

	workDir, _ := os.Getwd()

	start := time.Now()
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Stdin = os.Stdin
	raw, err := cmd.CombinedOutput()
	duration := time.Since(start)

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			fmt.Fprintf(os.Stderr, "pipesum run: %v\n", err)
			os.Exit(1)
		}
	}

	content := string(raw)
	content = strings.TrimSuffix(content, "\n")
	lines := strings.Split(content, "\n")

	originalLines := len(lines)
	originalBytes := len(raw)

	// Cache with full metadata
	if !opts.noCache {
		meta := CacheMeta{
			Command:  strings.Join(cmdArgs, " "),
			Tag:      opts.tag,
			ExitCode: exitCode,
			Duration: duration,
			WorkDir:  workDir,
			Session:  resolveSession(&opts),
		}
		id, cacheErr := CacheWrite(raw, originalLines, meta)
		if cacheErr != nil {
			fmt.Fprintf(os.Stderr, "pipesum: cache warning: %v\n", cacheErr)
		} else if opts.verbose {
			fmt.Fprintf(os.Stderr, "pipesum: cached as %s (%d lines, %d bytes)\n", id, originalLines, originalBytes)
		}
	}

	filtered, ferr := applyFilters(lines, &opts)
	if ferr != nil {
		fmt.Fprintf(os.Stderr, "pipesum: %v\n", ferr)
		os.Exit(1)
	}

	output := strings.Join(filtered, "\n") + "\n"

	if opts.stats {
		statsLine := StatsLine(originalLines, originalBytes, len(filtered), len(output))
		fmt.Println(statsLine)
	}

	fmt.Print(output)
	os.Exit(exitCode)
}

func cmdShow(args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "pipesum show: missing ID (use 'last' for most recent)\n")
		os.Exit(1)
	}

	id := args[0]
	remainingArgs := args[1:]

	fs := flag.NewFlagSet("pipesum show", flag.ExitOnError)
	var opts filterOpts
	addFilterFlags(fs, &opts)
	fs.Parse(remainingArgs)

	raw, err := CacheRead(id, resolveSession(&opts))
	if err != nil {
		fmt.Fprintf(os.Stderr, "pipesum show: %v\n", err)
		os.Exit(1)
	}

	content := strings.TrimSuffix(string(raw), "\n")
	lines := strings.Split(content, "\n")

	originalLines := len(lines)
	originalBytes := len(raw)

	filtered, err := applyFilters(lines, &opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pipesum show: %v\n", err)
		os.Exit(1)
	}

	output := strings.Join(filtered, "\n") + "\n"

	if opts.stats {
		statsLine := StatsLine(originalLines, originalBytes, len(filtered), len(output))
		fmt.Println(statsLine)
	}

	fmt.Print(output)
}

func cmdList(args []string) {
	fs := flag.NewFlagSet("pipesum list", flag.ExitOnError)
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
		fmt.Fprintf(os.Stderr, "pipesum list: %v\n", err)
		os.Exit(1)
	}

	if len(entries) == 0 {
		fmt.Println("No cached entries.")
		return
	}

	fmt.Printf("%-32s %5s %6s %8s %s\n", "ID", "EXIT", "LINES", "DURATION", "COMMAND")
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
		fmt.Printf("%-32s %5s %6d %8s %s\n", e.ID, exit, e.LineCount, dur, cmd)
	}
}

func cmdSession(args []string) {
	if len(args) > 0 && args[0] == "init" {
		id, err := initSession()
		if err != nil {
			fmt.Fprintf(os.Stderr, "pipesum session: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(id)
		return
	}

	// Default: print current session
	id := detectSession()
	if id == "" {
		fmt.Println("No active session. Run 'pipesum session init' to create one.")
		os.Exit(1)
	}
	fmt.Println(id)
}

func cmdClear(args []string) {
	fs := flag.NewFlagSet("pipesum clear", flag.ExitOnError)
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
				fmt.Fprintf(os.Stderr, "pipesum clear: invalid duration %q: %v\n", *olderThan, err)
				os.Exit(1)
			}
		}
	}

	n, err := CacheClear(d)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pipesum clear: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Cleared %d entries.\n", n)
}
