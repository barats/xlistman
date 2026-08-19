#!/usr/bin/env bash
# e2e.sh — bootstrap runner for the xListman frontend test suite.
#
#   ./scripts/e2e.sh setup    build, fresh DB, start daemon, seed, print agent prompt
#   ./scripts/e2e.sh stop     stop the e2e daemon
#   ./scripts/e2e.sh status   show whether the e2e daemon is up
#   ./scripts/e2e.sh summary  parse web/tests/report.md into PASS/FAIL totals
#
# The scenarios in web/tests/*.md are executed by an agent against Chrome via
# the chrome-devtools MCP server (a session always has it connected per
# ~/.grok/config.toml). The agent writes web/tests/report.md following the
# format in web/tests/README.md; `summary` turns that report into an exit
# code, so a script (or future CI) can gate on the suite result.
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT="$(pwd)"

export XLISTMAN_CONFIG="$ROOT/e2e.yaml"
BIN="$ROOT/xlistman"
DB=/tmp/xlistman-e2e.db
SINK=/tmp/xlistman-e2e-mail
SOCK=/tmp/xlistman-e2e.sock
PIDFILE=/tmp/xlistman-e2e.pid
LOG=/tmp/xlistman-e2e.log
BASE_URL=http://localhost:8090

cmd="${1:-setup}"

is_running() {
	[[ -S "$SOCK" ]] && return 0
	if [[ -f "$PIDFILE" ]] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
		return 0
	fi
	return 1
}

setup() {
	echo "== build =="
	go build -o "$BIN" .

	echo "== fresh database and sink =="
	rm -f "$DB" "$DB-wal" "$DB-shm"
	rm -rf "$SINK"
	mkdir -p "$SINK"

	echo "== start daemon (http $BASE_URL, socket $SOCK) =="
	if is_running; then
		echo "e2e daemon appears to already be running; run 'scripts/e2e.sh stop' first." >&2
		exit 1
	fi
	rm -f "$LOG"
	nohup "$BIN" serve >"$LOG" 2>&1 &
	echo $! >"$PIDFILE"

	for _ in $(seq 1 30); do
		[[ -S "$SOCK" ]] && break
		sleep 0.5
	done
	if [[ ! -S "$SOCK" ]]; then
		echo "daemon did not start; tail of $LOG:" >&2
		tail -n 20 "$LOG" >&2 || true
		exit 1
	fi

	echo "== seed =="
	"$ROOT/scripts/e2e-seed.sh"

	curl -fsS "$BASE_URL/health" >/dev/null || {
		echo "health check failed" >&2
		exit 1
	}

	echo
	echo "== e2e environment ready =="
	echo "Base URL:  $BASE_URL"
	echo "Config:    e2e.yaml (DB $DB, sink $SINK)"
	echo "Scenarios: $(ls web/tests/t*.md 2>/dev/null | wc -l | tr -d ' ') test files in web/tests/"
	echo
	echo "Next: tell the agent to run the suite, e.g.:"
	echo '  "Run the e2e suite: execute web/tests/*.md in order against'
	echo "   $BASE_URL and write the report to web/tests/report.md."
	echo
	echo "Then: ./scripts/e2e.sh summary  (exit code reflects the suite result)"
}

stop() {
	if [[ -f "$PIDFILE" ]] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
		kill "$(cat "$PIDFILE")" 2>/dev/null || true
		rm -f "$PIDFILE"
		echo "stopped e2e daemon (pid from $PIDFILE)"
	else
		echo "no e2e daemon running"
	fi
}

status() {
	if is_running; then
		echo "e2e daemon is running (http $BASE_URL)"
		exit 0
	fi
	echo "e2e daemon is not running"
	exit 1
}

summary() {
	local report="$ROOT/web/tests/report.md"
	if [[ ! -f "$report" ]]; then
		echo "no report at $report — run the suite first." >&2
		exit 1
	fi
	local steps_pass steps_fail tests_pass tests_fail
	steps_pass="$(grep -c '^\- \[PASS\]' "$report" || true)"
	steps_fail="$(grep -c '^\- \[FAIL\]' "$report" || true)"
	tests_pass="$(grep -c '^## .* — PASS$' "$report" || true)"
	tests_fail="$(grep -c '^## .* — FAIL$' "$report" || true)"
	echo "report:  $report"
	echo "tests:   $tests_pass passed, $tests_fail failed"
	echo "steps:   $steps_pass passed, $steps_fail failed"
	if [[ "$steps_fail" -gt 0 || "$tests_fail" -gt 0 ]]; then
		echo "suite result: FAIL"
		exit 1
	fi
	echo "suite result: PASS"
	exit 0
}

case "$cmd" in
setup)
	setup
	;;
stop)
	stop
	;;
status)
	status
	;;
summary)
	summary
	;;
*)
	echo "usage: $0 <setup|stop|status|summary>" >&2
	exit 2
	;;
esac
