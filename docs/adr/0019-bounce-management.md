# Bounce management: re-enable, reset, and owner notification

Phase 10 gives list Owners a **Bounces** management surface. The backend already
tracks per-Subscription bounce counts (attributed via VERP) and auto-disables a
Subscription once its count reaches the list's `bounce_threshold` (default 5),
but the operator has no way to see which members are bouncing or to act on it —
a disabled member can only re-enable themselves, and an active member creeping
toward the threshold is invisible until delivery stops. This phase adds a
per-list Bounces tab (Owners only) listing members with bounce activity and
offering two actions — **Re-enable** and **Reset count** — with CLI parity
(`subscriber re-enable`, `subscriber reset-bounces`), and wires the previously
dormant `owner_auto_disable_notice` setting so Owners are emailed when a member
is auto-disabled.

**Semantics decision — re-enable resets the bounce counter.** The model
described the auto-disable trigger as "consecutive bounces," but the counter was
cumulative and never reset: a member disabled at the threshold who re-enabled
was immediately one bounce away from re-disablement, making re-enabling
meaningless. Re-enabling now sets the Subscription Active **and** resets the
bounce count to zero, on every re-enable path (self-service web, the email
`re-enable` command, and the new Owner Bounces tab). A re-enabled member gets a
fresh runway. This is a deliberate correction of the wording/behavior mismatch.

**Auto-disable notification.** `owner_auto_disable_notice` (default off) is now
wired: when a bounce auto-disables a Subscription, the list's Owners are emailed
(deduplicated) that the member was disabled after N bounces. The auto-disable
flow moves into the shared `Pipeline.RecordBounce` (increment → check threshold
→ disable → notify), centralizing a flow that previously lived inline in the
LMTP bounce handler and was untested.

**Audit.** Owner-originated re-enable and reset are privileged membership
changes, so they record `member.re-enable` / `member.reset-bounces` Audit Events
(ADR 0018 scope). Self-service re-enable stays unrecorded (ADR 0018 excludes
self-service actions). Automated bounce increments and the auto-disable itself
remain unrecorded (ADR 0018 excludes automated events).

**Considered Options**:
- Keeping the cumulative counter on re-enable, exposing only a separate Reset
  action — rejected: it leaves the broken self-service and email re-enable
  behavior in place, where a re-enabled member re-disables on the next single
  bounce.
- Always emailing Owners on auto-disable — rejected: the opt-in
  `owner_auto_disable_notice` setting already exists (default off); respecting
  it keeps operator choice.
- Only enhancing the existing Members tab with bounce actions — rejected: the
  Members tab already shows `bounce_count` and status, but a dedicated Bounces
  tab surfaces problem members and their actions in one place; Members keeps
  showing the counts as before.
- Leaving the auto-disable logic inline in the LMTP handler — rejected: moving
  it into the shared Pipeline makes the increment/disable/notify sequence
  testable and keeps the notice wiring from drifting.
