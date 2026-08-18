#!/bin/bash
# Stop the user-space exim instance for the xListman LMTP-loop validation.
# Run from the repo root.
set -euo pipefail

pids="$(lsof -tiTCP:2525 -sTCP:LISTEN 2>/dev/null || true)"
if [ -n "$pids" ]; then
  kill $pids 2>/dev/null || true
  echo "exim stopped (pids: $pids)"
else
  echo "no exim listening on 127.0.0.1:2525"
fi
