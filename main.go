package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "show":
			cmdShow(os.Args[2:])
			return
		case "list":
			cmdList(os.Args[2:])
			return
		case "clear":
			cmdClear(os.Args[2:])
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
		id, cacheErr := CacheWrite(raw, originalLines, "", opts.tag)
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

	raw, err := CacheRead(id)
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
	fs.Parse(args)

	entries, err := CacheList(*last)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pipesum list: %v\n", err)
		os.Exit(1)
	}

	if len(entries) == 0 {
		fmt.Println("No cached entries.")
		return
	}

	fmt.Printf("%-28s %8s %6s %s\n", "ID", "SIZE", "LINES", "TAGS")
	for _, e := range entries {
		fmt.Printf("%-28s %8d %6d %s\n", e.ID, e.Size, e.LineCount, e.Tags)
	}
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
