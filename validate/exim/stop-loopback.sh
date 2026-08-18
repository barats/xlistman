#!/bin/bash
# Remove the 127.0.0.2 loopback alias and /etc/hosts entry used by the exim
# validation route. Requires root (sudo). Run from the repo root.
set -euo pipefail

if ifconfig lo0 | grep -q "inet 127.0.0.2"; then
  ifconfig lo0 -alias 127.0.0.2
  echo "removed lo0 alias 127.0.0.2"
fi

if grep -q "lists.lmtp.local" /etc/hosts; then
  sed -i '' '/lists\.lmtp\.local/d' /etc/hosts
  echo "removed lists.lmtp.local from /etc/hosts"
fi

dscacheutil -flushcache 2>/dev/null || true
echo "OK: loopback routing entry removed"
