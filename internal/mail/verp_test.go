package mail

import (
	"testing"
)

func TestEncodeVERP(t *testing.T) {
	tests := []struct {
		name     string
		list     string
		recipient string
		want     string
	}{
		{
			name:      "basic",
			list:      "dev@example.com",
			recipient: "alice@work.com",
			want:      "dev-bounces+alice=work.com@example.com",
		},
		{
			name:      "recipient subdomain",
			list:      "dev@example.com",
			recipient: "bob@mail.work.com",
			want:      "dev-bounces+bob=mail.work.com@example.com",
		},
		{
			name:      "list subdomain",
			list:      "announce@lists.example.com",
			recipient: "carol@example.org",
			want:      "announce-bounces+carol=example.org@lists.example.com",
		},
		{
			name:      "both subdomains",
			list:      "team@lists.example.com",
			recipient: "dave@sub.domain.org",
			want:      "team-bounces+dave=sub.domain.org@lists.example.com",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EncodeVERP(tc.list, tc.recipient)
			if err != nil {
				t.Fatalf("EncodeVERP(%q, %q) returned unexpected error: %v", tc.list, tc.recipient, err)
			}
			if got != tc.want {
				t.Errorf("EncodeVERP(%q, %q) = %q, want %q", tc.list, tc.recipient, got, tc.want)
			}
		})
	}
}

func TestEncodeVERP_Errors(t *testing.T) {
	tests := []struct {
		name      string
		list      string
		recipient string
	}{
		{"list missing @", "devexample.com", "alice@work.com"},
		{"recipient missing @", "dev@example.com", "alicework.com"},
		{"list empty local", "@example.com", "alice@work.com"},
		{"list empty domain", "dev@", "alice@work.com"},
		{"recipient empty local", "dev@example.com", "@work.com"},
		{"recipient empty domain", "dev@example.com", "alice@"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EncodeVERP(tc.list, tc.recipient)
			if err == nil {
				t.Errorf("EncodeVERP(%q, %q) = %q, want error", tc.list, tc.recipient, got)
			}
		})
	}
}

func TestDecodeVERP(t *testing.T) {
	tests := []struct {
		name          string
		envelope      string
		wantList      string
		wantRecipient string
	}{
		{
			name:          "basic",
			envelope:      "dev-bounces+alice=work.com@example.com",
			wantList:      "dev@example.com",
			wantRecipient: "alice@work.com",
		},
		{
			name:          "recipient subdomain",
			envelope:      "dev-bounces+bob=mail.work.com@example.com",
			wantList:      "dev@example.com",
			wantRecipient: "bob@mail.work.com",
		},
		{
			name:          "list subdomain",
			envelope:      "announce-bounces+carol=example.org@lists.example.com",
			wantList:      "announce@lists.example.com",
			wantRecipient: "carol@example.org",
		},
		{
			name:          "both subdomains",
			envelope:      "team-bounces+dave=sub.domain.org@lists.example.com",
			wantList:      "team@lists.example.com",
			wantRecipient: "dave@sub.domain.org",
		},
		{
			name:          "list name with hyphen",
			envelope:      "dev-team-bounces+eve=work.com@example.com",
			wantList:      "dev-team@example.com",
			wantRecipient: "eve@work.com",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotList, gotRecipient, err := DecodeVERP(tc.envelope)
			if err != nil {
				t.Fatalf("DecodeVERP(%q) returned unexpected error: %v", tc.envelope, err)
			}
			if gotList != tc.wantList {
				t.Errorf("DecodeVERP(%q) list = %q, want %q", tc.envelope, gotList, tc.wantList)
			}
			if gotRecipient != tc.wantRecipient {
				t.Errorf("DecodeVERP(%q) recipient = %q, want %q", tc.envelope, gotRecipient, tc.wantRecipient)
			}
		})
	}
}

func TestDecodeVERP_Errors(t *testing.T) {
	tests := []struct {
		name     string
		envelope string
	}{
		{"missing @", "dev-bounces+alice=work.com"},
		{"missing +", "dev-bouncesalice=work.com@example.com"},
		{"missing =", "dev-bounces+alicework.com@example.com"},
		{"empty list name", "-bounces+alice=work.com@example.com"},
		{"empty recipient local", "dev-bounces+=work.com@example.com"},
		{"empty recipient domain", "dev-bounces+alice=@example.com"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotList, gotRecipient, err := DecodeVERP(tc.envelope)
			if err == nil {
				t.Errorf("DecodeVERP(%q) = (%q, %q), want error", tc.envelope, gotList, gotRecipient)
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	tests := []struct {
		list      string
		recipient string
	}{
		{"dev@example.com", "alice@work.com"},
		{"announce@lists.example.com", "bob@mail.work.com"},
		{"team@lists.example.com", "carol@sub.domain.org"},
		{"dev-team@example.com", "dave@work.com"},
		{"news@example.co.uk", "eve@user.example.org"},
	}
	for _, tc := range tests {
		t.Run(tc.list+"->"+tc.recipient, func(t *testing.T) {
			encoded, err := EncodeVERP(tc.list, tc.recipient)
			if err != nil {
				t.Fatalf("EncodeVERP(%q, %q) returned unexpected error: %v", tc.list, tc.recipient, err)
			}
			gotList, gotRecipient, err := DecodeVERP(encoded)
			if err != nil {
				t.Fatalf("DecodeVERP(%q) returned unexpected error: %v", encoded, err)
			}
			if gotList != tc.list {
				t.Errorf("round-trip list = %q, want %q", gotList, tc.list)
			}
			if gotRecipient != tc.recipient {
				t.Errorf("round-trip recipient = %q, want %q", gotRecipient, tc.recipient)
			}
		})
	}
}
