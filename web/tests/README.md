# Frontend test suite (agent-executed, via chrome-devtools MCP)

This is a suite of **agent-executed** frontend tests for xListman. Each test is
one Markdown file in this directory. An agent runs them against a live browser
through the chrome-devtools MCP server, asserting on **text** (a11y snapshots,
DOM text, network, console) — never on screenshots. This keeps the suite
runnable by text-only models.

## How to run

1. `make e2e` — builds the binary, wipes/creates a fresh DB
   (`/tmp/xlistman-e2e.db`), starts the daemon on `http://localhost:8090`
   (config `e2e.yaml`, sink mail in `/tmp/xlistman-e2e-mail`), seeds fixtures,
   and prints an agent prompt.
2. Tell the agent to run the suite: *"Execute `web/tests/t*.md` in order
   against `http://localhost:8090` and write the report to
   `web/tests/report.md`."* The agent drives Chrome via the chrome-devtools MCP
   tools.
3. `make e2e-summary` — parses `web/tests/report.md` into PASS/FAIL totals and
   exits non-zero if anything failed.

Run `make web` first if you changed `web/src` since the last build (the SPA is
embedded into the binary from `web/build`).

The browser, daemon, and seeded data persist between tests; `scripts/e2e.sh
stop` tears the daemon down, `setup` starts a fresh one.

## Scenario file format

Each test is one Markdown file:

- `## <test name>` — top-level heading naming the test (the report uses it).
- A short description.
- `## Setup` *(optional)* — numbered terminal commands the agent runs **before**
  the browser steps. This is where throwaway fixtures are minted (per-test
  isolation, so tests stay order-independent) and audit events are checked.
- `## Steps` — a numbered list of steps, each one an **action** or an
  **assertion** from the vocabulary below.

Setup commands use the `run:` form:

| Setup step | What the agent does to pass it |
| --- | --- |
| `run: <command>` | Run the command in the terminal. PASS if it exits 0. |
| `run: <command> (expect: <text>)` | Run the command; PASS if it exits 0 **and** its output contains `<text>`. Used for audit-verification steps, e.g. `run: ./xlistman audit list mod@lists.test moderation.reject (expect: Reject me)`. |

Fixture helpers: `./scripts/e2e-post.sh <list> <from> <subject> <body>`
(inject a real post via the mail pipeline), `./scripts/e2e-confirm.sh <email>
<list>` (complete double opt-in by replying to the confirmation email),
`go run ./cmd/e2eseed disabled <list> <email>` (throwaway Disabled member),
`go run ./cmd/e2eseed held-sub <list> <email>` (throwaway Held subscription).

YAML front-matter is optional metadata (kept for humans; the agent follows the
steps literally).

## Vocabulary

The agent must translate each step into the right chrome-devtools MCP call and
verify it against the live page. Interpret steps exactly; do not improvise
assertions that are not written.

### Actions

| Step | What the agent does |
| --- | --- |
| `login as <email>` | The full magic-link flow, defined below. |
| `ensure anonymous` | Go to `/auth`. If the page shows "You're signed in", click **Sign out** in the header and wait for the sign-in form. |
| `navigate to <path>` | `navigate_page` to `http://localhost:8090<path>`. Wait for the page to settle (network quiet + content present). |
| `fill <label> with <value>` | `take_snapshot`, find the input whose accessible label matches `<label>` (e.g. "Email address", "Name"), `fill` it. |
| `select <label> to <value>` | `take_snapshot`, find the native `<select>` whose accessible label matches `<label>`, `fill` it with the option `<value>`. |
| `check <text>` / `uncheck <text>` | `take_snapshot`, find the checkbox whose associated/adjacent label text contains `<text>` (e.g. "Moderation"), `click` it to toggle the desired state. |
| `upload <path> to <label>` | `take_snapshot`, find the file input whose accessible label matches `<label>` (e.g. "CSV file"), `upload_file` the given path. |
| `click <text>` | `take_snapshot`, find the clickable element whose accessible text **contains** `<text>` (button, link, tab; prefer the smallest such element), `click` it. |
| `click <text> in card containing <other text>` | As above, but first find the card containing `<other text>` and click the `<text>` element inside it (disambiguates the per-subscription Delivery buttons). |
| `click <text> in row containing <other text>` | As above, but scope to the table row (`<tr>`) containing `<other text>`. |
| `press <key>` | `press_key` (e.g. `Enter`). |
| `reload` | `navigate_page` with `reload`. |
| `emulate viewport <W>x<H>` | `emulate` with `viewport=<W>x<H>`. |
| `reset viewport` | `emulate` with an empty `viewport`. |

