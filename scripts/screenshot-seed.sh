#!/usr/bin/env bash
# screenshot-seed.sh — creates the realistic demo dataset the README
# screenshots are captured from.
#
# Requires a running daemon configured with screenshots.yaml (started with
# `XLISTMAN_CONFIG=screenshots.yaml ./xlistman serve`, or your own script).
# Archive posts and the held messages are injected through the real mail
# pipeline via `xlistman deliver`, which relays to the daemon's pipe socket —
# the same path as LMTP mail.
#
# The domain, lists, subscribers, roles, archive, and moderation states are
# then captured as the five shots in docs/screenshots/ at 1280x900.
set -euo pipefail
cd "$(dirname "$0")/.."

export XLISTMAN_CONFIG="${XLISTMAN_CONFIG:-screenshots.yaml}"
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
	printf 'From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <%s@news.example.org>\r\n\r\n%s\r\n' \
		"$from" "$list" "$subject" "$date" "$msgid" "$body" \
		| run "$BIN" deliver "$list" >/dev/null
}

echo "== seeding domain and lists =="
run "$BIN" domain add news.example.org "Example News"
run "$BIN" list create dev@news.example.org --type discussion --owner sarah@example.com --desc "Developer discussion — build on the platform"
run "$BIN" list create announce@news.example.org --type newsletter --owner sarah@example.com --desc "Product announcements — releases and important news"
run "$BIN" list create help@news.example.org --type discussion --owner sarah@example.com --moderate --desc "Community help — moderated support list"

echo "== seeding subscribers and roles =="
run "$BIN" subscriber add dev@news.example.org priya@example.com
run "$BIN" subscriber add dev@news.example.org james@example.com
run "$BIN" subscriber add dev@news.example.org sarah@example.com
run "$BIN" subscriber add announce@news.example.org priya@example.com
run "$BIN" subscriber add announce@news.example.org sarah@example.com
run "$BIN" subscriber add help@news.example.org sarah@example.com
run "$BIN" subscriber add help@news.example.org tomas@example.com
run "$BIN" moderator add help@news.example.org tomas@example.com
run "$BIN" admin add admin@example.org
# Designated senders must be known Subscribers first (subscriber-first rule).
run "$BIN" subscriber add announce@news.example.org grace@example.com
run "$BIN" list add-sender announce@news.example.org grace@example.com

echo "== seeding the dev archive (via the mail pipeline) =="
deliver dev@news.example.org priya@example.com "Announcing the developer portal" "Hi everyone,

The new developer portal is live at portal.example.org — docs, API key management, and the changelog are now in one place. I'd love feedback on the getting-started guide before we link it from the homepage.

Priya"
deliver dev@news.example.org sarah@example.com "Re: Announcing the developer portal" "Thanks for pulling this together. One question: are the API v2 rate limits documented there? A few folks have asked about the 50/hour cap and whether it's per key or per app."
deliver dev@news.example.org james@example.com "Re: Announcing the developer portal" "Yes — I just added /docs/rate-limits, including a per-plan table. It's per API key, and the dashboard now shows a progress bar so you can see where you stand."
deliver dev@news.example.org sarah@example.com "Scheduled maintenance: Saturday 02:00–03:00 UTC" "Heads up: the API and webhook delivery will be unavailable this Saturday from 02:00 to 03:00 UTC for the storage upgrade. No deploys during the window. We'll post here when it's done."
deliver dev@news.example.org priya@example.com "Digest delivery — nothing since last week" "I switched my dev subscription to digest last week and haven't received one yet. I expected the daily digest around 09:00 UTC. Anyone else seeing this, or is it a known issue with the digest worker?"

echo "== seeding held messages on the moderated list =="
deliver help@news.example.org zoe@external.net "Webhook verification fails with 500" "Hi — I'm trying to verify my webhook endpoint for the new integration and I keep getting a 500 on the signature check. I followed the docs but the X-Signature header never matches. Has anyone hit this?"
deliver help@news.example.org priya@example.com "Plain-text-only option for digests?" "Is there a way to receive the newsletter as plain text only? I'd like to cut down on HTML in my inbox. Thanks!"

echo "== seeding store-only states (disabled member, held subscription) =="
go run ./cmd/e2eseed disabled dev@news.example.org nina@example.com
go run ./cmd/e2eseed held-sub help@news.example.org alex@example.com

echo "== seed complete =="
