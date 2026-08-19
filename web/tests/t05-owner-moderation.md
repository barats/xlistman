# Role console: owner sees My lists, approves a held message

As an owner of mod@lists.test, verifies the role console overview, opens a held
message from the moderation queue, approves it, and confirms it leaves the
queue.

## Steps

1. `login as owner@lists.test`
2. `navigate to /admin`
3. `expect text "My lists" to appear`
4. `expect text "dev@lists.test" to appear`
5. `expect text "mod@lists.test" to appear`
6. `expect text "announce@lists.test" to appear`
7. `click "mod@lists.test"`
8. `expect page URL to contain /admin/l/mod@lists.test`
9. `expect text "Overview" to appear`
10. `expect text "Held messages" to appear`
11. `click "Moderation"`
12. `expect text "Please review my proposal" to appear`
13. `click "Please review my proposal"`
14. `expect page URL to contain /held/`
15. `expect text "stranger@lists.test" to appear`
16. `click "Approve"`
17. `expect text "Message approved" to appear`
18. `navigate to /admin/l/mod@lists.test/moderation`
19. `expect text "No messages awaiting moderation." to appear`
20. `expect 0 console errors`
21. `expect no API request to return a 5xx`
