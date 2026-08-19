#!/usr/bin/env bash
# e2e-seed.sh — creates the fixture data for the frontend test suite.
#
# Requires a running e2e daemon (started by scripts/e2e.sh setup): archive
# posts and the held message are injected through the real mail pipeline via
# `xlistman deliver`, which relays to the daemon's pipe-mode socket.
set -euo pipefail
cd "$(dirname "$0")/.."

export XLISTMAN_CONFIG="${XLISTMAN_CONFIG:-e2e.yaml}"
BIN="$(pwd)/xlistman"

run() {
	echo "  > $*" >&2
	"$@"
}

# deliver <list> <from> <subject> <body> — inject a real post so it flows
# through the same pipeline as LMTP mail (archiving, posting policy, held
# queue, outbound notices).
deliver() {
	local list="$1" from="$2" subject="$3" body="$4"
	local date msgid
	date="$(date -u +'%a, %d %b %Y %H:%M:%S +0000')"
	msgid="$(date +%s%N)"
	printf 'From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <%s@lists.test>\r\n\r\n%s\r\n' \
		"$from" "$list" "$subject" "$date" "$msgid" "$body" \
		| run "$BIN" deliver "$list" >/dev/null
}

echo "== seeding domain and lists =="
run "$BIN" domain add lists.test "e2e test domain"
run "$BIN" list create dev@lists.test --type discussion --owner owner@lists.test --desc "Development discussion list"
run "$BIN" list create mod@lists.test --type discussion --owner owner@lists.test --moderate --desc "Moderated discussion list"
run "$BIN" list create announce@lists.test --type newsletter --owner owner@lists.test --desc "Announcements only"

echo "== seeding subscribers and roles =="
run "$BIN" subscriber add dev@lists.test member@lists.test
run "$BIN" subscriber add mod@lists.test member@lists.test
run "$BIN" subscriber add dev@lists.test owner@lists.test
run "$BIN" subscriber add mod@lists.test owner@lists.test
run "$BIN" moderator add mod@lists.test moderator@lists.test
run "$BIN" admin add admin@lists.test
# Designated senders must be known Subscribers first (subscriber-first rule);
# give sender@ a membership on dev so it exists before the allowlist grant.
run "$BIN" subscriber add dev@lists.test sender@lists.test
run "$BIN" list add-sender announce@lists.test sender@lists.test

echo "== seeding archive posts (via the mail pipeline) =="
deliver dev@lists.test member@lists.test "Welcome to the dev list" "Hello everyone, this is the first post to the dev list. We are planning the spring launch."
deliver dev@lists.test owner@lists.test "Re: Welcome to the dev list" "Thanks for the welcome. The apricot release is on track for next week."
deliver dev@lists.test member@lists.test "Maintenance window this weekend" "Heads up: the database will be unavailable on Saturday for an upgrade."

echo "== seeding a held message (non-subscriber to a moderated list) =="
deliver mod@lists.test stranger@lists.test "Please review my proposal" "Hello, I would like to discuss my proposal for the new feature. Please take a look."

echo "== seeding store-only states (disabled member, held subscription) =="
go run ./cmd/e2eseed

echo "== seed complete =="
