#!/bin/bash
# exim wrapper for the xListman LMTP-loop validation: always points at the
# validate/exim config with absolute -D macro paths (the daemon chdirs on
# startup and requires absolute spool/pid paths). Use for submissions, queue
# checks, and daemon start:
#   ./validate/exim/exim-cli.sh -f sender recipient < message.eml
set -euo pipefail

ABS="$(cd "$(dirname "$0")/../.." && pwd)"
exec exim \
  -DSPOOLDIR="$ABS/validate/exim/spool" \
  -DLOGDIR="$ABS/validate/exim/log" \
  -DMAILDIR="$ABS/validate/exim/mail" \
  -DPIDFILE="$ABS/validate/exim/spool/exim-daemon.pid" \
  -C "$ABS/validate/exim/exim.conf" \
  "$@"
