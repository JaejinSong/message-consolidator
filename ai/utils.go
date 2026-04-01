package ai

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
	"strings"
)

func unmarshalAnalyze(cleanJSON, rawJSON string) ([]store.TodoItem, error) {
	var items []store.TodoItem
	// Why: Attempts first-pass unmarshaling as a flat array (standard format).
	if err := json.Unmarshal([]byte(cleanJSON), &items); err == nil {
		return items, nil
	}

	// Why: [Fallback] Handles AI wrapping tasks in objects like {"tasks": [...]}.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(cleanJSON), &obj); err != nil {
		logger.Errorf("[GEMINI] JSON unmarshal failed: %v, RAW: %s", err, rawJSON)
		return nil, err
	}

	for _, val := range obj {
		if err := json.Unmarshal(val, &items); err == nil && len(items) > 0 {
			return items, nil
		}
	}
	return nil, fmt.Errorf("no items found in JSON")
}

func unmarshalTranslate(cleanJSON, rawJSON, language string) ([]store.TranslateRequest, error) {
	var translations []store.TranslateRequest
	if err := json.Unmarshal([]byte(cleanJSON), &translations); err != nil {
		//Why: [Fallback] Checks if the translation response is wrapped in a dedicated object structure before failing, ensuring robustness against varying AI output formats.
		var tr store.TranslateResponse
		if err2 := json.Unmarshal([]byte(cleanJSON), &tr); err2 == nil && len(tr.Translations) > 0 {
			translations = tr.Translations
		} else {
			if err2 != nil {
				logger.Errorf("[GEMINI] Translation JSON unmarshal fallback failed: %v", err2)
			}
			logger.Errorf("[GEMINI] Translation JSON unmarshal failed: %v, RAW: %s", err, rawJSON)
			return nil, err
		}
	}
	logger.Debugf("[GEMINI] Successfully translated %d items to %s", len(translations), language)
	return translations, nil
}

func DecodeBase64URL(data string) (string, error) {
	//Why: [DecodeBase64URL] Robustly handles various Base64 encoding flavors (URL-safe, Standard, with/without padding) common in diverse message source headers.
	// 1. URL-safe encoding (with padding)
	decoded, err := base64.URLEncoding.DecodeString(data)
	if err == nil {
		return string(decoded), nil
	}

	// 2. URL-safe encoding (without padding) - Commonly used in web/tokens
	decoded, err = base64.RawURLEncoding.DecodeString(data)
	if err == nil {
		return string(decoded), nil
	}

	// 3. Standard encoding (with padding)
	decoded, err = base64.StdEncoding.DecodeString(data)
	if err == nil {
		return string(decoded), nil
	}

	// 4. Standard encoding (without padding)
	decoded, err = base64.RawStdEncoding.DecodeString(data)
	if err != nil {
		return "", err
	}

	return string(decoded), nil
}

// sanitizeJSON cleans AI response from markdown code blocks and whitespace.
// Why: Orchestrates the multi-stage JSON extraction process while adhering to strict 30-line function limits.
func sanitizeJSON(s string) string {
	s = strings.TrimSpace(s)
	s = extractMarkdownBlock(s)
	return extractBracketPayload(s)
}

// extractMarkdownBlock isolates content within markdown code blocks (e.g., ```json).
// Why: Specifically targets and extracts the core payload from AI responses formatted in markdown to improve parsing accuracy.
func extractMarkdownBlock(s string) string {
	startIdx := strings.Index(s, "```json")
	if startIdx == -1 {
		startIdx = strings.Index(s, "```")
	}
	if startIdx == -1 {
		return s
	}

	newlineIdx := strings.IndexByte(s[startIdx:], '\n')
	if newlineIdx == -1 {
		return s
	}

	contentStart := startIdx + newlineIdx + 1
	endIdx := strings.Index(s[contentStart:], "```")
	if endIdx == -1 {
		return s
	}

	return strings.TrimSpace(s[contentStart : contentStart+endIdx])
}

// extractBracketPayload identifies and validates the outermost bracket JSON structure.
// Why: Performs the final extraction and self-healing (e.g., closing truncated arrays) to ensure valid JSON output.
func extractBracketPayload(s string) string {
	start := strings.IndexAny(s, "[{")
	end := strings.LastIndexAny(s, "]}")
	if start == -1 || end == -1 || start >= end {
		return ""
	}

	payload := s[start : end+1]
	if payload[0] == '[' {
		payload = handleMultipleArrays(payload)
	}

	// Why: [Repair] Specifically handles truncated AI outputs where an array is opened but not closed, though the last object is complete.
	if len(payload) > 0 && payload[0] == '[' && payload[len(payload)-1] == '}' {
		payload += "]"
	}

	return payload
}

// handleMultipleArrays prevents "invalid character after top-level value" by taking only the first block.
func handleMultipleArrays(p string) string {
	if firstEnd := strings.Index(p, "]["); firstEnd != -1 {
		return p[:firstEnd+1]
	}
	if firstEnd := strings.Index(p, "]\n["); firstEnd != -1 {
		return p[:firstEnd+1]
	}
	return p
}
