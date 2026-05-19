package channels

import (
	"bytes"
	"message-consolidator/ai/core"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"google.golang.org/api/gmail/v1"

	"github.com/recapco/emailreplyparser"
)

var (
	reWhitespace = regexp.MustCompile(`\s+`)
)

func extractBody(payload *gmail.MessagePart) string {
	if payload == nil {
		return ""
	}
	if body := decodePart(payload); body != "" {
		return body
	}
	for _, part := range payload.Parts {
		if part.MimeType == "text/plain" {
			if body := decodePart(part); body != "" {
				return body
			}
		}
	}
	for _, part := range payload.Parts {
		if body := decodePart(part); body != "" {
			return body
		}
		if result := extractBody(part); result != "" {
			return result
		}
	}
	return ""
}

// Why: Decodes the body of a single MIME part using its declared MIME type so extractBody can iterate without nested branches.
func decodePart(part *gmail.MessagePart) string {
	if part == nil || part.Body == nil || part.Body.Data == "" {
		return ""
	}
	switch part.MimeType {
	case "text/plain":
		return decodeBase64URL(part.Body.Data)
	case "text/html":
		return stripHTML(decodeBase64URL(part.Body.Data))
	}
	return ""
}

// Why: HTML walker recurses with conditional pruning (script/style/blockquote/Gmail-quote)
// and per-element trailing whitespace; cognitive complexity is intrinsic to DOM traversal,
// not the structure. Splitting the inner closure into helpers fragments the parse contract.
//
//nolint:gocognit // DOM walker complexity is intrinsic to HTML sanitization.
func stripHTML(raw string) string {
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		// Why: Provides a graceful fallback to a simple whitespace-normalized version if the HTML parser fails, ensuring some level of sanitization.
		return strings.TrimSpace(reWhitespace.ReplaceAllString(raw, " "))
	}

	var buf bytes.Buffer
	var f func(*html.Node)
	f = func(n *html.Node) {
		// Why: Explicitly excludes script and style nodes and their entire subtrees to prevent their configuration or logic content from leaking into the extracted text.
		if n.Type == html.ElementNode {
			if n.Data == "script" || n.Data == "style" {
				return
			}
			// Why: Prunes Gmail reply quotes and blockquotes at the DOM level. This is 100% accurate unlike regex which often fails with nested history.
			if n.Data == "blockquote" {
				buf.WriteString(" ")
				return
			}
			for _, attr := range n.Attr {
				if attr.Key == "class" && (strings.Contains(attr.Val, "gmail_quote") || strings.Contains(attr.Val, "gmail_attr")) {
					buf.WriteString(" ")
					return
				}
			}
		}
		// Why: Skips comment nodes to further reduce noise in the extracted content.
		if n.Type == html.CommentNode {
			return
		}

		if n.Type == html.TextNode {
			buf.WriteString(n.Data)
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}

		// Why: Injects a space after block-level elements to prevent words from being merged together.
		if n.Type == html.ElementNode {
			switch n.Data {
			case "p", "div", "br", "tr", "td", "li", "h1", "h2", "h3", "h4", "h5", "h6":
				buf.WriteString(" ")
			}
		}
	}
	f(doc)

	// Why: Normalizes the final output by collapsing multi-line breaks and excessive spaces into a single space, and unescapes common HTML entities to ensure the text is human-readable.
	s := buf.String()
	s = strings.ReplaceAll(s, " ", " ")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	return strings.TrimSpace(reWhitespace.ReplaceAllString(s, " "))
}

func decodeBase64URL(data string) string {
	decoded, err := core.DecodeBase64URL(data)
	if err != nil {
		return ""
	}
	return decoded
}

// Why: cleanEmailBody strips signatures and quotes using emailreplyparser and ensures the body remains within AI token limits.
func cleanEmailBody(body string) string {
	if body == "" {
		return ""
	}

	// Use verified library to strip quoted text (latest reply only)
	email, err := emailreplyparser.Read(body)
	if err != nil {
		return truncateText(strings.TrimSpace(body), 3000)
	}

	var visibleFragments []string
	for _, f := range email.Fragments {
		// Library considers signatures "visible" but we want them hidden/removed
		if !f.Hidden && !f.Signature {
			visibleFragments = append(visibleFragments, f.String())
		}
	}

	result := strings.Join(visibleFragments, "\n")

	// Fallback: if library returns empty but original was not empty, use truncated original
	if strings.TrimSpace(result) == "" && strings.TrimSpace(body) != "" {
		result = body
	}

	return truncateText(strings.TrimSpace(result), 3000)
}

// truncateText caps the string to maxLen characters/runes safely.
func truncateText(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	if len(runes) > maxLen {
		return string(runes[:maxLen]) + "\n...[TRUNCATED]"
	}
	return s
}
