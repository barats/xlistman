# Archives: browse, full-text search, and thread detail

As a member of dev@lists.test, verifies the members-only archive: the list
renders seeded posts, full-text search narrows results, and a thread detail
shows the message body.

## Steps

1. `login as member@lists.test`
2. `navigate to /l/dev@lists.test/archives`
3. `expect text "Archives" to appear`
4. `expect text "Welcome to the dev list" to appear`
5. `expect text "Maintenance window this weekend" to appear`
6. `fill "Search archives" with apricot`
7. `click "Search"`
8. `expect text "Re: Welcome to the dev list" to appear`
9. `expect text "Maintenance window this weekend" to be absent`
10. `click "Re: Welcome to the dev list"`
11. `expect page URL to contain /archives/`
12. `expect text "apricot release is on track" to appear`
13. `expect 0 console errors`
14. `expect no API request to return a 5xx`
