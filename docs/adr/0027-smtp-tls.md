# Outbound SMTP TLS

Outbound mail was sent with `net/smtp.SendMail`: opportunistic STARTTLS only
(used if the relay happened to advertise it), no implicit TLS (`:465`), no way
to require an encrypted channel, and `PLAIN` credentials were transmitted
whenever a username was configured — even in the clear. This ADR records the
decision to make the relay connection's transport security explicit and to
guard credentials behind encryption.

**Decision — a `smtp.tls` mode with four values.** `smtp.tls` ∈
`none | starttls | starttls-required | implicit`, default `starttls`. The
default is byte-for-byte today's behavior (opportunistic STARTTLS), so
existing configs are unchanged. `implicit` negotiates TLS immediately on
connect (the `:465`/SMTPS pattern); `starttls-required` fails the send if the
relay does not advertise STARTTLS.

**Decision — verify by default, with an escape hatch.** Certificates are
verified against the system roots with `ServerName = smtp.host`. A separate
`tls_insecure_skip_verify` toggle (default `false`) exists for relays with
self-signed or private-CA certificates; it is off by default so the secure
path is the default.

**Decision — credentials only over encryption.** When `smtp.username` is set,
`PLAIN` credentials are only sent once the session is encrypted (implicit TLS,
or STARTTLS successfully negotiated); otherwise the send fails with a clear
error instead of leaking the password. This matches modern relay practice
(e.g. Postfix rejects AUTH without TLS) and makes the common misconfiguration
loud rather than silent.

**Config validation** rejects an unknown `smtp.tls` value at startup, and
rejects `smtp.tls=none` combined with `smtp.username` (that combination can
never deliver mail, since auth would be refused).

**Considered Options**:
- **Separate `smtp.require_starttls` boolean** next to a three-value mode —
  rejected: it splits "mode" from "strictness" into two knobs that can be set
  inconsistently (`require_starttls` with `tls: none` is meaningless).
- **Port-based auto-detection** (`465` implies implicit, `25`/`587` imply
  STARTTLS) — rejected: port alone should not decide the security posture, and
  it cannot express "plaintext even on `465`" or a custom TLS port.
- **Verify-only, no escape hatch** — rejected: self-signed/private-CA relays
  are common in on-prem and corporate setups; a self-hosted tool needs a
  documented, default-off way to trust them.
- **Keep sending credentials over plaintext** — rejected: it leaks passwords
  whenever a relay does not offer STARTTLS; refusing loudly is safer and
  forces the operator to fix the relay or drop auth.
