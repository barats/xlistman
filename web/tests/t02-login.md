# Magic-link login (the login helper, exercised end to end)

Verifies the passwordless login flow: requesting a link on the auth page,
extracting the emailed verify URL from the sink dir, following it, and landing
signed in.

## Steps

1. `login as member@lists.test`
2. `expect text "My subscriptions" to appear`
3. `navigate to /auth`
4. `expect text "You're signed in" to appear`
5. `expect text "Signed in as member@lists.test" to appear`
6. `expect 0 console errors`
7. `expect no API request to return a 5xx`
