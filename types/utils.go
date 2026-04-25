package types

import (
	"mime"
	"net/mail"
	"strings"
)

// ExtractContacts robustly parses a comma-separated list of email addresses,
// decoding MIME headers and handling unquoted non-ASCII names.
func ExtractContacts(header string) []mail.Address {
	if header == "" {
		return nil
	}

	parser := mail.AddressParser{WordDecoder: &mime.WordDecoder{}}

	// 1. Try standard list parsing first (handles commas in quotes correctly)
	if addresses, err := parser.ParseList(header); err == nil {
		contacts := make([]mail.Address, len(addresses))
		for i, addr := range addresses {
			contacts[i] = *addr
		}
		return contacts
	}

	// 2. Fallback for mixed or slightly malformed headers (splitting cautiously)
	var contacts []mail.Address
	parts := strings.Split(header, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		addr, err := parser.Parse(p)
		if err == nil {
			contacts = append(contacts, *addr)
			continue
		}

		// Robust fallback for "Name <email>" even if slightly malformed
		idx := strings.Index(p, "<")
		if idx == -1 {
			contacts = append(contacts, mail.Address{Name: "", Address: p})
			continue
		}
		contacts = append(contacts, parseNameAndEmail(p, idx))
	}
	return contacts
}

func parseNameAndEmail(p string, idx int) mail.Address {
	name := strings.Trim(strings.TrimSpace(p[:idx]), "\"")
	dec := new(mime.WordDecoder)
	if decoded, err := dec.DecodeHeader(name); err == nil {
		name = decoded
	}
	endIdx := strings.Index(p, ">")
	email := ""
	if endIdx > idx {
		email = strings.TrimSpace(p[idx+1 : endIdx])
	} else {
		email = strings.TrimSpace(p[idx+1:])
	}
	return mail.Address{Name: name, Address: email}
}

// ExtractNameFromEmail parses an email address header (e.g., "Name <email@example.com>")
// and returns the display name or the email address itself if the name is empty.
// Why: Standardizes name extraction across Gmail, Slack, and other sources to ensure consistent UI display.
func ExtractNameFromEmail(header string) string {
	contacts := ExtractContacts(header)
	if len(contacts) > 0 {
		if contacts[0].Name != "" {
			return contacts[0].Name
		}
		return contacts[0].Address
	}
	return ""
}
