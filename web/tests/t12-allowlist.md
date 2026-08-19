# Operational: newsletter allowlist add/remove and subscriber-first guard

Owner operational functions on the Allowlist tab of a newsletter list: the
subscriber-first guard (unknown email is refused), designating a known
subscriber, and removing them. Runs on a throwaway newsletter list; the
actions are verified against the audit trail.

## Setup

1. `run: ./xlistman list create opsnl@lists.test --type newsletter --owner owner@lists.test --desc "newsletter fixture"`
2. `run: ./xlistman subscriber add opsnl@lists.test author@lists.test`

## Steps

1. `login as owner@lists.test`
2. `navigate to /admin/l/opsnl@lists.test/allowlist`
3. `expect text "Designated senders" to appear`
4. `expect text "No designated senders yet." to appear`
5. `fill "Subscriber email" with nobody@lists.test`
6. `click "Add sender"`
7. `expect text "unknown subscriber: nobody@lists.test" to appear`
8. `fill "Subscriber email" with author@lists.test`
9. `click "Add sender"`
10. `expect text "Sender added." to appear`
11. `expect text "author@lists.test" to appear`
12. `run: ./xlistman audit list opsnl@lists.test sender.add (expect: author@lists.test)`
13. `click "Remove" in row containing "author@lists.test"`
14. `expect text "Sender removed." to appear`
15. `expect text "No designated senders yet." to appear`
16. `run: ./xlistman audit list opsnl@lists.test sender.remove (expect: author@lists.test)`
17. `expect 0 console errors`
18. `expect no API request to return a 5xx`