### Assertions

| Step | What the agent does to pass it |
| --- | --- |
| `expect text "<t>" to appear` | The exact text `<t>` appears in the page (a11y snapshot or `document.body.innerText`). Wait/retry up to ~5s for async renders. |
| `expect text "<t>" to be absent` | The exact text `<t>` does **not** appear in the page. |
| `expect text "<t>" in row containing "<o>" to appear` / `to be absent` | Scope to the table row containing `<o>` and check `<t>` appears / is absent within it. |
| `expect <n> occurrences of "<t>" in row containing "<o>"` | In the row containing `<o>`, count exact occurrences of `<t>` in its text (e.g. a role badge plus its toggle button both render "Moderator"). |
| `expect button "<t>" to be disabled` / `to be enabled` | The button whose accessible text contains `<t>` has / lacks the `disabled` attribute (from the a11y snapshot; fall back to `evaluate_script` on `disabled` if the snapshot omits it). |
| `expect page URL to contain <path>` | `location.pathname` (or the URL) contains `<path>`. |
| `expect input <label> to have value <v>` | The input labeled `<label>` currently has value `<v>`. |
| `expect <n> console errors` | `list_console_messages` filtered to `error` returns exactly `<n>` messages since navigation. |
| `expect request /api/<path> to return <status>` | In `list_network_requests`, a request whose URL matches `/api/<path>` returned HTTP `<status>`. |
| `expect response of request /api/<path> to contain "<t>"` | Find the request whose URL matches `/api/<path>` in `list_network_requests`, fetch its response body via `get_network_request`, and check it contains `<t>` (e.g. the CSV export body). |
| `expect page to fit viewport` | `evaluate_script`: `document.documentElement.scrollWidth <= document.documentElement.clientWidth` (allows ±1px). |

### Login step (the only composite action)

`login as <email>` is defined as:

1. `ensure anonymous` (sign out if a session exists).
2. `navigate to /auth`.
3. `fill "Email address" with <email>`.
4. `click "Send login link"`.
5. `expect text "Check your inbox" to appear`.
6. Run `scripts/e2e-get-link.sh <email> --wait` from the terminal to get the
   `/api/auth/verify?token=...` URL.
7. `navigate to` that full URL (it redirects to `/` and sets the session).
8. `expect text "My subscriptions" to appear` (header shows a signed-in user).

Fixture addresses are distinct per role, so the per-email magic-link rate limit
never trips across tests.

### Implicit invariants

Every test ends with:

- `expect 0 console errors`
- `expect no API request to return a 5xx` — i.e., scan `list_network_requests`;
  if any `/api/...` request returned 5xx, the step fails. (A 401/403 is fine;
  those are asserted deliberately by gate tests.)

## Report format

The agent writes `web/tests/report.md`, appending as it goes:

- Per step: `- [PASS] <step>` or `- [FAIL] <step> — expected <…>, found <…>`.
- At the end of each test: `## <test name> — PASS` or `## <test name> — FAIL`.
- Last line of the file: `# Suite: <N> passed, <M> failed`.

`scripts/e2e.sh summary` parses this; a FAIL anywhere makes it exit non-zero.

## Seeded fixtures (fresh per run)

Domain `lists.test`. Lists: `dev@lists.test` (discussion, open, with 3 archive
posts), `mod@lists.test` (discussion, **moderated**, 1 held message from
`stranger@lists.test`), `announce@lists.test` (newsletter).

- `admin@lists.test` — server Administrator.
- `owner@lists.test` — Owner of dev, mod, and announce.
- `moderator@lists.test` — Moderator of mod.
- `member@lists.test` — plain member of dev and mod.
- `sender@lists.test` — designated sender on announce.
- `disabled@lists.test` — Disabled member of dev (bounce count at threshold).
- `heldsub@lists.test` — Held subscription on mod (awaiting owner approval).

## Troubleshooting

- `scripts/e2e.sh status` — is the daemon up?
- `tail /tmp/xlistman-e2e.log` — daemon log (SMTP sink writes go to
  `/tmp/xlistman-e2e-mail`).
- On a FAIL the agent records the actual page text and console errors; the
  environment is left up so it can be inspected.
