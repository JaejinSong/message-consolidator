package services

import (
	"regexp"
	"strings"
)

var (
	subjectBracketRe = regexp.MustCompile(`(?i)^\s*(\[[^\]]*\]\s*)+`)
	subjectReplyRe   = regexp.MustCompile(`(?i)^(re|fwd|fw):\s*`)
)

// ackOnlyTokens is the set of short acknowledgment tokens that do NOT constitute
// substantive task resolution. When a fromMe reply is composed solely of these
// tokens (after stripping whitespace, greetings, and signatures), it should be
// reclassified without AI evaluation — AI RESOLVE on "ok/thanks" would incorrectly
// close the sender's own task.
var ackOnlyTokens = map[string]bool{
	"네": true, "넵": true, "알겠습니다": true, "확인했습니다": true,
	"감사합니다": true, "감사해요": true, "고맙습니다": true, "오케이": true,
	"ok": true, "okay": true, "thanks": true, "thx": true, "thank you": true,
	"noted": true, "got it": true, "sure": true, "fine": true,
	"understood": true, "will do": true, "👍": true, "✅": true, "✔️": true,
}

// signaturePrefixes marks the start of email signature blocks to strip before ack detection.
var signaturePrefixes = []string{"From:", "Sent from", "보내는 사람:"}

// greetingPrefixes strips common openers that are not substantive content.
var greetingPrefixes = []string{"안녕하세요", "Hi", "Hello", "Hey"}

// maxAckLength is the character budget after stripping. Anything longer is
// treated as a substantive reply that deserves AI evaluation.
const maxAckLength = 50

// isAckOnlyReply returns true when the reply text, after stripping whitespace,
// common greetings, and email signature blocks, consists solely of short
// acknowledgment tokens. Maximum stripped length is maxAckLength characters.
func isAckOnlyReply(text string) bool {
	stripped := stripSignature(text)
	stripped = stripGreeting(stripped)
	stripped = strings.TrimSpace(stripped)
	if len([]rune(stripped)) > maxAckLength {
		return false
	}
	return matchesAckTokens(stripped)
}

func stripSignature(text string) string {
	for _, prefix := range signaturePrefixes {
		if idx := strings.Index(text, prefix); idx != -1 {
			text = text[:idx]
		}
	}
	return text
}

func stripGreeting(text string) string {
	trimmed := strings.TrimSpace(text)
	for _, g := range greetingPrefixes {
		if strings.HasPrefix(trimmed, g) {
			rest := strings.TrimPrefix(trimmed, g)
			rest = strings.TrimLeft(rest, " \t\n\r,.")
			trimmed = rest
		}
	}
	return trimmed
}

// matchesAckTokens checks if the text (after stripping) is composed solely of
// known ack tokens plus punctuation noise (!, ., ,, ~, ^^, 🙏).
func matchesAckTokens(text string) bool {
	// Remove punctuation noise to isolate token words.
	noise := strings.NewReplacer("!", "", ".", "", ",", "", "~", "", "^", "", "🙏", "", " ", "")
	cleaned := strings.ToLower(noise.Replace(text))
	if cleaned == "" {
		return false
	}
	// Check against direct map lookup for the whole cleaned string.
	if ackOnlyTokens[cleaned] {
		return true
	}
	// Multi-token: split on whitespace and check each word.
	words := strings.Fields(strings.ToLower(text))
	for _, w := range words {
		wClean := noise.Replace(w)
		if wClean == "" {
			continue
		}
		if !ackOnlyTokens[wClean] {
			return false
		}
	}
	return len(words) > 0
}

// completionSignalTokens are substrings that suggest a message reports the completion
// or resolution of some task. Used as a cheap gate before the FTS + LLM cross-channel
// match, so unrelated chatter never pays for a candidate search.
// Why: most of this user's counterparty chats are Indonesian/Malay, not Korean/English —
// add those completion phrases. Bare "sudah"/"ok" are deliberately excluded (too broad).
var completionSignalTokens = []string{
	"완료", "처리했", "처리 했", "끝냈", "끝났", "마쳤", "마무리", "해결", "반영", "배포",
	"제출", "전달드", "전달했", "보냈", "보내드", "송부", "회신", "업로드", "공유드", "완납",
	"done", "finished", "completed", "resolved", "fixed", "deployed", "submitted",
	"sent", "shipped", "uploaded", "closed", "handled", "delivered", "wrapped up",
	"selesai", "sudah bisa", "sudah ok", "sudah aman", "sudah normal", "sudah jalan",
	"sudah up", "berhasil", "beres", "udah bisa", "dah siap", "dah ok",
	"now okay", "works now", "working now", "already up", "is up now", "no problem now",
}

// hasCompletionSignal reports whether text plausibly announces a completion. Case-insensitive
// substring match — deliberately high-recall; the LLM transition check is the precision gate.
func hasCompletionSignal(text string) bool {
	lower := strings.ToLower(text)
	for _, tok := range completionSignalTokens {
		if strings.Contains(lower, tok) {
			return true
		}
	}
	return false
}

func extractSubjectFromText(originalText string) string {
	for _, line := range strings.Split(originalText, "\n") {
		if strings.HasPrefix(line, "S: ") {
			s := strings.TrimPrefix(line, "S: ")
			s = subjectBracketRe.ReplaceAllString(s, "")
			s = subjectReplyRe.ReplaceAllString(s, "")
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// HasCompletionSignal exposes hasCompletionSignal to scanner adapters so they can
// gate cross-channel dispatch without duplicating the keyword list.
func HasCompletionSignal(text string) bool { return hasCompletionSignal(text) }
