# Operational: subscription lifecycle (double opt-in, unsubscribe, re-enable)

Self-service operational functions as a subscriber: full double opt-in
subscription (web form → confirmation email → confirm via the mail pipeline →
Active in /me), unsubscribing, and re-enabling a Disabled subscription. All
actions use throwaway addresses so the shared seed stays intact.

## Setup

1. `run: go run ./cmd/e2eseed disabled dev@lists.test freshdis@lists.test`

## Steps

1. `ensure anonymous`
2. `navigate to /l/dev@lists.test`
3. `fill "Email address" with fresh@lists.test`
4. `click "Subscribe"`
5. `expect text "A confirmation email is on its way" to appear`
6. `run: ./scripts/e2e-confirm.sh fresh@lists.test dev@lists.test`
7. `login as fresh@lists.test`
8. `navigate to /me`
9. `expect text "dev@lists.test" to appear`
10. `expect text "Active" to appear`
11. `click "Unsubscribe" in card containing "dev@lists.test"`
12. `expect text "Unsubscribed on dev@lists.test." to appear`
13. `expect text "dev@lists.test" to be absent`
14. `ensure anonymous`
15. `login as freshdis@lists.test`
16. `navigate to /me`
17. `expect text "Bounces (5) disabled delivery" to appear`
18. `click "Re-enable"`
19. `expect text "Re-enabled on dev@lists.test." to appear`
20. `expect text "Active" to appear`
21. `expect 0 console errors`
22. `expect no API request to return a 5xx`
