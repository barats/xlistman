# Operational: settings save, persistence, and validation guard

Owner operational functions on the Settings tab: editing settings, saving,
verifying persistence after reload, and a validation guard (negative numeric
setting). Runs on a throwaway list so the shared seed is untouched; the change
is verified against the audit trail.

## Setup

1. `run: ./xlistman list create ops@lists.test --type discussion --owner owner@lists.test --desc "settings fixture"`

## Steps

1. `login as owner@lists.test`
2. `navigate to /admin/l/ops@lists.test/settings`
3. `expect text "Identity" to appear`
4. `fill "Description" with Operational settings fixture`
5. `fill "Subject prefix" with [ops]`
6. `check "Moderation"`
7. `click "Save settings"`
8. `expect text "Settings saved." to appear`
9. `navigate to /admin/l/ops@lists.test/settings`
10. `expect input "Description" to have value "Operational settings fixture"`
11. `expect input "Subject prefix" to have value "[ops]"`
12. `run: ./xlistman audit list ops@lists.test settings.update (expect: subject_prefix)`
13. `fill "Bounces before auto-disable" with -1`
14. `click "Save settings"`
15. `expect text "numeric settings cannot be negative" to appear`
16. `expect 0 console errors`
17. `expect no API request to return a 5xx`
