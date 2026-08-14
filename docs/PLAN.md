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

### Phase 2 — mailroom gaps — complete and tested
- Expiry sweeper: the hourly sweep now also prunes expired Magic Links and Sessions, no
  grace period (ADR 0012).
- Queue max-retry → bounce: post deliveries carry `OriginalSender`; at max retries
  (configurable `queue.max_retries`, default 8) they bounce a failure notice to the poster
  then drop; list-originated notices drop with a warning log (ADR 0006).
- Digest worker: per-list daily/weekly compilation from the archive via an elapsed-based
  self-healing watermark (`last_digest_sent_at`), multipart/digest format, atomic watermark
  update so multiple instances can't double-send, sent via the outbound queue with VERP
  (ADR 0014).
- Test suite green; digest compiled and delivered live in a sink-mode smoke test.

## Next

### Phase 3 — Web UI (SvelteKit 5 SPA)
- `web/` SvelteKit app (shadcn-svelte), compiled to static and embedded via `go:embed`
  (ADR 0007).
- Public: list index, list info, subscribe form. Auth: magic-link request + verify.
  Subscriber: my subscriptions, delivery prefs, re-enable, unsubscribe.
  Archives: threaded browsing + full-text search (members-only).
- Go server serves the SPA with a fallback to `index.html` for client-side routes.
  (`make build` currently requires the frontend build in `web/build`.)

### Phase 4 — Integration and validation
- `make build` end-to-end (frontend + Go binary), run, and exercise in the browser.
- Optionally validate the LMTP loop against local `postfix` (Docker is not available here).
