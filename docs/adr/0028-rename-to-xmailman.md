# Rename to xMailman

The product was originally named xListman. This ADR records the decision to
rename it to xMailman as a full identity change, and the rules that came with
it.

**Decision — the product is now xMailman, everywhere.** The rename is a full
identity change, not a display-name rebrand: the Go module path
(`github.com/barats/xmailman`), the binary (`xmailman`), the environment
variable prefix (`XMAILMAN_*`), the default config file (`xmailman.yaml`), the
systemd unit and user (`xmailman`), the Docker entrypoint and image
(`ghcr.io/barats/xmailman`), the web site name (`xMailman`), and the CLI
help/version strings all use the new name. The web meta tag is
`xmailman-site-name`.

**Decision — clean break, no aliases.** No `xlistman`-named compatibility
shims are kept: no legacy config filename fallback, no `XLISTMAN_*` env prefix
support, no `xlistman` binary symlink, no old identifiers anywhere in the
tree. The product is pre-1.0 with a single maintainer; the next release
(v0.4.0) is the first under the new identity, and the changelog entry states
that the old names are gone.

**Decision — GitHub repository renamed in place.** `barats/xlistman` was
renamed to `barats/xmailman` on GitHub so that stars, issues, releases, go
imports, and clone URLs redirect; the empty `barats/xmailman` repository
created in anticipation of the move was deleted first to free the name.

**Decision — the name is an homage, stated explicitly.** "xMailman" is a
deliberate nod to GNU Mailman, and the README continues to describe the
product as "an alternative to GNU Mailman" rather than pretending the names
are unrelated. The one-binary, self-hosted design is the differentiation, not
the name.

**Considered Options**:
- **Display-name-only rebrand** (keep `xlistman` binary, env prefix, config
  path, and module) — rejected: it leaves two names in circulation and makes
  the real identifier (`xlistman`) contradict the visible brand.
- **New empty repository as the new home** (keep `barats/xlistman` as a stub,
  publish to `barats/xmailman` fresh) — rejected: it orphans stars, issues,
  releases, and go-import history; GitHub's in-place rename provides redirects
  for all of them.
- **Compatibility aliases for one release** (`XLISTMAN_*` / `xlistman.yaml`
  accepted alongside the new names, then dropped) — rejected: pre-1.0, a
  single maintainer, and no known external operators; aliases are ongoing
  product surface (tests, docs, two prefixes) rather than a courtesy.
- **Rename to something Mailman-free** — rejected: the homage is the point;
  keeping the README's explicit "alternative to GNU Mailman" framing addresses
  the search/identity confusion instead of dodging it.
