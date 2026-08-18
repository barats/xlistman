# Audit trail: an immutable record of privileged actions

Phase 8 adds an **Audit Event** trail: an immutable record of every privileged
action taken on an instance, List, or membership, capturing who acted, when,
and what it acted on. It answers "who did what" for the destructive and
accountable operations that the web console and CLI make available — moderation
decisions, subscription approvals, membership and role changes, configuration
changes, and list/domain/Administrator lifecycle operations. The trail is
append-only: events are never edited or deleted, and they survive the deletion
of the List they refer to, so a deleted list's history remains visible to
server Administrators. The term **Audit Event** is defined in CONTEXT.md.

**Scope — recorded actions** (namespaced so the two "approve" actions in the
domain are unambiguous): `moderation.approve|reject|discard` (on Held
Messages), `subscription.approve|reject` (on Held Subscriptions),
`member.add|remove`, `role.grant|revoke` (Owner/Moderator),
`sender.add|remove` (Designated Sender allowlist), `settings.update` (one
event per save, with the changed settings named), `list.create|delete|type`,
`domain.create|delete`, and `admin.designate|revoke`.

**Scope — excluded**: automated state changes (the expiry sweeper discarding
Held Messages, bounce auto-disable) have no human actor and are answerable
through the server log, so they are not recorded; self-service actions on
one's own Subscription (unsubscribe, re-enable, delivery change) are the
member's own business; and failed attempts never changed state.

**Actor**: every event carries an actor — either the acting Subscriber, stored
as a snapshot (id and email at the time, so later removal or address changes
cannot rewrite history), or a distinct **CLI actor** for commands run locally
by the operator, whose OS user is captured in the event detail. The CLI has no
subscriber identity (ADR 0005: local, direct store access), so a separate
actor kind keeps the trail complete without forcing the operator to be a
member.

**Recording point**: actions that already funnel through the shared
`Pipeline` (moderation, subscription approval/rejection, member add/remove,
role grant/revoke) record the event inside the Pipeline, taking the actor from
the email/CLI/web caller, so the three surfaces cannot drift, and a failed
audit write fails the action there. Store-direct operations (domain/list
lifecycle, settings, allowlist, Administrators) record at the web handler and
CLI call site, mirroring how validation is already duplicated between those
two surfaces; the schema has no transaction spanning the state change and its
audit row, so an audit failure there is logged loudly rather than rolled back.

**Surfaces**: a per-list **Audit** tab in the web role console visible to
Owners only (Moderators keep the moderation-only boundary), an instance-wide
**Audit** tab in the server-admin area visible to Administrators only, and a
`xlistman audit` CLI command for parity. Both views are reverse-chronological
with an optional action filter.

**Storage**: a dedicated `audit_events` table (no foreign keys, matching the
schema). Never pruned. `DeleteList` deliberately does not remove audit rows.

**Considered Options**:
- Recording only held-message moderation actions — rejected: the stated value
  is accountability for destructive operations, and a trail that records a
  moderator's discard but not a server admin's list deletion would leave the
  most destructive operation unrecorded.
- Recording automated and self-service events (full state-change history) with
  a system actor — rejected: it dilutes the trail's signal with noise and adds
  a special actor type, for little accountability value.
- Requiring the server operator to designate a Subscriber as the CLI identity —
  rejected: forces the operator to be a member, adds configuration, and breaks
  when that subscriber is removed; a distinct CLI actor is simpler and honest.
- Best-effort audit writes — rejected: a silent recording failure would
  undermine the feature's purpose. Pipeline-mediated actions fail when their
  audit write fails; store-direct operations, which cannot span the state
  change and its audit row in one transaction, log a loud error instead.
- A moderation-only surface (no instance-wide view) — rejected: instance-level
  events (domain create/delete, admin designate/revoke) and a deleted list's
  history are only meaningful to Administrators.
