# Operational: server administration (domains, lists, administrators)

Administrator operational functions: creating a domain, creating and deleting
a list (with the typed-address confirm guard), changing a list type (with the
warning dialog), and designating/revoking an Administrator. Runs on throwaway
entities; every privileged action is verified against the audit trail.

## Setup

1. `run: ./xlistman list create opsx@lists.test --type discussion --desc "admin fixture list"`
2. `run: ./xlistman subscriber add opsx@lists.test opsadmin@lists.test`

## Steps

1. `login as admin@lists.test`
2. `navigate to /server/domains`
3. `expect text "Add a domain" to appear`
4. `fill "Domain" with ops.example`
5. `fill "Description" with e2e domain`
6. `click "Add domain"`
7. `expect text "ops.example" to appear`
8. `run: ./xlistman audit server domain.create (expect: ops.example)`
9. `navigate to /server/lists`
10. `fill "Name" with del`
11. `click "Create list"`
12. `expect text "del@lists.test" to appear`
13. `click "Make newsletter" in row containing "del@lists.test"`
14. `expect text "Change del@lists.test to Newsletter?" to appear`
15. `click "Change type"`
16. `expect text "Make discussion" in row containing "del@lists.test" to appear`
17. `run: ./xlistman audit server list.type (expect: del@lists.test)`
18. `click "Delete" in row containing "del@lists.test"`
19. `expect text "Delete del@lists.test permanently?" to appear`
20. `expect button "Delete permanently" to be disabled`
21. `fill "Type del@lists.test to confirm" with del@lists.test`
22. `expect button "Delete permanently" to be enabled`
23. `click "Delete permanently"`
24. `expect text "del@lists.test" to be absent`
25. `run: ./xlistman audit server list.delete (expect: del@lists.test)`
26. `navigate to /server/administrators`
27. `expect text "Designate an Administrator" to appear`
28. `fill "Subscriber email" with opsadmin@lists.test`
29. `click "Add Administrator"`
30. `expect text "opsadmin@lists.test" to appear`
31. `run: ./xlistman audit server admin.designate (expect: opsadmin@lists.test)`
32. `click "Revoke" in row containing "opsadmin@lists.test"`
33. `expect text "opsadmin@lists.test" to be absent`
34. `run: ./xlistman audit server admin.revoke (expect: opsadmin@lists.test)`
35. `expect 0 console errors`
36. `expect no API request to return a 5xx`
