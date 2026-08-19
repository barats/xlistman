// Package members implements the shared CSV import/export logic for member
// migration (Phase 14). It is used by both the CLI and the web console so the
// two surfaces parse and serialize identically; the bulk-add semantics live in
// mail.Pipeline.ImportMembers.
package members

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	netmail "net/mail"
	"strings"
)

// ImportMaxRows caps a single CSV import so a huge upload cannot wedge the
// server. Files with more data rows are rejected whole, not truncated.
const ImportMaxRows = 10000

// MemberRow is one row of a member export.
type MemberRow struct {
	Email        string
	Status       string   // "" for a role holder who is not subscribed
	DeliveryMode string   // "" for a role holder who is not subscribed
	Roles        []string // list roles (owner, moderator, designated_sender)
}

// ExportCSV serializes member rows as a CSV file with a header row, stable
// column order (email,status,delivery_mode,roles), RFC 4180 quoting, and a
// trailing newline. Roles are semicolon-joined. Rows are emitted in the order
// given; callers sort beforehand for deterministic output.
func ExportCSV(rows []MemberRow) []byte {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.Write([]string{"email", "status", "delivery_mode", "roles"})
	for _, r := range rows {
		w.Write([]string{r.Email, r.Status, r.DeliveryMode, strings.Join(r.Roles, ";")})
	}
	w.Flush()
	return buf.Bytes()
}

// ImportSource is the parsed, validated output of a CSV import file.
type ImportSource struct {
	// Emails is the normalized (lowercased) list of addresses to import, in
	// file order. Non-email columns of an exported file are ignored, so an
	// export re-imports cleanly.
	Emails []string
	// Invalid counts the rows that were blank or had an unparseable email
	// and were skipped by parsing.
	Invalid int
}

// ParseImport reads a member CSV: either an exported file with an
// email,status,delivery_mode,roles header (header detected and skipped), or a
// bare list of one email per row. A UTF-8 BOM is stripped and CRLF tolerated
// (Excel-friendly). Blank rows are skipped without counting. A row whose
// first field is not a valid email is counted as Invalid. The file is
// rejected whole (an error) when it has more than ImportMaxRows data rows.
func ParseImport(r io.Reader) (*ImportSource, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read import file: %w", err)
	}
	// Strip a UTF-8 byte-order mark so Excel exports parse cleanly.
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	reader := csv.NewReader(bytes.NewReader(data))
	reader.FieldsPerRecord = -1 // rows may have any width (exports carry extra columns)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse CSV: %w", err)
	}

	start := 0
	if len(records) > 0 && strings.EqualFold(strings.TrimSpace(firstField(records[0])), "email") {
		start = 1 // header row
	}
	dataRows := len(records) - start
	if dataRows > ImportMaxRows {
		return nil, fmt.Errorf("import file has %d rows; the maximum is %d", dataRows, ImportMaxRows)
	}

	src := &ImportSource{}
	for _, rec := range records[start:] {
		raw := strings.TrimSpace(firstField(rec))
		if raw == "" {
			continue // blank row, not counted as invalid
		}
		email, ok := normalizeEmail(raw)
		if !ok {
			src.Invalid++
			continue
		}
		src.Emails = append(src.Emails, email)
	}
	return src, nil
}

// firstField returns the first column of a CSV record ("" when empty).
func firstField(rec []string) string {
	if len(rec) == 0 {
		return ""
	}
	return rec[0]
}

// normalizeEmail parses and lowercases an email address, accepting RFC 5322
// display forms ("Foo <foo@example.com>") the way the web layer does.
func normalizeEmail(raw string) (string, bool) {
	addr, err := netmail.ParseAddress(raw)
	if err != nil {
		return "", false
	}
	email := strings.ToLower(addr.Address)
	if email == "" {
		return "", false
	}
	return email, true
}

// ImportResult reports the outcome of a bulk import. Skipped is the sum of
// rows that were not added: Already-subscribed members, Disabled members
// (never re-enabled by import), and Invalid rows from parsing.
type ImportResult struct {
	Added    int
	Already  int
	Disabled int
	Invalid  int
}

// Skipped returns the total number of rows not added.
func (r ImportResult) Skipped() int { return r.Already + r.Disabled + r.Invalid }

// Detail is the human-readable audit detail ("added N, skipped M").
func (r ImportResult) Detail() string {
	return fmt.Sprintf("added %d, skipped %d", r.Added, r.Skipped())
}
