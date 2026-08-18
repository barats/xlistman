#!/bin/bash
# Validate the LMTP loop against local Postfix: stage the config and start Postfix.
# Requires root (sudo). Reversible via stop-postfix.sh.
# Run from the repo root.
set -euo pipefail

# 1. Back up and extend main.cf with the xListman virtual domain + LMTP transport.
if [ -f /etc/postfix/main.cf.xlistman-validate.bak ]; then
  echo "main.cf backup already exists; not re-applying" >&2
else
  cp /etc/postfix/main.cf /etc/postfix/main.cf.xlistman-validate.bak
  cat >> /etc/postfix/main.cf <<'EOF'

# --- xListman LMTP loop validation (added by start-postfix.sh) ---
virtual_mailbox_domains = lists.test
virtual_transport = lmtp:[127.0.0.1]:8024
EOF
fi

# 2. Deadbeat alias: a recipient Postfix accepts (250) but whose local
#    delivery fails, forcing a DSN back to the VERP envelope sender.
if [ -f /etc/aliases.xlistman-validate.bak ]; then
  echo "aliases backup already exists; not re-applying" >&2
else
  cp /etc/aliases /etc/aliases.xlistman-validate.bak
  printf '\n# --- xListman LMTP loop validation ---\ndeadbeat: no_such_local_user_xyz\n' >> /etc/aliases
fi

newaliases

# 3. Sanity-check the config, then start.
postfix check
postfix start
postfix reload
postfix status
echo "OK: Postfix running with lists.test -> lmtp:[127.0.0.1]:8024"
