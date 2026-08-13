package mail

import (
	"fmt"
	"strings"
)

// AddressType identifies the function of an email address handled by xListman.
type AddressType int

const (
	AddressTypePost AddressType = iota
	AddressTypeRequest
	AddressTypeOwner
	AddressTypeSubscribe
	AddressTypeUnsubscribe
	AddressTypeBounce
	AddressTypeConfirm
	AddressTypeUnknown
)

// ParsedAddress describes a recipient address routed to xListman.
type ParsedAddress struct {
	Type       AddressType
	ListName   string // local part before any suffix
	Domain     string
	ListAddr   string // listname@domain
	EncodedPart string // for bounces: recipient=domain; for confirm: token
}

// ParseAddress determines the function of an email address.
//
// Address conventions (dash-suffix):
//   listname@domain              - post to list
//   listname-request@domain      - email commands
//   listname-owner@domain        - contact owners
//   listname-subscribe@domain    - subscribe shortcut
//   listname-unsubscribe@domain  - unsubscribe shortcut
//   listname-bounces+enc@domain  - VERP bounce address
//   listname-confirm+token@domain - confirmation token
func ParseAddress(addr string) (ParsedAddress, error) {
	atIdx := strings.LastIndex(addr, "@")
	if atIdx < 0 {
		return ParsedAddress{}, fmt.Errorf("invalid address %q: missing @", addr)
	}
	local := addr[:atIdx]
	domain := addr[atIdx+1:]
	if local == "" || domain == "" {
		return ParsedAddress{}, fmt.Errorf("invalid address %q", addr)
	}

	// Check for plus-addressing (bounces, confirm).
	if plusIdx := strings.Index(local, "+"); plusIdx >= 0 {
		prefix := local[:plusIdx]
		encoded := local[plusIdx+1:]

		// Find the list name from the prefix (strip -bounces, -confirm suffix).
		if strings.HasSuffix(prefix, "-bounces") {
			listName := strings.TrimSuffix(prefix, "-bounces")
			return ParsedAddress{
				Type: AddressTypeBounce, ListName: listName, Domain: domain,
				ListAddr: listName + "@" + domain, EncodedPart: encoded,
			}, nil
		}
		if strings.HasSuffix(prefix, "-confirm") {
			listName := strings.TrimSuffix(prefix, "-confirm")
			return ParsedAddress{
				Type: AddressTypeConfirm, ListName: listName, Domain: domain,
				ListAddr: listName + "@" + domain, EncodedPart: encoded,
			}, nil
		}
	}

	// Check dash-suffix addresses.
	suffixes := []struct {
		suffix string
		at     AddressType
	}{
		{"-request", AddressTypeRequest},
		{"-owner", AddressTypeOwner},
		{"-subscribe", AddressTypeSubscribe},
		{"-unsubscribe", AddressTypeUnsubscribe},
	}
	for _, s := range suffixes {
		if strings.HasSuffix(local, s.suffix) {
			listName := strings.TrimSuffix(local, s.suffix)
			if listName == "" {
				continue
			}
			return ParsedAddress{
				Type: s.at, ListName: listName, Domain: domain,
				ListAddr: listName + "@" + domain,
			}, nil
		}
	}

	// No suffix: it's a post to the list.
	return ParsedAddress{
		Type: AddressTypePost, ListName: local, Domain: domain,
		ListAddr: addr,
	}, nil
}
