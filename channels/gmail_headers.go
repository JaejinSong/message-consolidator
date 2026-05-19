package channels

import (
	"context"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
	"mime"
	"net/mail"
	"strings"

	"google.golang.org/api/gmail/v1"
)

func extractHeaders(headers []*gmail.MessagePartHeader) (subject, from, to, cc, bcc, deliveredTo string) {
	for _, h := range headers {
		switch h.Name {
		case "Subject":
			subject = h.Value
		case "From":
			from = h.Value
		case "To":
			to = h.Value
		case "Cc":
			cc = h.Value
		case "Bcc":
			bcc = h.Value
		case "Delivered-To":
			deliveredTo = h.Value
		}
	}
	return
}

// parseAddrLower extracts the bare email address from a header value
// (display-name and surrounding whitespace stripped) and lowercases it.
// Returns empty string on parse failure or empty input.
func parseAddrLower(header string) string {
	if header == "" {
		return ""
	}
	addr, err := mail.ParseAddress(header)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(addr.Address))
}

// classifyGmail maps from/to flags to a display category string.
func classifyGmail(isFromMe, isTo bool) string {
	if isFromMe {
		return CategorySent
	} else if isTo {
		return CategoryMine
	}
	return CategoryOthers
}

// Why: Determines the relationship between the user and the email headers (To, Cc, Bcc, Delivered-To) to decide how the email should be classified and prioritized.
func checkRecipientStatus(email, from, to, cc, bcc, deliveredTo string) (isFromMe, isDirect, isCc, isBcc, isDelTo bool) {
	emailLower := strings.ToLower(email)
	isFromMe = strings.Contains(strings.ToLower(from), emailLower)
	isDirect = strings.Contains(strings.ToLower(to), emailLower)
	isCc = strings.Contains(strings.ToLower(cc), emailLower)
	isBcc = strings.Contains(strings.ToLower(bcc), emailLower)
	isDelTo = strings.Contains(strings.ToLower(deliveredTo), emailLower)
	return
}

// upsertAddresses parses a comma-separated list of email addresses and registers each one in the contacts store.
// It returns the (email, displayName) of the first parsed contact.
func upsertAddresses(ctx context.Context, tenantEmail, header, source string) (string, string) {
	if header == "" {
		return "", ""
	}

	//Why: Parses standard RFC 5322 format for multiple addresses and ensures display names are correctly decoded from MIME encoding.
	contacts, err := mail.ParseAddressList(header)
	if err != nil {
		logger.Debugf("[GMAIL] Failed to parse address list: %v", err)
		return types.ExtractNameFromEmail(header), ""
	}

	dec := new(mime.WordDecoder)
	firstEmail, firstName := "", ""

	for _, addr := range contacts {
		email := strings.ToLower(strings.TrimSpace(addr.Address))
		if email == "" {
			continue
		}
		name := addr.Name
		if decoded, err := dec.DecodeHeader(name); err == nil {
			name = decoded
		}
		if firstEmail == "" {
			firstEmail, firstName = email, name
		}
		_ = store.AutoUpsertContact(ctx, tenantEmail, email, name, source)
	}

	if firstEmail != "" {
		return firstEmail, firstName
	}
	return types.ExtractNameFromEmail(header), ""
}

// isInternalSender reports whether the From header's email domain belongs to an
// internal domain (exact match or subdomain). Used to scope the internal-List-ID
// marketing exemption to in-house senders only — external advertisers routing
// through an internal Google Group must not inherit the exemption.
func isInternalSender(fromHeader string, internalDomains []string) bool {
	if fromHeader == "" || len(internalDomains) == 0 {
		return false
	}
	addr, err := mail.ParseAddress(fromHeader)
	if err != nil {
		return false
	}
	at := strings.LastIndex(addr.Address, "@")
	if at < 0 {
		return false
	}
	domain := strings.ToLower(strings.TrimSpace(addr.Address[at+1:]))
	if domain == "" {
		return false
	}
	for _, d := range internalDomains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" {
			continue
		}
		if domain == d || strings.HasSuffix(domain, "."+d) {
			return true
		}
	}
	return false
}
