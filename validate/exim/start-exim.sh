#!/bin/bash
# Start the user-space exim instance for the xListman LMTP-loop validation.
# No sudo needed: daemon on 127.0.0.1:2525, spool/logs under validate/.
# Run from the repo root. Stop with stop-exim.sh.
set -euo pipefail

if lsof -nP -iTCP:2525 -sTCP:LISTEN >/dev/null 2>&1; then
  echo "exim already running on 127.0.0.1:2525"
  exit 0
fi

exec "$(dirname "$0")/exim-cli.sh" -bd -q10m
