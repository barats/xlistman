# Server administration: overview, lists, and creating a list

As an Administrator, verifies the /server area, the seeded lists, and creates a
new list that then appears on the public index.

## Steps

1. `login as admin@lists.test`
2. `navigate to /server`
3. `expect text "Server administration" to appear`
4. `expect text "Overview" to appear`
5. `click "Lists"`
6. `expect text "Create a list" to appear`
7. `expect text "dev@lists.test" to appear`
8. `expect text "mod@lists.test" to appear`
9. `expect text "announce@lists.test" to appear`
10. `fill "Name" with ci`
11. `fill "Description" with Created by the e2e suite`
12. `click "Create list"`
13. `expect text "ci@lists.test" to appear`
14. `navigate to /`
15. `expect text "ci@lists.test" to appear`
16. `expect 0 console errors`
17. `expect no API request to return a 5xx`
