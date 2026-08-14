# xListman Plan

Goal: a one-binary, self-hosted mailing list manager (GNU Mailman alternative) with a
passwordless web UI. See `CONTEXT.md` for the domain language and `docs/adr/` for decisions.

## Done

### Backend (email + CLI) — complete and tested
- Inbound: LMTP + pipe-mode Unix socket; subscribe / confirm / post / unsubscribe / bounce /
  moderation / email commands (`-request`) / owner forwarding (`-owner`), all via
  `listname+role@domain` addressing (ADR 0002, 0010, 0011).
- Outbound: database-backed queue with exponential backoff worker (ADR 0006).
- Domain model with full four-status Subscription lifecycle, VERP bounce tracking with
  auto-disable, held-message moderation (ADR 0009, 0010).
- CLI admin: `domain`, `list`, `owner`, `subscriber`, `moderation`, `queue`, `config`
  (ADR 0005).

### Web API (Phase 1) — complete and tested
- Passwordless auth: one-time Magic Links and DB-backed Sessions with a session cookie
  (ADR 0012).
- Subscribe via web shares `Pipeline.Subscribe` with the email path.
- Self-service: `/api/me`, set delivery (`regular|digest|nomail`), re-enable, unsubscribe.
- Archives: members-only list/search/detail with SQLite FTS5 full-text search (ADR 0013).
- Test suite green (`go test ./...`); smoke-tested live with curl.

## Next

### Phase 2 — mailroom gaps (make the backend honest)
- Digest worker: per-list daily/weekly compilation for digest-mode subscribers (currently
  digest subscribers receive nothing).
- Queue worker: max-retry → bounce to the original sender (ADR 0006 promises it; today it
  retries forever).
- Expiry sweeper: delete expired held messages, Magic Links, and Sessions on a schedule
  (ADR 0012 requires session/token pruning).

### Phase 3 — Web UI (SvelteKit 5 SPA)
- `web/` SvelteKit app (shadcn-svelte), compiled to static and embedded via `go:embed`
  (ADR 0007).
- Public: list index, list info, subscribe form. Auth: magic-link request + verify.
  Subscriber: my subscriptions, delivery prefs, re-enable, unsubscribe.
  Archives: threaded browsing + full-text search (members-only).
- Go server serves the SPA with a fallback to `index.html` for client-side routes.

### Phase 4 — Integration and validation
- `make build` end-to-end (frontend + Go binary), run, and exercise in the browser.
- Optionally validate the LMTP loop against local `postfix` (Docker is not available here).
