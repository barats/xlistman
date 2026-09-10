#!/bin/bash
# Stop Postfix and restore the pre-validation system config.
# Requires root (sudo). Run from the repo root.
set -euo pipefail

postfix stop || true

if [ -f /etc/postfix/main.cf.xmailman-validate.bak ]; then
  mv /etc/postfix/main.cf.xmailman-validate.bak /etc/postfix/main.cf
  echo "restored /etc/postfix/main.cf"
fi

if [ -f /etc/aliases.xmailman-validate.bak ]; then
  mv /etc/aliases.xmailman-validate.bak /etc/aliases
  newaliases
  echo "restored /etc/aliases"
fi

echo "OK: Postfix stopped and config restored."
