#!/usr/bin/env bash
set -euo pipefail

TRIMOUT="./trimout"

# Build if needed
if [ ! -f "$TRIMOUT" ]; then
    go build -o trimout .
fi

# Approximate token count (~4 chars per token for English text)
tokens() {
    local bytes=$1
    echo $(( (bytes + 3) / 4 ))
}

# Print table header
printf "%-30s %8s %8s %8s %8s %7s\n" "SCENARIO" "RAW_LN" "FILT_LN" "RAW_TOK" "FILT_TOK" "SAVED"
printf "%-30s %8s %8s %8s %8s %7s\n" "--------" "------" "-------" "-------" "--------" "-----"

# Run a benchmark: name, filter_flags, input_command
bench() {
    local name="$1"
    local flags="$2"
    shift 2
    local input
    input=$("$@" 2>&1 || true)

    local raw_bytes raw_lines filt filt_bytes filt_lines raw_tok filt_tok saved
    raw_bytes=${#input}
    raw_lines=$(echo "$input" | wc -l)
    filt=$(echo "$input" | $TRIMOUT --no-cache $flags)
    filt_bytes=${#filt}
    filt_lines=$(echo "$filt" | wc -l)
    raw_tok=$(tokens "$raw_bytes")
    filt_tok=$(tokens "$filt_bytes")

    if [ "$raw_tok" -gt 0 ]; then
        saved=$(( 100 - (filt_tok * 100 / raw_tok) ))
    else
        saved=0
    fi

    printf "%-30s %8d %8d %8d %8d %6d%%\n" "$name" "$raw_lines" "$filt_lines" "$raw_tok" "$filt_tok" "$saved"
}

# --- Generate synthetic test data ---

# Simulated build log (~500 lines)
gen_build_log() {
    for i in $(seq 1 200); do
        echo "  Compiling module_${i} v0.1.0"
    done
    for i in $(seq 1 280); do
        echo "    Finished module_${i} in 0.${i}s"
    done
    echo ""
    echo "warning: unused variable 'x' in src/handler.go:42"
    echo "warning: deprecated function 'oldFunc' in src/legacy.go:15"
    echo ""
    echo "Build completed: 200 modules, 2 warnings, 0 errors"
    echo "Total time: 45.2s"
}

# Simulated test output (~300 lines, with failures)
gen_test_output() {
    for i in $(seq 1 290); do
        echo "ok  	github.com/example/pkg${i}	0.0${i}s"
    done
    echo "--- FAIL: TestUserAuth (0.05s)"
    echo "    auth_test.go:42: expected token 'abc', got 'def'"
    echo "--- FAIL: TestDBConnect (0.12s)"
    echo "    db_test.go:88: connection refused: localhost:5432"
    echo "FAIL	github.com/example/auth	0.15s"
    echo "FAIL	github.com/example/db	0.12s"
    echo ""
    echo "FAIL: 2 tests failed, 290 passed"
}

# Simulated verbose log with ANSI codes
gen_ansi_log() {
    for i in $(seq 1 150); do
        echo -e "\033[32m✓\033[0m Test case $i passed (\033[33m0.${i}s\033[0m)"
    done
    for i in $(seq 1 10); do
        echo -e "\033[31m✗\033[0m Test case $((150+i)) FAILED"
        echo -e "  \033[90mExpected: foo\033[0m"
        echo -e "  \033[90mGot:      bar\033[0m"
    done
    echo -e "\033[1m\033[31m10 failures\033[0m, 150 passed"
}

# Large repetitive output (like npm install)
gen_repetitive() {
    for i in $(seq 1 400); do
        echo "added package-name-${i}@1.2.${i}"
    done
    for pkg in lodash react express webpack babel; do
        echo "added ${pkg}@4.17.21"
        echo "added ${pkg}@4.17.21"
        echo "added ${pkg}@4.17.21"
    done
    echo ""
    echo "added 415 packages in 12.4s"
}

echo ""
echo "=== Real commands ==="
bench "ps aux"                    "--ends 10"                ps aux
bench "ls -la /usr/bin"           "--ends 10"                ls -la /usr/bin
bench "dpkg -l (all packages)"   "--ends 10"                dpkg -l
bench "env"                       "--ends 5"                 env

echo ""
echo "=== Synthetic: build log ==="
bench "raw (no filter)"           ""                         gen_build_log
bench "--ends 10"                 "--ends 10"                gen_build_log
bench "--ends 30"                 "--ends 30"                gen_build_log
bench "--ends 10 --strip-ansi"   "--ends 10 --strip-ansi"   gen_build_log
bench "--grep warning"            "--grep warning"           gen_build_log

echo ""
echo "=== Synthetic: test output ==="
bench "raw (no filter)"           ""                         gen_test_output
bench "--ends 10"                 "--ends 10"                gen_test_output
bench "--ends 30"                 "--ends 30"                gen_test_output
bench "--grep FAIL"               "--grep FAIL"              gen_test_output

echo ""
echo "=== Synthetic: ANSI log ==="
bench "raw (no filter)"           ""                         gen_ansi_log
bench "--ends 10"                 "--ends 10"                gen_ansi_log
bench "--ends 10 --strip-ansi"   "--ends 10 --strip-ansi"   gen_ansi_log

echo ""
echo "=== Synthetic: repetitive output ==="
bench "raw (no filter)"           ""                         gen_repetitive
bench "--ends 10"                 "--ends 10"                gen_repetitive
bench "--ends 10 --dedup"        "--ends 10 --dedup"         gen_repetitive
bench "--dedup"                   "--dedup"                  gen_repetitive

echo ""
echo "Note: Token count is approximate (~4 chars/token)."
echo "Actual LLM token counts vary by model and tokenizer."
