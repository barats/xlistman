package mail

import (
	"fmt"
	"strings"
)

// VERP (Variable Envelope Return Path) encodes the recipient address in the
// envelope sender so bounces can be attributed to a specific subscription.
//
// The format is:
//
//	listname-bounces+recipient=recipientdomain@listdomain
//
// For example, the list dev@example.com sending to alice@work.com produces the
// envelope sender dev-bounces+alice=work.com@example.com.

// EncodeVERP builds a VERP envelope sender for a message sent from listAddr to
// recipientAddr.
func EncodeVERP(listAddr, recipientAddr string) (string, error) {
	listLocal, listDomain, found := strings.Cut(listAddr, "@")
	if !found {
		return "", fmt.Errorf("invalid list address %q: missing @", listAddr)
	}
	if listLocal == "" || listDomain == "" {
		return "", fmt.Errorf("invalid list address %q", listAddr)
	}

	recipLocal, recipDomain, found := strings.Cut(recipientAddr, "@")
	if !found {
		return "", fmt.Errorf("invalid recipient address %q: missing @", recipientAddr)
	}
	if recipLocal == "" || recipDomain == "" {
		return "", fmt.Errorf("invalid recipient address %q", recipientAddr)
	}

	return listLocal + "-bounces+" + recipLocal + "=" + recipDomain + "@" + listDomain, nil
}

// DecodeVERP reverses EncodeVERP: given a VERP envelope sender it returns the
// original list address and recipient address.
func DecodeVERP(envelopeSender string) (listAddr, recipientAddr string, err error) {
	atIdx := strings.LastIndex(envelopeSender, "@")
	if atIdx < 0 {
		return "", "", fmt.Errorf("invalid VERP envelope sender %q: missing @", envelopeSender)
	}
	localPart := envelopeSender[:atIdx]
	listDomain := envelopeSender[atIdx+1:]
	if localPart == "" || listDomain == "" {
		return "", "", fmt.Errorf("invalid VERP envelope sender %q", envelopeSender)
	}

	// The local part has the form {listname}-bounces+{recipientLocal}={recipientDomain}.
	// LastIndex is used so a list name that itself contains the "-bounces+" marker
	// (or ends in "-bounces") is still parsed correctly.
	const marker = "-bounces+"
	mIdx := strings.LastIndex(localPart, marker)
	if mIdx < 0 {
		return "", "", fmt.Errorf("invalid VERP envelope sender %q: missing -bounces+ separator", envelopeSender)
	}
	listLocal := localPart[:mIdx]
	recipientEncoded := localPart[mIdx+len(marker):]
	if listLocal == "" {
		return "", "", fmt.Errorf("invalid VERP envelope sender %q: empty list name", envelopeSender)
	}

	// The recipient's "@" was encoded as "=". Domain labels cannot contain "=",
	// so the last "=" separates the recipient local part from its domain.
	eqIdx := strings.LastIndex(recipientEncoded, "=")
	if eqIdx < 0 {
		return "", "", fmt.Errorf("invalid VERP envelope sender %q: missing = in recipient", envelopeSender)
	}
	recipLocal := recipientEncoded[:eqIdx]
	recipDomain := recipientEncoded[eqIdx+1:]
	if recipLocal == "" || recipDomain == "" {
		return "", "", fmt.Errorf("invalid VERP envelope sender %q", envelopeSender)
	}

	return listLocal + "@" + listDomain, recipLocal + "@" + recipDomain, nil
}
