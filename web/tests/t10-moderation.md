# Operational: moderation reject and discard

Owner operational functions on the Moderation tab: rejecting and discarding
held messages (approve is covered by t05). Throwaway held posts are minted via
the mail pipeline; each action is verified against the audit trail.

## Setup

1. `run: ./scripts/e2e-post.sh mod@lists.test stranger2@lists.test "Reject me" "Please reject this message."`
2. `run: ./scripts/e2e-post.sh mod@lists.test stranger3@lists.test "Discard me" "Please discard this message."`

## Steps

1. `login as owner@lists.test`
2. `navigate to /admin/l/mod@lists.test/moderation`
3. `expect text "Reject me" to appear`
4. `expect text "Discard me" to appear`
5. `click "Reject me"`
6. `expect page URL to contain /held/`
7. `click "Reject"`
8. `expect text "Message rejected" to appear`
9. `navigate to /admin/l/mod@lists.test/moderation`
10. `expect text "Reject me" to be absent`
11. `run: ./xlistman audit list mod@lists.test moderation.reject (expect: Reject me)`
12. `click "Discard me"`
13. `expect page URL to contain /held/`
14. `click "Discard"`
15. `expect text "Message discarded" to appear`
16. `navigate to /admin/l/mod@lists.test/moderation`
17. `expect text "Discard me" to be absent`
18. `run: ./xlistman audit list mod@lists.test moderation.discard (expect: Discard me)`
19. `expect 0 console errors`
20. `expect no API request to return a 5xx`
