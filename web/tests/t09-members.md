# Operational: members, held subscriptions, roles, and CSV import/export

Owner operational functions on the Members tab: authoritative add/remove,
approve and reject held subscription requests, grant/revoke a role including
the last-owner guard, and CSV import/export. Throwaway addresses keep the
seed intact; privileged actions are verified against the audit trail.

## Setup

1. `run: go run ./cmd/e2eseed held-sub mod@lists.test heldsub2@lists.test`
2. `run: go run ./cmd/e2eseed held-sub mod@lists.test heldsub3@lists.test`
3. `run: printf 'imported1@lists.test\nimported2@lists.test\n' > "${TMPDIR:-/tmp}/xlistman-e2e-import.csv"`

## Steps

1. `login as owner@lists.test`
2. `navigate to /admin/l/dev@lists.test/members`
3. `expect text "Add a member" to appear`
4. `fill "Email" with alice@lists.test`
5. `click "Add member"`
6. `expect text "Member added." to appear`
7. `expect text "alice@lists.test" to appear`
8. `run: ./xlistman audit list dev@lists.test member.add (expect: alice@lists.test)`
9. `click "Moderator" in row containing "alice@lists.test"`
10. `expect 2 occurrences of "Moderator" in row containing "alice@lists.test"`
11. `run: ./xlistman audit list dev@lists.test role.grant (expect: alice@lists.test)`
12. `click "Moderator" in row containing "alice@lists.test"`
13. `expect 1 occurrence of "Moderator" in row containing "alice@lists.test"`
14. `click "Remove" in row containing "alice@lists.test"`
15. `expect text "alice@lists.test" to be absent`
16. `run: ./xlistman audit list dev@lists.test member.remove (expect: alice@lists.test)`
17. `click "Owner" in row containing "owner@lists.test"`
18. `expect text "cannot remove the last owner" to appear`
19. `run: ./xlistman owner list dev@lists.test (expect: owner@lists.test)`
20. `navigate to /admin/l/mod@lists.test/members`
21. `expect text "Awaiting approval" to appear`
22. `click "Approve" in row containing "heldsub2@lists.test"`
23. `fill "Search members" with heldsub2@lists.test`
24. `click "Search"`
25. `expect text "Active" to appear`
26. `expect text "Requested membership" to be absent`
27. `run: ./xlistman audit list mod@lists.test subscription.approve (expect: heldsub2@lists.test)`
28. `fill "Search members" with heldsub3@lists.test`
29. `click "Search"`
30. `expect text "No members match that search." to appear`
31. `navigate to /admin/l/mod@lists.test/members`
32. `click "Reject" in row containing "heldsub3@lists.test"`
33. `fill "Search members" with heldsub3@lists.test`
34. `click "Search"`
35. `expect text "No members match that search." to appear`
36. `run: ./xlistman audit list mod@lists.test subscription.reject (expect: heldsub3@lists.test)`
37. `navigate to /admin/l/dev@lists.test/members`
38. `click "Export"`
39. `expect request /api/console/lists/lists.test/dev/members/export to return 200`
40. `run: ./xlistman subscriber export dev@lists.test (expect: email,status,delivery_mode,roles)` (the CLI export shares the same CSV writer as the web export, so this verifies the CSV body the button downloads)
41. `upload ${TMPDIR:-/tmp}/xlistman-e2e-import.csv to "CSV file"` (resolve `$TMPDIR` via the shell; the browser MCP only permits file paths under the OS temp dir)
42. `click "Import"`
43. `expect text "Imported 2 members (skipped 0)." to appear`
44. `fill "Search members" with imported1@lists.test`
45. `click "Search"`
46. `expect text "imported1@lists.test" to appear`
47. `run: ./xlistman audit list dev@lists.test member.import (expect: imported1@lists.test)`
48. `expect 0 console errors`
49. `expect no API request to return a 5xx`
