package main

import (
	"testing"
)

func TestIsTrimoutCommand(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"trimout show last", true},
		{"trimout list", true},
		{"trimout session init", true},
		{"trimout clear --older-than 7d", true},
		{"/usr/local/bin/trimout show last", true},
		{"./trimout show last", true},
		{"make", false},
		{"bash -c 'trimout show last'", false},
		{"echo trimout", false},
		{"", false},
		{"TRIMOUT_CACHE_DIR=/tmp trimout list", false}, // env prefix — not first word
		{"grep trimout file.txt", false},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			if got := isTrimoutCommand(tt.cmd); got != tt.want {
				t.Errorf("isTrimoutCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestFindLastPipe(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want int
	}{
		{"simple pipe", "make | tail -5", 5},
		{"no pipe", "make", -1},
		{"double pipe (or)", "cmd1 || cmd2", -1},
		{"pipe then or", "a | b || c", 2},
		{"pipe in single quotes", "echo 'a | b'", -1},
		{"pipe in double quotes", `echo "a | b"`, -1},
		{"multiple pipes", "a | b | c", 6},
		{"pipe-and", "cmd1 |& cmd2", -1},
		{"escaped pipe", `echo hello \| world`, -1},
		{"pipe in subshell", "echo $(cmd1 | cmd2)", -1},
		{"pipe in backticks", "echo `cmd1 | cmd2`", -1},
		{"trailing pipe no rhs", "make |", 5},
		{"pipe with or after", "a | b || c | d", 11},
		{"only or operators", "cmd1 || cmd2 || cmd3", -1},
		{"mixed quotes and pipe", `echo "hello" | grep 'world'`, 13},
		{"empty string", "", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findLastPipe(tt.cmd)
			if got != tt.want {
				t.Errorf("findLastPipe(%q) = %d, want %d", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestParseTrailerFlags(t *testing.T) {
	tests := []struct {
		trailer string
		want    []string
	}{
		{"tail -50", []string{"--tail", "50"}},
		{"tail -n 50", []string{"--tail", "50"}},
		{"head -20", []string{"--head", "20"}},
		{"head -n 20", []string{"--head", "20"}},
		{"grep ERROR", []string{"--grep", "ERROR"}},
		{"grep 'FAIL|ERROR'", []string{"--grep", "FAIL|ERROR"}},
		{`grep "warning"`, []string{"--grep", "warning"}},
		{"sort", nil},
		{"wc -l", nil},
		{"tail", nil},
		{"head", nil},
		{"tail -f", nil},          // streaming tail, don't rewrite
		{"grep -i error", nil},    // flags we don't handle yet, fall through safely
		{"grep -v pattern", nil},  // same
		{"grep", nil},             // bare grep, no pattern
		{"tee output.log", nil},   // unrelated command
		{"less", nil},             // pager
		{"cat", nil},              // cat
		{"tail -n +5", nil},       // tail from line 5, not simple -N
	}
	for _, tt := range tests {
		t.Run(tt.trailer, func(t *testing.T) {
			got := parseTrailerFlags(tt.trailer)
			if tt.want == nil {
				if got != nil {
					t.Errorf("parseTrailerFlags(%q) = %v, want nil", tt.trailer, got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("parseTrailerFlags(%q) = %v, want %v", tt.trailer, got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("parseTrailerFlags(%q)[%d] = %q, want %q", tt.trailer, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestDetectTrailingFilter(t *testing.T) {
	tests := []struct {
		name    string
		cmd     string
		wantCmd string
		wantOk  bool
	}{
		{"tail", "make | tail -50", "make", true},
		{"head", "cargo build 2>&1 | head -20", "cargo build 2>&1", true},
		{"grep", "pytest | grep FAIL", "pytest", true},
		{"no pipe", "make", "", false},
		{"unknown trailer", "make | sort", "", false},
		{"pipe in quotes", "echo 'a | tail -5'", "", false},
		{"multi-pipe with tail", "cmd1 | cmd2 | tail -10", "cmd1 | cmd2", true},
		{"tail -f not rewritten", "make | tail -f", "", false},
		{"empty command", "", "", false},
		{"just a pipe", "|", "", false},
		{"pipe with empty rhs", "make | ", "", false},
		{"subshell pipe", "echo $(make | grep err) | tail -5", "echo $(make | grep err)", true},
		{"grep with flags falls through", "make | grep -i error", "", false},
		{"redirect before pipe", "make 2>&1 | tail -20", "make 2>&1", true},
		{"complex lhs", "cd /tmp && make -j4 | head -10", "cd /tmp && make -j4", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, _, ok := detectTrailingFilter(tt.cmd)
			if ok != tt.wantOk {
				t.Errorf("detectTrailingFilter(%q) ok = %v, want %v", tt.cmd, ok, tt.wantOk)
			}
			if ok && base != tt.wantCmd {
				t.Errorf("detectTrailingFilter(%q) base = %q, want %q", tt.cmd, base, tt.wantCmd)
			}
		})
	}
}
