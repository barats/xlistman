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

### Phase 5 — Role console: web moderation + newsletter allowlist — complete and tested
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
- Test suite green; verified end-to-end in the browser (console overview/roles, held queue,
  approve/reject/discard, allowlist add/remove + subscriber-first errors, 401/403 gates,
  mobile viewport) via DOM checks.

### Phase 6 — Web admin console: settings, membership, and roles — complete and tested
- Per-list admin console with tabs: Overview / Settings / Members / Moderation / Allowlist,
  each a nested SvelteKit route (ADR 0016).
- Authorization: Owners get full per-list control (settings, members, roles, moderation,
  allowlist); Moderators keep moderation only (no settings, no member list). List deletion
  and ListType changes stay CLI-only (ADR 0005).
- Members: list (email, subscription status, delivery mode, role badges), authoritative add
  (GetOrCreateSubscriber → Active, no confirmation), remove, and approve/reject of Held
  Subscriptions (fixes the moderated-policy dead end).
- Roles: grant/revoke Owner and Moderator to any Subscriber, with a last-owner guard so a
  list can never have zero Owners.
- Settings: all 15 ListSettings + Description, grouped, with inline validation.
- Notifications wired to previously dormant settings: welcome on approval (WelcomeEmail),
  goodbye on Owner removal (GoodbyeEmail), pending-approval notice at confirm time,
  rejection notice on reject.
- CLI parity: `moderator add|remove`, `subscriber approve|reject`, `list config` — all
  sharing the same store functions as the web.
- Test suite green (`go test ./...`); verified end-to-end in the browser (owner console and
  tabs, settings save, members add/approve/reject, role grant/revoke + last-owner guard,
  held queue approve/reject/discard, allowlist add/remove + subscriber-first errors,
  moderator scoping, 401 gate, mobile viewport) via DOM checks.

### Phase 7 — Web server administration: the Administrator role — complete and tested
- New instance-wide **Administrator** privilege: a Subscriber designated via CLI (`admin add|remove|list`, first by the server operator) who can create domains/lists, manage other Administrators, delete lists, and change ListType from the web. Supersedes ADR 0005 for these operations; magic-link Subscriber auth + an instance-wide flag (no separate admin auth). New `Administrator` entity + store functions; CLI parity commands.
- The server-admin area lives at a top-level route `/server` (nav label **Server**, shown only to Administrators) with tabs Overview / Domains / Lists / Administrators — a page separate from the per-list role console, **My lists**, which lives at the top-level route `/admin`. The HTTP API stays at `/api/console/admin/*`.
- List creation mirrors the CLI: default first Owner is the creating Administrator, overridable to any known Subscriber, so a list is never accidentally ownerless.
- List deletion is a hard delete (list + settings + archive + held + subscriptions + roles + queue in one transaction) — fixes the existing orphan bug where `DeleteList` removed only the List row. Web requires typing the full address to confirm (the button stays disabled until it matches); server logs the deletion. CLI `list delete` uses the same cascade.
- ListType change: warning + confirm dialog stating the posting-policy consequence; new `list type` CLI command for parity.
- Nav is role-gated: **My lists** shows only to subscribers holding an Owner or Moderator role (via `has_list_role` on `/api/me`), **Server** only to Administrators — a plain member sees neither.
- Test suite green (`go test ./...`); verified end-to-end in the browser (Console nav gate, overview/domains/lists/administrators tabs, domain + list creation, typed-address delete guard, list-type change warning, administrator designate/revoke, 401/403 gates, mobile viewport) via DOM/text-snapshot checks (no screenshots).

### Phase 8 — Audit trail: immutable record of privileged actions
- New domain term: **Audit Event** (CONTEXT.md); an append-only `audit_events`
  table that records every privileged human action — moderation
  (approve/reject/discard), subscription approval/rejection, member add/remove,
  role grant/revoke, sender allowlist changes, settings updates, list
  create/delete/type, domain create/delete, and administrator designate/revoke —
  capturing who (a Subscriber snapshot, or the local CLI operator), when, and
  what it acted on (ADR 0018).
- Recorded inside the shared Pipeline for the actions email/CLI/web already
  funnel through (moderation, subscriptions, members, roles) — a failed audit
  write fails the action there — and at the web and CLI call sites for
  store-direct operations (list/domain/admin/settings), where audit failures
  are logged loudly (no cross-row transaction exists in the schema).
- Excluded: automated events (expiry sweep, bounce auto-disable), self-service
  actions, and failed attempts. Events are never edited, deleted, or pruned,
  and survive list deletion.
- Surfaces: per-list **Audit** tab in the web role console (Owners only),
  instance-wide **Audit** tab in the server-admin area (Administrators only),
  and a `xlistman audit` CLI command — all reverse-chronological with an
  action filter.
- Test suite green (`go test ./...`); verified end-to-end in the browser
  (owner audit tab, admin audit tab, moderation/role/settings events recorded
  and shown, filters, 401/403 gates, mobile viewport).

### Phase 9 — Sender held-status view
- A subscriber's own posts currently awaiting moderation approval are visible
  on the web UI: a **Posts awaiting approval** section on `/me` (self-service,
  any signed-in Subscriber), backed by `GET /api/me/held-posts`, which lists
  the sender's held posts across all lists (case-insensitive sender match)
  with list address, subject, sent time, and expiry.
- Read-only: moderation stays with Owners and Moderators. Pending posts are
  the gap it closes — the `SenderHeldNotice` email is per-list and often the
  only signal a sender has that their post is in the queue; approved/rejected
  outcomes remain email-notified, and discarded posts stay silent by design.
- Test suite green (`go test ./...`); verified end-to-end in the browser
  (sender sees own held posts, empty state, case-insensitive scoping,
  401 gate, mobile viewport).

### Phase 10 — Bounce management
- Per-list **Bounces** tab (Owners only) listing members with bounce activity
  (bounce_count > 0 or Disabled), with **Re-enable** and **Reset count**
  actions; CLI parity `subscriber re-enable|reset-bounces` (ADR 0019).
- Semantics fix: re-enabling a Subscription now resets its bounce counter on
  every re-enable path (self-service web, email `re-enable` command, Owner
  Bounces action), so a re-enabled member starts fresh instead of re-disabling
  on the next single bounce (CONTEXT.md Disabled Subscription updated).
- Wired the dormant `owner_auto_disable_notice` setting: the auto-disable flow
  moved into `Pipeline.RecordBounce` (increment → threshold → disable → notify
  Owners), replacing the inline, untested LMTP handler logic.
- Audit: Owner re-enable/reset record `member.re-enable` / `member.reset-bounces`
  events; self-service re-enable and automated bounces stay unrecorded
  (ADR 0018 exclusions).
- Test suite green (`go test ./...`); verified end-to-end in the browser
  (owner bounces tab, re-enable + reset actions reflected in members, owner
  notice on auto-disable, 401/403 gates, empty state, mobile viewport).

### After Phase 10 (deferred catalog continues)
- Optionally validate the LMTP loop against local `postfix` (installed here, not yet running).
