# MTA validation harness

Disposable-but-kept fixtures for validating the xListman LMTP loop against a
real local MTA. The harness files (configs + scripts) are tracked in the repo
under `validate/postfix/` and `validate/exim/`; transient outputs (databases,
logs, spools, mailboxes) are gitignored.

Both validations use the same domain/address conventions:

- Domain: `lists.test` (reserved, non-routable), routed to xListman's LMTP
  server on `127.0.0.1:8024` (Postfix) or `127.0.0.2:8024` (exim).
- List: `dev@lists.test` (discussion), owner `barat@localhost`, with a second
  subscriber `poster@localhost` and a `deadbeat@localhost` subscription used
  to force the VERP bounce reverse leg.
- Settings: `bounce_threshold=2`, `owner_auto_disable_notice=true`.

The reverse leg works only when the MTA *accepts* the deadbeat recipient at
SMTP time and then *fails at final delivery*, so the DSN (null envelope sender,
RFC 3464) bounces back to the VERP envelope sender and routes through LMTP to
the bounce handler.

## Postfix (ADR 0021)

Prerequisite: Postfix installed (macOS ships it). Root needed to start it.

```
sudo ./validate/postfix/start-postfix.sh     # backup + extend main.cf/aliases, start
XLISTMAN_CONFIG=validate/postfix/postfix-validate.yaml ./xlistman serve
# ... run the flows (see docs/adr/0021) ...
sudo ./validate/postfix/stop-postfix.sh      # stop and restore main.cf + aliases
```

Config: `validate/postfix/postfix-validate.yaml` (outbound to localhost:25);
`validate/postfix/postfix.db` (gitignored). Postfix wiring:
`virtual_mailbox_domains = lists.test` + `virtual_transport = lmtp:[127.0.0.1]:8024`.

## exim (ADR 0022)

Prerequisites: `brew install exim` (no root for the instance itself), plus a
root one-time loopback setup so exim can deliver to xListman over LMTP:

```
sudo ./validate/exim/start-loopback.sh       # lo0 alias 127.0.0.2 + /etc/hosts: lists.lmtp.local
./validate/exim/start-exim.sh                # exim daemon on 127.0.0.1:2525
XLISTMAN_CONFIG=validate/exim/exim-validate.yaml ./xlistman serve
# ... run the flows (see docs/adr/0022) ...
./validate/exim/stop-exim.sh
sudo ./validate/exim/stop-loopback.sh        # revert the loopback setup
```

Config: `validate/exim/exim.conf` (routers: `lists.test` -> LMTP transport on
`127.0.0.2:8024` via `lists.lmtp.local`; `deadbeat@localhost` -> always-failing
appendfile; `@localhost` -> mbox appendfile under `validate/exim/mail/`).
`validate/exim/exim-validate.yaml` (LMTP on 127.0.0.2:8024, outbound to
localhost:2525). Spool, logs, mailboxes, and `validate/exim/exim.db` are
gitignored.

Why the loopback alias: exim refuses to route to its own SMTP interface
(127.0.0.1) as a loop guard, and no router option overrides it. `127.0.0.2` is
not in exim's `local_interfaces`, so the guard does not fire; the alias makes
it a bindable address, and the /etc/hosts entry names it (`lists.lmtp.local`)
so no DNS is involved.
