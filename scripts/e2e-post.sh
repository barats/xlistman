#!/usr/bin/env bash
# e2e-post.sh <list> <from> <subject> <body>
#
# Injects a real post through the pipe-mode socket so it flows through the
# same pipeline as LMTP mail (posting policy, archive, held queue, outbound
# notices). Used by test Setup blocks to mint throwaway held messages / posts.
set -euo pipefail
cd "$(dirname "$0")/.."

list="${1:?usage: e2e-post.sh <list> <from> <subject> <body>}"
from="${2:?from email required}"
subject="${3:?subject required}"
body="${4:-}"

export XLISTMAN_CONFIG="${XLISTMAN_CONFIG:-scripts/e2e.yaml}"
date="$(date -u +'%a, %d %b %Y %H:%M:%S +0000')"
msgid="$(date +%s%N)"

printf 'From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <%s@lists.test>\r\n\r\n%s\r\n' \
	"$from" "$list" "$subject" "$date" "$msgid" "$body" \
	| ./xlistman deliver "$list" >/dev/null
