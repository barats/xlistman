# Subscriber self-service: /me subscriptions, delivery prefs, held posts

As a plain member, verifies the subscriptions list, changing a delivery
preference (and back), and the empty held-posts section.

## Steps

1. `login as member@lists.test`
2. `navigate to /me`
3. `expect text "My subscriptions" to appear`
4. `expect text "Signed in as member@lists.test" to appear`
5. `expect text "dev@lists.test" to appear`
6. `expect text "mod@lists.test" to appear`
7. `click "Digest" in card containing "dev@lists.test"`
8. `expect text "Delivery set to digest on dev@lists.test." to appear`
9. `click "Regular" in card containing "dev@lists.test"`
10. `expect text "Delivery set to regular on dev@lists.test." to appear`
11. `expect text "Posts awaiting approval" to appear`
12. `expect text "No posts awaiting approval" to appear`
13. `expect 0 console errors`
14. `expect no API request to return a 5xx`
