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

### Phase 3 — Web UI (SvelteKit 5 SPA) — complete and tested
- `web/` SvelteKit app (shadcn-svelte), compiled to static and embedded via `go:embed`
  (ADR 0007).
- Public: list index, list info, subscribe form. Auth: magic-link request + verify.
  Subscriber: my subscriptions, delivery prefs, re-enable, unsubscribe.
  Archives: threaded browsing + full-text search (members-only).
- Go server serves the SPA with a fallback to `index.html` for client-side routes.
  (`make build` currently requires the frontend build in `web/build`.)
- SPA build committed so `go build` alone yields a complete binary.

### Phase 4 — Integration and validation — complete
- `make build` end-to-end (frontend + Go binary), run, and exercised in the browser
  (DOM-based checks; no screenshots).
- Verified live: list index/detail, double opt-in subscribe, magic-link login, /me
  delivery prefs + re-enable + unsubscribe, members-only archives (browse/search/detail),
  401/403 gate, invalid-link redirect, sign-out, mobile viewport, empty states.
- Fixed a real integration bug found during validation: the emailed magic link pointed at
  `/auth/verify` (no such SPA route) instead of `/api/auth/verify`; the login flow 404'd.
  The link now targets the API endpoint, and the `login` test helper asserts the path so
  this cannot regress.
- Test suite green (`go test ./...`).

## Next

### Phase 5 — Role console: web moderation + newsletter allowlist
- New domain term: **List Role** (Owner | Moderator | Designated Sender); a web role
  console surfaces every list where the signed-in Subscriber holds a role.
- Web moderation: held-message queue + detail + Approve/Reject/Discard for Owners and
  Moderators, sharing one moderation-action function with the email and CLI paths so the
  three cannot drift. Session + server-side role check; held messages by raw ID on the web
  (email keeps opaque tokens, ADR 0010). Moderator-side only; no sender held-status view.
- Allowlist management: Designated Sender (Subscriber-first — only known Subscribers can be
  designated) add/remove/list via the CLI (server admins, ADR 0005) and via the role console
  for Owners of Newsletter lists. Designated Sender grants posting only; archives stay
  members-only. A sender may also hold a subscription (no exclusivity).
- Deferred (explicitly out of scope): web list configuration, web owner/subscriber/moderator
  management, sender held-status view, moderation audit trail, bounce management UI.
- ADR 0015 records the decision and supersedes ADR 0010's deferred-web rationale.

- Optionally validate the LMTP loop against local `postfix` (Docker is not available here).
