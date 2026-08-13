package mail

import (
	"testing"
)

func TestParseAddress_Post(t *testing.T) {
	p, err := ParseAddress("dev@example.com")
	if err != nil {
		t.Fatalf("ParseAddress: %v", err)
	}
	if p.Type != AddressTypePost {
		t.Errorf("Type = %v, want AddressTypePost", p.Type)
	}
	if p.ListName != "dev" || p.Domain != "example.com" {
		t.Errorf("ListName=%q Domain=%q", p.ListName, p.Domain)
	}
	if p.ListAddr != "dev@example.com" {
		t.Errorf("ListAddr = %q", p.ListAddr)
	}
}

func TestParseAddress_Suffixed(t *testing.T) {
	tests := []struct {
		addr     string
		wantType AddressType
		wantList string
	}{
		{"dev-request@example.com", AddressTypeRequest, "dev"},
		{"dev-owner@example.com", AddressTypeOwner, "dev"},
		{"dev-subscribe@example.com", AddressTypeSubscribe, "dev"},
		{"dev-unsubscribe@example.com", AddressTypeUnsubscribe, "dev"},
		{"announce-request@lists.example.com", AddressTypeRequest, "announce"},
		{"dev-team-owner@example.com", AddressTypeOwner, "dev-team"},
	}
	for _, tc := range tests {
		t.Run(tc.addr, func(t *testing.T) {
			p, err := ParseAddress(tc.addr)
			if err != nil {
				t.Fatalf("ParseAddress: %v", err)
			}
			if p.Type != tc.wantType {
				t.Errorf("Type = %v, want %v", p.Type, tc.wantType)
			}
			if p.ListName != tc.wantList {
				t.Errorf("ListName = %q, want %q", p.ListName, tc.wantList)
			}
		})
	}
}

func TestParseAddress_Bounce(t *testing.T) {
	p, err := ParseAddress("dev-bounces+alice=work.com@example.com")
	if err != nil {
		t.Fatalf("ParseAddress: %v", err)
	}
	if p.Type != AddressTypeBounce {
		t.Errorf("Type = %v, want AddressTypeBounce", p.Type)
	}
	if p.ListName != "dev" {
		t.Errorf("ListName = %q, want %q", p.ListName, "dev")
	}
	if p.EncodedPart != "alice=work.com" {
		t.Errorf("EncodedPart = %q, want %q", p.EncodedPart, "alice=work.com")
	}
}

func TestParseAddress_Confirm(t *testing.T) {
	p, err := ParseAddress("dev-confirm+abc123@example.com")
	if err != nil {
		t.Fatalf("ParseAddress: %v", err)
	}
	if p.Type != AddressTypeConfirm {
		t.Errorf("Type = %v, want AddressTypeConfirm", p.Type)
	}
	if p.EncodedPart != "abc123" {
		t.Errorf("EncodedPart = %q, want %q", p.EncodedPart, "abc123")
	}
}

func TestParseAddress_Errors(t *testing.T) {
	tests := []string{
		"no-at-sign",
		"@example.com",
		"dev@",
	}
	for _, addr := range tests {
		t.Run(addr, func(t *testing.T) {
			_, err := ParseAddress(addr)
			if err == nil {
				t.Errorf("expected error for %q", addr)
			}
		})
	}
}
