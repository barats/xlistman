# Web protecting: in-memory rate limiting for the web API

The web API's write endpoints had no throttling at all: the `rate_limits` config existed but was never enforced, so a flood of magic-link requests (or email-bombing a known subscriber's address through the MTA) hit the database and the outbound queue unbounded. We added in-memory, per-instance rate limiting to the web write endpoints: token-bucket limiters keyed per email address (magic-link sends, default 3/hour) and per client IP (magic-link 50/hour, subscribe 5/hour). Over-quota magic-link requests still return 202 — the endpoint must never leak which addresses are subscribers — while over-quota per-IP requests return 429 with a Retry-After header. The `golang.org/x/time/rate` library provides the buckets.

## Considered Options

- **In-memory, per-instance counters (chosen).** Cheap, zero DB load, absorbs bursts at whatever instance receives them. An explicit deviation from ADR 0008's "rate limiting counters are stored in the database" phrasing: DB-backed counters would add a write to the exact database this protects.
- **DB-backed counters (ADR 0008's original intent).** Multi-instance-safe and survives restarts, but self-defeating here. When multi-instance actually happens, rate limiting belongs in a reverse proxy / load balancer in front of the instances, not the app database.

## Consequences

- Per-email magic-link sending is capped (default 3/hour), so a single subscriber's inbox and the MTA are protected from link-storming; unknown addresses never count against the cap and over-quota requests are silently 202'd, preserving the anti-enumeration property.
- Per-IP ceilings stop raw endpoint floods before they reach the DB (the magic-link gate runs before any query; the subscribe gate runs before the list lookup).
- State resets on restart and is per-instance; this is acceptable for burst protection and is documented as the reason the limiter is not DB-backed.
- A known-subscriber address's quota can still be burned by an attacker who deliberately triggers three real sends; the cap keeps that at three emails, and the alternative (counting all requests) would let an attacker DoS a victim with unknown addresses — strictly worse.
