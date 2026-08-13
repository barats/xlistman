# xListman

A one-binary mailing list manager that integrates with an MTA to manage mailing lists, subscriptions, archives, and administration. An alternative to GNU Mailman with a single-binary deployment model.

## Language

**Domain**:
A virtual email domain hosted by an xListman instance (e.g., `example.com`). An instance can host multiple domains.
_Avoid_: Virtual host, mail domain

**List**:
A mailing list, identified by the pair `(listname, domain)` (e.g., `dev@example.com`).
_Avoid_: Mailing list entry, list instance

**Subscriber**:
A verified email address known to xListman. The primary identity for subscriptions, ownership, and moderation. A Subscriber does not need to be subscribed to any list (e.g., an owner who receives no posts).
_Avoid_: User, account, member (use Member for the subscription role specifically)

**Subscription**:
The relationship between a Subscriber and a List that causes posts to be delivered to that address. Carries delivery preferences (e.g., regular, digest).
_Avoid_: Membership, signup

**Owner**:
A Subscriber with administrative authority over a List: configuration, membership management, and moderation. Separate from Subscription.
_Avoid_: Admin, list manager

**Moderator**:
A Subscriber who can approve or reject held messages on a List, but cannot change list configuration.
_Avoid_: Approver, reviewer

**Member**:
A Subscriber who has a Subscription to a List. Used informally to mean "someone subscribed to a list."
_Avoid_: Subscriber (use Subscriber for the entity, Member for the role)

**ListType**:
A property of a List that determines who can post and how posts are handled. Two types: Discussion and Newsletter.
_Avoid_: List category, list mode

**Discussion**:
A ListType for two-way lists. Subscribers can post. Has a single "moderation" toggle: when off, subscriber posts are delivered immediately and non-subscriber posts are rejected; when on, all posts are held for moderator approval.

**Newsletter**:
A ListType for one-way lists. Only designated senders (owners or an allowlist) can post; all other posts are rejected. Subscribers receive but cannot post.

**Held Message**:
A post awaiting moderator approval, stored in a per-list moderation queue.
_Avoid_: Pending message, queued message

**Bounce**:
A delivery failure notification received for a Subscriber's address. Tracked per Subscription via VERP (Variable Envelope Return Path), which encodes the recipient in the envelope sender so bounces can be attributed to a specific Subscription.

**Disabled Subscription**:
A Subscription automatically deactivated due to excessive consecutive bounces (configurable threshold, default 5). The Subscriber can re-enable it via the web UI or email command. Not deleted.
_Avoid_: Suspended subscription, blocked subscription

**Archive**:
The stored history of posts to a List, browsable by Members via the web UI as threaded conversations with full-text search. Always members-only; no public access. Retained indefinitely by default, with an optional configurable max age per list.
_Avoid_: Message store, history

**Digest**:
A batched compilation of posts to a List, sent to Subscribers whose delivery preference is set to digest. MIME multipart/digest format. Frequency is per-list (daily or weekly). Sent at a fixed time when the period elapses, only if there are new messages.
_Avoid_: Summary, roundup

**Nomail**:
A delivery preference that pauses message delivery without unsubscribing. The Subscriber remains a Member but receives no posts until they re-enable delivery. Useful for vacations or temporary email issues.
_Avoid_: Vacation mode, delivery pause

**Subscription Policy**:
A property of a List that determines how subscription requests are handled after double opt-in confirmation. Three options: Open (subscription activates immediately), Moderated (held for owner approval), Closed (no new subscriptions; owners add subscribers manually).
_Avoid_: Join policy, admission policy

**Designated Sender**:
A Subscriber authorized to post to a Newsletter list, in addition to owners. Managed via a per-list allowlist.
_Avoid_: Authorized poster, allowed sender

**Outbound Queue**:
A persistent queue of messages pending delivery to the MTA, stored in the database. All outbound mail (posts, digests, notifications, confirmations) flows through it. A background worker sends via SMTP with exponential backoff on failure. After max retries, the message is bounced to the original sender.
_Avoid_: Send queue, delivery queue
