package channels

import (
	"message-consolidator/logger"
	"net/mail"
	"strings"

	"google.golang.org/api/gmail/v1"
)

func hasLabel(labels []string, label string) bool {
	for _, l := range labels {
		if l == label {
			return true
		}
	}
	return false
}

func hasImportantLabel(labels []string) bool {
	return hasLabel(labels, "IMPORTANT") || hasLabel(labels, "STARRED")
}

// isMarketingHeader identifies promotional emails using standard headers like List-Unsubscribe and Precedence.
// Why: Internal Google Groups (e.g. indonesia@whatap.io) re-inject List-Unsubscribe on every member copy per RFC 2369;
// the exemption applies only when BOTH List-ID matches an internal domain AND the From sender is internal —
// otherwise an external advertiser routing through an internal group inherits the exemption and bypasses the cut.
func isMarketingHeader(headers []*gmail.MessagePartHeader, fromHeader string, internalDomains []string) bool {
	if hasInternalListID(headers, internalDomains) && isInternalSender(fromHeader, internalDomains) {
		return false
	}
	for _, h := range headers {
		if h.Name == "List-Unsubscribe" {
			return true
		}
		if h.Name == "Precedence" {
			val := strings.ToLower(h.Value)
			if val == "bulk" || val == "list" || val == "junk" {
				return true
			}
		}
	}
	return false
}

func hasInternalListID(headers []*gmail.MessagePartHeader, internalDomains []string) bool {
	if len(internalDomains) == 0 {
		return false
	}
	for _, h := range headers {
		if h.Name == "List-ID" && listIDMatchesAny(h.Value, internalDomains) {
			return true
		}
	}
	return false
}

func listIDMatchesAny(headerValue string, domains []string) bool {
	val := strings.ToLower(strings.TrimSpace(headerValue))
	if start := strings.LastIndex(val, "<"); start >= 0 {
		if end := strings.Index(val[start:], ">"); end > 0 {
			val = strings.TrimSpace(val[start+1 : start+end])
		}
	}
	for _, d := range domains {
		if val == d || strings.HasSuffix(val, "."+d) {
			return true
		}
	}
	return false
}

// isSelfAddressedBulk detects the "From == To, recipients in BCC" bulk-send
// pattern used by senders that bypass ESPs (no List-Unsubscribe / Precedence).
// Why: External advertisers using their own SMTP often address the message to
// themselves and BCC the distribution list; matching From/To addresses with the
// user appearing only via BCC/Delivered-To is a strong bulk signal. Excludes
// the user's own self-memos (isFromMe path handled elsewhere).
// Strict rule: cut only when To parses to exactly one address equal to From.
// Multi-recipient To headers are left to other filters.
func isSelfAddressedBulk(fromHeader, toHeader, userEmail string) bool {
	fromAddr := parseAddrLower(fromHeader)
	if fromAddr == "" {
		return false
	}
	if fromAddr == strings.ToLower(strings.TrimSpace(userEmail)) {
		return false
	}
	toList, err := mail.ParseAddressList(toHeader)
	if err != nil || len(toList) != 1 {
		return false
	}
	toAddr := strings.ToLower(strings.TrimSpace(toList[0].Address))
	return fromAddr == toAddr
}

// isSystemOriginEmail detects emails sent by this system (e.g. weekly reports)
// so the scanner can skip re-ingestion without burning AI quota or creating ghost tasks.
func isSystemOriginEmail(headers []*gmail.MessagePartHeader, subject string) bool {
	if strings.HasPrefix(subject, "[WR]") {
		return true
	}
	for _, h := range headers {
		if h.Name == "X-WhatAp-Origin" {
			return true
		}
	}
	return false
}

func isSkipSender(from string, skips []string) bool {
	fromLower := strings.ToLower(from)
	if strings.Contains(fromLower, "no-reply") || strings.Contains(fromLower, "noreply") || strings.Contains(fromLower, "do-not-reply") || strings.Contains(fromLower, "mailer-daemon") {
		return true
	}
	for _, s := range skips {
		if strings.Contains(fromLower, s) {
			logger.Debugf("[GMAIL] skipping noise email from: %s (matches skip rule: %s)", from, s)
			return true
		}
	}
	return false
}
