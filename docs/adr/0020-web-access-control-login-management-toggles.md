# Web access control: DB-backed login and management toggles

Server operators need a way to protect an instance from the web attack surface without restarting the daemon or stopping email. We added two instance-wide switches — web login and web management — that an Administrator toggles via the CLI, stored in the database (a `web_settings` row) and read by the HTTP server per request, so changes apply immediately with no restart, persist across restarts, and are shared by all instances (ADR 0008).

`xlistman disable login` blocks the magic-link flow (no new Sessions) and deletes every existing Session; `xlistman disable management` blocks the per-list role console and the server-admin area. Public pages, the subscribe form, subscriber self-service, all email paths, and the CLI itself are unaffected. Every toggle is recorded as an Audit Event (`web.login-enable`, `web.login-disable`, `web.management-enable`, `web.management-disable`).

## Considered Options

- **Database-backed flags (chosen).** Matches the existing pattern where the CLI writes directly to the DB (Administrator, Audit Event) and the server reads it per request (`IsAdministrator`). No restart, persistent, multi-instance-safe, and covered by the existing backup/StateDirectory model.
- **Config-file flags.** Would require a restart (or a new reload mechanism) to take effect — the wrong behavior for an incident-response switch, and the CLI would be editing a file the running daemon never re-reads.
- **Control channel to the daemon.** Most "live," but no CLI command talks to the daemon today (all open the DB directly), so this is a large architectural change with no payoff the DB approach doesn't already provide.

## Consequences

- "Disable login logs everyone out" is deliberate: it makes the switch a real lockdown, not just a gate on new sign-ins.
- The web UI surfaces state via a public `GET /api/web-status` endpoint so the `/auth` page and the consoles can show clear disabled notices instead of failing mysteriously.
- The CLI is the only way to flip these switches — the web console cannot disable its own management — so an Administrator can always undo a lockdown.
