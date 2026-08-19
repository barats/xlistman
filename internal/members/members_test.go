package members

import (
	"strings"
	"testing"
)

func TestExportCSV(t *testing.T) {
	rows := []MemberRow{
		{Email: "alice@example.com", Status: "active", DeliveryMode: "regular", Roles: nil},
		{Email: "owner@example.com", Status: "active", DeliveryMode: "digest", Roles: []string{"owner"}},
		{Email: "quote,@example.com", Status: "disabled", DeliveryMode: "nomail", Roles: []string{"moderator"}},
	}
	got := string(ExportCSV(rows))
	want := "email,status,delivery_mode,roles\n" +
		"alice@example.com,active,regular,\n" +
		"owner@example.com,active,digest,owner\n" +
		"\"quote,@example.com\",disabled,nomail,moderator\n"
	if got != want {
		t.Errorf("ExportCSV = %q, want %q", got, want)
	}
}

func TestParseImportHeaderFile(t *testing.T) {
	csv := "email,status,delivery_mode,roles\r\n" +
		"alice@example.com,active,regular,\r\n" +
		"\"Bob <bob@example.com>\",active,regular,\r\n" +
		"\r\n"
	src, err := ParseImport(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}
	if len(src.Emails) != 2 {
		t.Fatalf("emails = %d, want 2", len(src.Emails))
	}
	if src.Emails[0] != "alice@example.com" {
		t.Errorf("emails[0] = %q, want alice@example.com", src.Emails[0])
	}
	// Display form "Bob <bob@...>" is normalized to the bare lowercased address.
	if src.Emails[1] != "bob@example.com" {
		t.Errorf("emails[1] = %q, want bob@example.com", src.Emails[1])
	}
	if src.Invalid != 0 {
		t.Errorf("invalid = %d, want 0", src.Invalid)
	}
}

func TestParseImportBareEmails(t *testing.T) {
	// No header: every row is a member email. Blank lines and comment-free.
	src, err := ParseImport(strings.NewReader("alice@example.com\nBob@Example.COM\n\ncarol@example.com\n"))
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}
	if len(src.Emails) != 3 {
		t.Fatalf("emails = %d, want 3", len(src.Emails))
	}
	if src.Emails[1] != "bob@example.com" {
		t.Errorf("emails[1] = %q, want lowercased bob@example.com", src.Emails[1])
	}
}

func TestParseImportBOM(t *testing.T) {
	// A UTF-8 BOM before the header must be stripped.
	csv := "\xEF\xBB\xBFemail\none@example.com\n"
	src, err := ParseImport(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}
	if len(src.Emails) != 1 || src.Emails[0] != "one@example.com" {
		t.Errorf("emails = %v, want [one@example.com]", src.Emails)
	}
}

func TestParseImportInvalidAndBlank(t *testing.T) {
	csv := "email\nnot-an-email\nvalid@example.com\n   \n@missing\n"
	src, err := ParseImport(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}
	if len(src.Emails) != 1 || src.Emails[0] != "valid@example.com" {
		t.Errorf("emails = %v, want [valid@example.com]", src.Emails)
	}
	if src.Invalid != 2 {
		t.Errorf("invalid = %d, want 2 (bad + missing-local rows)", src.Invalid)
	}
}

func TestParseImportRowCap(t *testing.T) {
	var b strings.Builder
	b.WriteString("email\n")
	for i := 0; i <= ImportMaxRows; i++ {
		b.WriteString("u@example.com\n")
	}
	_, err := ParseImport(strings.NewReader(b.String()))
	if err == nil {
		t.Fatal("ParseImport: expected a row-cap error, got nil")
	}
	if !strings.Contains(err.Error(), "maximum") {
		t.Errorf("error = %q, want a message about the maximum row count", err.Error())
	}
}

func TestParseImportMalformedCSV(t *testing.T) {
	if _, err := ParseImport(strings.NewReader("\"unterminated\n")); err == nil {
		t.Fatal("ParseImport: expected a parse error for malformed CSV, got nil")
	}
}

func TestImportResult(t *testing.T) {
	r := ImportResult{Added: 3, Already: 2, Disabled: 1, Invalid: 1}
	if r.Skipped() != 4 {
		t.Errorf("Skipped = %d, want 4", r.Skipped())
	}
	if got := r.Detail(); got != "added 3, skipped 4" {
		t.Errorf("Detail = %q, want %q", got, "added 3, skipped 4")
	}
}
