#!/usr/bin/env bash
# e2e-get-link.sh <email> [--wait]
#
# Prints the newest magic-link verify URL emailed to <email>, by scanning the
# e2e sink directory. The outbound worker writes sink files asynchronously
# (5s poll interval), so pass --wait to poll for up to ~15s for the file to
# appear (the login step in web/tests does this after submitting the form).
#
# Overridable via E2E_SINK_DIR and E2E_BASE_URL.
set -euo pipefail

email="${1:?usage: e2e-get-link.sh <email> [--wait]}"
want_wait="${2:-}"
sink="${E2E_SINK_DIR:-/tmp/xlistman-e2e-mail}"
base_url="${E2E_BASE_URL:-http://localhost:8090}"

# Sink filenames are <nanos>-<recipient>.eml with non-[A-Za-z0-9._-] chars
# replaced by '_' (see internal/mail/smtp.go). Tokens are hex (32 bytes).
sanitized="$(printf '%s' "$email" | tr -c 'A-Za-z0-9._-' '_')"

find_link() {
	local file link
	file="$(ls -1t "$sink"/*"${sanitized}.eml" 2>/dev/null | head -1 || true)"
	[[ -z "$file" ]] && return 1
	link="$(grep -oE "${base_url}/api/auth/verify\\?token=[0-9a-f]+" "$file" | head -1 || true)"
	[[ -z "$link" ]] && return 1
	printf '%s\n' "$link"
}

if link="$(find_link)"; then
	echo "$link"
	exit 0
fi

if [[ "$want_wait" == "--wait" ]]; then
	for _ in $(seq 1 30); do
		sleep 0.5
		if link="$(find_link)"; then
			echo "$link"
			exit 0
		fi
	done
fi

echo "e2e-get-link: no magic-link email for $email in $sink" >&2
exit 1
