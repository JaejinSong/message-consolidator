package types

import (
	"mime"
	"net/mail"
	"strings"
)

// ExtractNameFromEmail parses an email address header (e.g., "Name <email@example.com>")
// and returns the display name or the email address itself if the name is empty.
// Why: Standardizes name extraction across Gmail, Slack, and other sources to ensure consistent UI display.
func ExtractNameFromEmail(header string) string {
	if header == "" {
		return ""
	}
	addrs, err := mail.ParseAddressList(header)
	if err == nil && len(addrs) > 0 {
		if addrs[0].Name != "" {
			return addrs[0].Name
		}
		return addrs[0].Address
	}

	firstRecip := strings.Split(header, ",")[0]
	if idx := strings.Index(firstRecip, "<"); idx != -1 {
		return parseBracketedName(firstRecip, idx)
	}
	return strings.TrimSpace(firstRecip)
}

// parseBracketedName handles extraction when a name is accompanied by an email in brackets.
func parseBracketedName(firstRecip string, idx int) string {
	name := strings.TrimSpace(firstRecip[:idx])
	name = strings.Trim(name, "\"")
	if name != "" {
		// Why: Decodes MIME-encoded headers to ensure non-ASCII characters (like Korean names) are correctly displayed.
		dec := new(mime.WordDecoder)
		if decoded, err := dec.DecodeHeader(name); err == nil {
			return decoded
		}
		return name
	}
	
	endIdx := strings.Index(firstRecip, ">")
	if endIdx > idx {
		return strings.TrimSpace(firstRecip[idx+1 : endIdx])
	}
	return strings.TrimSpace(firstRecip)
}
