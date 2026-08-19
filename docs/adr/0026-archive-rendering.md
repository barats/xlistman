# MIME-aware archive rendering

The archive stored each post as its raw RFC 822/MIME bytes and served them
back verbatim; the web UI split headers from body with a literal
`\r\n\r\n` search and rendered the rest as plain text. Any multipart message —
an attachment, an HTML body, a forwarded or nested email — rendered as MIME
noise: boundary markers, `Content-Type` headers, base64 blobs. This ADR
records the decision to parse MIME server-side and render messages the way a
reader expects, across every surface that shows a message body.

**Decision — server-side parse to a structured view.** A single Go MIME parser
produces a normalized representation of a message: its readable `text/plain`
body, an optional `text/html` body, nested `message/rfc822` messages, and a
flat list of Attachments (ADR 0025) with name, size, content type, and a
members-only download. The archive detail API returns this structure; clients
render it, so the same parsing cannot drift across surfaces.

**Rendering rules.**
- Plain text by default; a "View HTML" toggle when both parts exist; an
  HTML-only message renders its HTML by default. All HTML is sanitized
  (`bluemonday`) — the archive is members-only, but it still renders untrusted
  sender-supplied HTML, so script/event stripping is mandatory.
- Nested `message/rfc822` parts render inline as quoted sub-messages
  (From/Date/Subject + body, recursively) so a forwarded email reads as a
  thread, with a depth cap of 3 before collapsing to a "view attached message"
  link.
- Attachments are listed under the body (name/size/type) and downloadable via a
  members-only endpoint that streams the specific part. No separate "archive
  keeps attachments" knob: the attachment policy already decides what lands in
  the archive.

**Search indexes extracted text.** FTS previously indexed raw message bytes, so
it matched MIME headers, boundary markers, and base64 noise while missing the
readable text inside encoded parts. Full-text search now indexes extracted
clean text (headers stripped, parts decoded, HTML reduced to text, attachments
skipped), stored at archive time; existing archive rows are backfilled once at
migration.

**Moderation surfaces reuse the model.** The moderator notification email
attaches the original message as an `.eml` file instead of inlining raw MIME
bytes, and the web held-message detail page renders with the same parsed view,
so a moderator judges a post by how it reads, not by its MIME source.

**Considered Options**:
- **Client-side MIME parsing** (send raw bytes to the browser, parse in JS) —
  rejected: heavier client, harder to test, and the extracted-text requirement
  for search needs server-side parsing anyway.
- **Plain-text-only rendering** (extract text, never render HTML) — rejected:
  it permanently degrades HTML-only posts; a plain-by-default/HTML-on-toggle
  model matches "correctly display the email" for both text-forward and
  HTML-forward lists.
- **Nested messages as downloadable `.eml` links only** — rejected: inline
  recursive rendering is the "correctly display" reading; the depth cap keeps
  pathological nesting bounded.
- **Leave search on raw bytes** — rejected: it returns MIME-junk matches and
  misses encoded content; the parser makes clean-text indexing cheap.
