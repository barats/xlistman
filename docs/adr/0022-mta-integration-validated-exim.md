# MTA integration validated against exim: LMTP over a non-root instance

ADR 0021 validated the full loop against a local Postfix and fixed the null
envelope sender handling. This ADR validates the same loop against a second,
independently implemented MTA — exim — to prove the integration generalizes
and, in particular, that the null-sender fix (RFC 3464 DSNs) holds under a
different MTA's bounce generation. It records the exim-specific wiring, which
differs substantially from Postfix.

**Instance shape.** exim is not preinstalled on macOS and a system install
(config in `/etc`, spool under `/var/spool/exim`, a dedicated user) is
invasive, so the validation runs a user-space instance: `exim_user` /
`trusted_users` set to the invoking user, the daemon on a high port
(`daemon_smtp_ports = 2525`, `local_interfaces = 127.0.0.1`), and the spool,
logs, and mailboxes under the disposable fixture. The daemon chdirs on
startup and requires **absolute** spool/pid paths, so the config paths are
macros (`SPOOLDIR`, `LOGDIR`, `MAILDIR`, `PIDFILE`) overridden at runtime with
`-D` flags by a wrapper script. Delivery mechanics are identical on any port,
so a high-port instance validates the production shape.

**Inbound wiring.** exim routes the hosted domain with a `manualroute` router
to an LMTP transport:

```
xlistman_lmtp:
  driver = manualroute
  domains = lists.test
  route_list = * lists.lmtp.local
  transport = xlistman_lmtp

xlistman_lmtp:
  driver = smtp
  protocol = lmtp
  port = 8024
```

Any local part in the domain is accepted and the full `listname+role@domain`
envelope recipient is passed through, mirroring Postfix's
`virtual_mailbox_domains` behavior.

**exim-specific findings.**
- The standalone `lmtp` transport driver is for LMTP over a Unix socket or a
  command; network LMTP requires `driver = smtp` with `protocol = lmtp`.
- exim refuses to deliver through a remote transport to its own SMTP interface
  ("remote host address is the local host") as a loop guard, and no router
  option overrides it. The route target must therefore be a **different local
  address that is not in `local_interfaces`**: a second loopback address,
  `127.0.0.2`, set up as a `lo0` alias and named in `/etc/hosts`
  (`lists.lmtp.local`) so no DNS is involved. The daemon's `local_interfaces`
  stays `127.0.0.1`, so the guard does not fire.
- A non-root daemon must set `exim_user` (otherwise exim switches to its
  compiled-in exim uid and fails) and `trusted_users` for `-C` to be accepted.

**Reverse leg.** exim rejects *unroutable* recipients at RCPT time, which would
send the outbound worker down the ADR 0006 queue-retry path instead of the LMTP
reverse leg. As with Postfix, the deadbeat recipient must be *accepted* and
then *fail at final delivery*: a router matching `deadbeat@localhost` sends it
to an always-failing `appendfile` (a path under `/nonexistent`), so exim
generates a DSN to the VERP envelope sender (`dev-bounces+…@lists.test`), which
routes back through the `lists.test` router into the LMTP bounce handler. This
re-validates the null-sender fix: exim delivers the DSN with `MAIL FROM:<>`.

**Outbound.** xListman submits via SMTP to `localhost:2525`; post envelope
senders are VERP addresses in the list domain; final local delivery to
`owner@localhost` is an `appendfile` mbox under the fixture (self-contained,
no mail-spool permissions).

**Considered Options.**
- The `lmtp` transport driver (socket/command) — rejected: xListman's LMTP
  server is TCP; the driver requires `socket` or `command`.
- Routing directly to `127.0.0.1` — rejected: exim's local-host loop guard.
- An `/etc/hosts` entry mapping to `127.0.0.1` — rejected: the guard is
  address-based, so the target must be a non-local address; the alias +
  hostname under `127.0.0.2` is the working form.
- A system-level exim on port 25 under root — rejected: a user-space instance
  validates the identical routing/transport/DSN mechanics without system
  changes.

## Validation results

The full loop was exercised live against the user-space exim instance with a
dedicated fixture database and the routing above. All five flows passed:
subscribe + confirm double opt-in, a post round trip (subject prefix, footer,
`List-*` headers, VERP envelope sender, final delivery to the fixture mbox),
the `-request` email command, a raw SMTP submission to exim's `smtpd` on
`:2525`, and the VERP bounce reverse leg. The MTA's queue was empty at the
end.

**Null-sender fix re-validated under a second MTA.** exim generates delivery
failure DSNs with the null reverse-path `MAIL FROM:<>` (RFC 3464), exactly as
Postfix does. With ADR 0021's fix in place, the DSN is accepted over LMTP,
decoded, and attributed to the subscription; the deadbeat member auto-disabled
at the list's `bounce_threshold` and the Owners were notified. The reverse leg
now works under both MTAs.

**exim-specific validation findings.**
- The deadbeat reverse-leg trigger differs from Postfix. exim rejects
  *unroutable* recipients at RCPT time (the ADR 0006 queue-retry path), and an
  `appendfile` to a nonexistent path is only a *transient* failure (defer +
  retry). The reliable immediate bounce is a `pipe` transport running a
  command that exits with a permanent-failure code (`/usr/bin/false`).
- exim's taint checking (4.94+) forbids tainted data (`$local_part`, derived
  from the untrusted envelope) in transport file paths. Local delivery uses a
  constant mbox path; only `owner@localhost` receives real local mail.
- exim's manualroute parses a bare IPv4 literal in `route_list` but not an IPv6
  literal (`::1` — the `:` is consumed as the `host:port` separator) or a
  bracketed literal, which is why the second loopback address (`127.0.0.2`)
  named in `/etc/hosts` is the working route target.
