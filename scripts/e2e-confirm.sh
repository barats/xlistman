#!/usr/bin/env bash
# e2e-confirm.sh <email> <list>
#
# Completes the double opt-in for <email> on <list> by replying (via the mail
# pipeline) to the newest confirmation email sent to <email>. Used by test
# Setup steps to finish a subscription the web form started.
#
# Overridable via E2E_SINK_DIR.
set -euo pipefail
cd "$(dirname "$0")/.."

email="${1:?usage: e2e-confirm.sh <email> <list>}"
list="${2:?list address required}"
sink="${E2E_SINK_DIR:-/tmp/xlistman-e2e-mail}"

sanitized="$(printf '%s' "$email" | tr -c 'A-Za-z0-9._-' '_')"
file="$(ls -1t "$sink"/*"${sanitized}.eml" 2>/dev/null | head -1 || true)"
if [[ -z "$file" ]]; then
	echo "e2e-confirm: no mail for $email in $sink" >&2
	exit 1
fi

# The confirmation email's Reply-To is <listname>-confirm+<token>@<domain>.
name="${list%%@*}"
domain="${list##*@}"
confirm="$(grep -m1 -i "^Reply-To: ${name}-confirm+.*@${domain}" "$file" | sed -E 's/^[Rr]eply-[Tt]o: //' | tr -d '\r' || true)"
if [[ -z "$confirm" ]]; then
	echo "e2e-confirm: no confirmation for $email on $list in $file" >&2
	exit 1
fi

./scripts/e2e-post.sh "$confirm" "$email" "Confirm" "Please confirm my subscription."
echo "e2e-confirm: confirmed $email on $list via $confirm"
