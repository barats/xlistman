# Posting is email-only; no web composer

Members post to a List only via email (LMTP/SMTP inbound through the MTA), never
through the web UI. The web UI is read/admin-only for list content: subscribers
read archives and manage themselves, Owners/Administrators administer, nobody
composes a post in the browser. Phase 14 scoping explicitly considered adding a
web post composer and rejected it; this ADR records the decision so a future
contributor does not silently re-derive it.

Posting already has one shared entry point — `Pipeline.ProcessPost`, which
determines sender authorization, applies the posting policy
(`DecidePostAction`: accept/hold/reject), rewrites headers, archives, enqueues
delivery, and routes held posts to moderation with their notices. Email is the
designed interface of the product; the web UI exists for administration,
self-service, and reading archives.

A web composer would have been a second posting surface with its own content
model (plain text vs HTML vs attachments), its own validation, and its own
moderation/audit interaction, all layered on top of the same Pipeline. The cost
of keeping two posting surfaces from drifting is real, and the benefit is
marginal: email is the natural posting interface for a mailing list manager, and
members are expected to have mail configured.

## Considered Options

- **Email-only posting (chosen).** One posting path, one policy, one content
  model. The web UI stays read/admin-only for content. New members learn "post
  by email to `listname@domain`," the same contract every other list manager
  uses.
- **Web post composer.** Surface a compose form for lists where the signed-in
  Subscriber has posting rights, constructing a message server-side and calling
  the same `Pipeline.ProcessPost`. Rejected: it duplicates the posting surface,
  forces content-model decisions (HTML, attachments, size limits) the email path
  does not make, and adds moderation/audit wiring — all to serve a minority use
  case. If ever reconsidered, it must funnel through `ProcessPost` unchanged,
  never a parallel path.

## Consequences

- The web UI remains read/admin-only for list content; nobody posts from the
  browser.
- Posting policy, moderation, audit, archiving, and bounce behavior stay
  single-sourced in the email path — the "paths cannot drift" property holds.
- A member who cannot or will not use email cannot post — accepted as the
  product's design.
- Future web posting is possible only via `Pipeline.ProcessPost`, preserving the
  one-policy property.
