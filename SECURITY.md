# Security Policy

## Reporting a Vulnerability

xListman is pre-1.0 and maintained by a single developer. If you find a
security vulnerability, please report it privately so it can be fixed before
it is disclosed publicly:

- Open a [private vulnerability report](https://github.com/barats/xlistman/security/advisories/new)
  on GitHub (recommended), or
- File a regular issue if you are unsure whether the finding is security-relevant.

Please include a minimal reproduction (list configuration, mail flow or HTTP
request, and the observed behavior). You should receive a response within a
few days. Coordinated disclosure is appreciated: we aim to ship a fix in a
patch release before details are made public.

## Security Posture

Design decisions relevant to security, in no particular order:

- **No passwords.** Authentication is passwordless: one-time Magic Links that
  expire, backed by DB-stored sessions. There is no credential storage, so
  there are no credentials to leak.
- **Email-only posting.** List content is authored by email (ADR 0024); the
  web UI is read/admin-only, so there is no web upload/rendering attack
  surface for posts beyond the archive viewer.
- **Sanitized archives.** Archive HTML is sanitized server-side with
  bluemonday before rendering (ADR 0026).
- **Rate limiting.** Web write endpoints (subscribe, magic-link) are
  rate-limited in memory, and the magic-link send endpoint does not reveal
  whether an address is subscribed (ADR 0023).
- **Audit trail.** Privileged actions are recorded immutably in an
  append-only `audit_events` table (ADR 0018).
- **Minimal runtime.** The Docker image runs the single static binary on
  `scratch` with no shell or package manager.

## Operational Notes

- **Serve over HTTPS.** xListman does not terminate TLS itself; run it behind
  a reverse proxy (Caddy, nginx, Traefik) that does. Set `web.base_url` to the
  public HTTPS origin.
- **SMTP credentials** live in the config file or environment. Use the
  `${ENV_VAR}` secret-expansion syntax in `xlistman.yaml` rather than
  committing plaintext.
- **Back up before upgrading.** The SQLite database is the store of record;
  keep a copy of `xlistman.db` before updating to a new release.

## Supported Versions

Only the latest tagged release is supported. Security fixes land in a patch
bump of the current `0.x` line.
