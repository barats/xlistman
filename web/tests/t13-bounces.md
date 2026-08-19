# Operational: bounce management (re-enable and reset count)

Owner operational functions on the Bounces tab: re-enabling a Disabled member
and resetting a bounce count, with the effect reflected back in the Members
tab and recorded in the audit trail. Runs on a throwaway list.

## Setup

1. `run: ./xlistman list create opsb@lists.test --type discussion --owner owner@lists.test --desc "bounce fixture"`
2. `run: go run ./cmd/e2eseed disabled opsb@lists.test dave@lists.test`
3. `run: go run ./cmd/e2eseed disabled opsb@lists.test erin@lists.test`

## Steps

1. `login as owner@lists.test`
2. `navigate to /admin/l/opsb@lists.test/bounces`
3. `expect text "Bounces" to appear`
4. `expect text "dave@lists.test" to appear`
5. `expect text "erin@lists.test" to appear`
6. `click "Re-enable" in row containing "dave@lists.test"`
7. `expect text "Re-enabled dave@lists.test." to appear`
8. `expect text "dave@lists.test" to be absent`
9. `run: ./xlistman audit list opsb@lists.test member.re-enable (expect: dave@lists.test)`
10. `click "Reset count" in row containing "erin@lists.test"`
11. `expect text "Reset bounce count for erin@lists.test." to appear`
12. `run: ./xlistman audit list opsb@lists.test member.reset-bounces (expect: erin@lists.test)`
13. `navigate to /admin/l/opsb@lists.test/members`
14. `fill "Search members" with dave@lists.test`
15. `click "Search"`
16. `expect text "dave@lists.test" to appear`
17. `expect text "Active" to appear`
18. `expect 0 console errors`
19. `expect no API request to return a 5xx`
