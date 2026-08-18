# MTA integration validated against Postfix: virtual-domain LMTP transport

ADR 0002 chose the integration at the protocol level — LMTP inbound (with a
pipe-mode Unix socket fallback) and SMTP outbound through the database-backed
queue (ADR 0006) — but the concrete MTA-side wiring was never recorded or
exercised against a real MTA. This ADR records the integration shape after live
validation of the full loop against a local Postfix, including the decisions
that validation surfaced.

**Inbound wiring.** Postfix treats xListman's hosted domains as virtual mailbox
domains and hands every recipient in them to xListman over LMTP:

```
virtual_mailbox_domains = lists.example.com
virtual_transport = lmtp:[127.0.0.1]:8024
```

The `[127.0.0.1]` bracket syntax pins the LMTP client to that host (no MX
lookup). Postfix's LMTP client does not apply `recipient_delimiter` — it
delivers the full envelope recipient through to xListman — so xListman parses
`listname+role@domain` itself (post, `-request`, `-owner`, `-subscribe`,
`-unsubscribe`, `-bounces+VERP`, `-confirm+token`, `-moderate+token`). This is
the production integration for an instance hosting virtual domains.

**Inbound, rejected alternatives.**
- `transport_maps` with the domain also in `mydestination` — rejected: a
  `mydestination` domain triggers `local_recipient_maps`
  (`passwd.byname` + `$alias_maps`), which rejects list addresses like
  `dev-subscribe@…` as unknown local users at SMTP time; the whole point of the
  `+role` scheme is that the local part is not a system user.
- `transport_maps` with the domain in `relay_domains` — rejected: relay
  semantics (address verification, no final-delivery guarantees) are the wrong
  model for a final-delivery agent like a mailing list host.

**LMTP binding.** The LMTP server is unauthenticated (it accepts and processes
mail for any domain it hosts), so it must bind a private interface —
`127.0.0.1:8024` in development — not `:8024` on all interfaces. The dev
default of binding all interfaces is a foot-gun in any real deployment.

**Outbound wiring.** The queue worker submits via SMTP to the MTA's submission
port (`smtp.host: localhost`, `smtp.port: 25`), no auth, permitted by the
MTA's `mynetworks` for loopback. Post envelope senders are VERP addresses in
the list domain (`dev-bounces+recipient=domain@lists.example.com`), so any
delivery failure causes the MTA to bounce a DSN back to the VERP address, which
routes in through the same `virtual_transport` and is attributed to the
Subscription by the LMTP bounce handler (ADR 0019). The reverse leg is part of
the loop, not a separate channel.

**Validating the reverse leg.** To force a DSN back to a VERP sender against a
local Postfix, the failing recipient must be *accepted* at SMTP (so the queue
considers delivery done) but then *fail* at final delivery. The reliable trick
is an `/etc/aliases` entry mapping a subscribed address to a non-existent local
user (`deadbeat: no_such_local_user_xyz`): `local_recipient_maps` includes
`$alias_maps`, so Postfix accepts the RCPT (250), then `local(8)` fails the
expansion and Postfix emits the DSN. A recipient rejected at RCPT time instead
exercises the ADR 0006 queue max-retry path, not the LMTP reverse leg.

**Considered Options** (validation harness).
- A second Postfix instance on an alternate config directory, leaving the
  system Postfix untouched — rejected: it duplicates `main.cf` and overrides
  `queue_directory`/`data_directory` to run a true second instance, and it
  validates a different deployment shape than production uses. The system
  Postfix was instead given a small, backed-up, fully reverted change.
- Exercising the loop with outbound in sink mode — rejected: that validates
  only the inbound half and leaves the SMTP client unproven against a real MTA.

## Validation results

The full loop was exercised live against a local Postfix with the wiring above
and a dedicated fixture database: subscribe + confirm double opt-in, a post
round trip (subject prefix, footer, `List-*` headers, VERP envelope sender,
final delivery to a local subscriber mailbox), the `-request` email command, a
raw SMTP submission to the MTA's `smtpd`, and a deliberately failed delivery
whose DSN routed back through LMTP and auto-disabled the member at the list's
`bounce_threshold`, notifying the Owners. The MTA's queue was empty at the end.

**Bug found and fixed: the null envelope sender.** Postfix delivers bounce DSNs
(RFC 3464) with the null reverse-path `MAIL FROM:<>`. The LMTP handler stored
the null sender as an empty string and used `mailFrom == ""` as its "no MAIL
yet" gate, so `RCPT` was rejected with `503 MAIL first` and every real bounce
was silently dropped — the reverse leg could only work in tests that supplied a
non-null envelope sender. The handler now records "MAIL seen" separately from
the (possibly empty) sender address (`mailSet` alongside `mailFrom`), and a
wire-level regression test drives a null-sender DSN through `serve` and asserts
the bounce is attributed. The envelope MAIL FROM is not used anywhere in
processing (recipients are addressed by `RCPT`, senders by the message `From`
header), so accepting the null sender is safe.

**Validation harness notes.** Two behaviors shaped the test: the pipeline never
delivers a post back to its own sender, so a post round trip needs a second
subscriber who is not the poster; and the outbound worker polls the queue every
~5s, so assertions must wait out the poll interval plus the MTA's asynchronous
delivery. The reverse-leg trigger — an `/etc/aliases` entry mapping a subscribed
address to a non-existent local user — is described above.
