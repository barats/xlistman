# Gates: anonymous 401s, role-gated nav, and mobile viewport

Verifies the auth boundary: anonymous users are blocked from members-only and
console routes, a signed-in member without roles sees neither "My lists" nor
"Server", and the public index fits a mobile viewport.

## Steps

1. `ensure anonymous`
2. `navigate to /me`
3. `expect text "Sign in required" to appear`
4. `navigate to /admin`
5. `expect text "Sign in required" to appear`
6. `navigate to /server`
7. `expect text "Administrator required" to appear`
8. `navigate to /l/dev@lists.test/archives`
9. `expect text "Members only" to appear`
10. `expect text "Sign in" to appear`
11. `navigate to /admin/l/dev@lists.test/settings`
12. `expect text "Not authorized" to appear`
13. `login as member@lists.test`
14. `expect text "My subscriptions" to appear`
15. `expect text "My lists" to be absent`
16. `expect text "Server" to be absent`
17. `navigate to /admin`
18. `expect text "You don't hold a role on any list yet." to appear`
19. `navigate to /server`
20. `expect text "Administrator required" to appear`
21. `navigate to /admin/l/dev@lists.test`
22. `expect text "Not authorized" to appear`
23. `emulate viewport 390x844`
24. `navigate to /`
25. `expect page to fit viewport`
26. `reset viewport`
27. `expect 0 console errors`
28. `expect no API request to return a 5xx`
