# Public list index, list detail, and subscribe form (anonymous)

Verifies the public, no-login surface: the list index lists all seeded lists,
a list detail renders its description, and the subscribe form double opt-in
request works.

## Steps

1. `ensure anonymous`
2. `navigate to /`
3. `expect text "Mailing lists" to appear`
4. `expect text "dev@lists.test" to appear`
5. `expect text "mod@lists.test" to appear`
6. `expect text "announce@lists.test" to appear`
7. `click "dev@lists.test"`
8. `expect page URL to contain /l/dev@lists.test`
9. `expect text "Development discussion list" to appear`
10. `expect text "Subscribe" to appear`
11. `fill "Email address" with stranger2@lists.test`
12. `click "Subscribe"`
13. `expect text "A confirmation email is on its way" to appear`
14. `expect 0 console errors`
15. `expect no API request to return a 5xx`
