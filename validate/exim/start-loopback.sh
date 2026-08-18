#!/bin/bash
# Add the 127.0.0.2 loopback alias and /etc/hosts entry used by the exim
# validation route (lists.lmtp.local -> 127.0.0.2). Requires root (sudo).
# Reversible via stop-loopback.sh. Run from the repo root.
set -euo pipefail

# 1. Loopback alias so 127.0.0.2 is a bindable local address on macOS
#    (the whole 127/8 is loopback, but only 127.0.0.1 is assigned by default).
if ifconfig lo0 | grep -q "inet 127.0.0.2"; then
  echo "lo0 alias 127.0.0.2 already present"
else
  ifconfig lo0 alias 127.0.0.2
  echo "added lo0 alias 127.0.0.2"
fi

# 2. /etc/hosts entry so lists.lmtp.local resolves without DNS. exim refuses
#    to route to its own SMTP interface (127.0.0.1) as a loop guard, so the
#    LMTP target needs a different local address under a hostname.
if grep -q "lists.lmtp.local" /etc/hosts; then
  echo "/etc/hosts already maps lists.lmtp.local"
else
  printf '127.0.0.2\tlists.lmtp.local\n' >> /etc/hosts
  echo "added '127.0.0.2 lists.lmtp.local' to /etc/hosts"
fi

dscacheutil -flushcache 2>/dev/null || true

# 3. Verify.
ifconfig lo0 | grep "inet 127.0.0.2" || { echo "ERROR: alias not set" >&2; exit 1; }
dscacheutil -q host -a name lists.lmtp.local 2>/dev/null | grep "ip_address: 127.0.0.2" \
  || grep -q "lists.lmtp.local" /etc/hosts || { echo "ERROR: /etc/hosts entry missing" >&2; exit 1; }
echo "OK: lists.lmtp.local -> 127.0.0.2 available"
