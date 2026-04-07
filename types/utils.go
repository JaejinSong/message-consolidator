package types

import (
	"mime"
	"net/mail"
	"strings"
)

// ExtractContacts robustly parses a comma-separated list of email addresses,
// decoding MIME headers and handling unquoted non-ASCII names.
func ExtractContacts(header string) []mail.Address {
	var contacts []mail.Address
	if header == "" {
		return contacts
	}

	parser := mail.AddressParser{WordDecoder: &mime.WordDecoder{}}

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

		// Fallback for malformed addresses like "홍길동 <hong@example.com>"
		if idx := strings.Index(p, "<"); idx != -1 {
			name := strings.TrimSpace(p[:idx])
			name = strings.Trim(name, "\"")

			dec := new(mime.WordDecoder)
			if decoded, err := dec.DecodeHeader(name); err == nil {
				name = decoded
			}

			endIdx := strings.Index(p, ">")
			var email string
			if endIdx > idx {
				email = strings.TrimSpace(p[idx+1 : endIdx])
			} else {
				email = strings.TrimSpace(p[idx+1:])
			}
			contacts = append(contacts, mail.Address{Name: name, Address: email})
		} else {
			contacts = append(contacts, mail.Address{Name: "", Address: p})
		}
	}
	return contacts
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
