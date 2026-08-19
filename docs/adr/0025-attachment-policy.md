# Per-list attachment policy

Posts to a List can carry files as MIME parts, but xListman previously had no
notion of an attachment: messages were opaque bytes end to end, and the
per-list `max_message_size` setting (default 1 MB) was stored but never
enforced on the inbound path. This ADR records the decision to make attachment
handling an explicit per-list policy with real enforcement.

**Domain term.** An **Attachment** (CONTEXT.md) is a MIME part that carries a
`filename` (whether declared as `Content-Disposition: attachment` or `inline`)
or that is declared `Content-Disposition: attachment`. The message's primary
text body part is never an Attachment; any other such part is — inline images
with filenames count.

**Decision.** Three per-list settings, with permissive defaults so existing
lists never start rejecting posts on upgrade:
- `allow_attachments` (bool, default true) — whether the List accepts any
  Attachment.
- `max_attachment_size` (bytes, per single attachment, default 0 = unlimited) —
  enforced only when attachments are allowed.
- `max_message_size` (bytes, total message) — the existing setting, now finally
  enforced as the total-message cap, so the size story is coherent: a
  whole-message cap and a per-attachment cap.

**Permissive total-cap default.** The old `max_message_size` default was 1 MB
but was never enforced, so a post that large was always accepted in practice.
Enforcing it as-is would make existing default-configured lists suddenly start
rejecting >1 MB posts on upgrade. New lists therefore default to `0`
(unlimited), and existing lists whose stored value is still the old 1 MB
default are reset to `0` at migration. A list only enforces a cap its owner
actually chose.

**Violation handling — reject the whole message.** All three violations — a
disallowed attachment, an attachment over the per-attachment cap, or a message
over the total cap — reject the post with a notice to the sender naming the
reason. Rejection, not stripping, because it is consistent with the existing
posting-policy rejection path and never silently mangles a post the owner did
not approve.

**Enforcement point.** Checked once, at the top of `Pipeline.ProcessPost`, the
shared inbound funnel for both ListTypes and both LMTP and pipe modes. A held
post is checked when it arrives; it is not re-checked at approve time, so a
message accepted when received remains deliverable even if the policy changed
while it sat in the moderation queue.

**Considered Options**:
- **Strip attachments and deliver the text** — rejected: deleting MIME parts
  risks corrupting the message and silently delivers a partial post; a sender
  whose file was dropped would get no signal that it was removed.
- **Hold violating posts for moderation** — rejected: policy violations should
  be deterministic, not a per-message human judgment; moderation is for
  judgment calls about content, not for enforcing the list's configured rules.
- **Single attachment-size knob replacing `max_message_size`** — rejected: the
  total cap and the per-attachment cap answer different questions; both are
  needed and cheap.
- **Sum-of-attachments cap** — rejected: "limit size" most naturally means per
  file, and the total is already bounded by `max_message_size`.
