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
	// Why: [FlexItem] Defines a flat struct to capture all Gemini fields, specifically using interface{} for ID to gracefully handle both string ("1") and number (1) formats common in AI responses.
	type flexItem struct {
		ID              interface{}     `json:"id"`
		State           string          `json:"state"`
		Task            string          `json:"task"`
		Requester       string          `json:"requester"`
		Assignee        string          `json:"assignee"`
		AssignedTo      string          `json:"assigned_to,omitempty"`
		AssignedAt      string          `json:"assigned_at"`
		SourceTS        string          `json:"source_ts"`
		Category        string          `json:"category"`
		Deadline        string          `json:"deadline"`
		AssigneeReason  string          `json:"assignee_reason,omitempty"`
		IsContextQuery  bool            `json:"is_context_query"`
		Constraints     []string        `json:"constraints"`
		Metadata        json.RawMessage `json:"metadata"`
		AffinityScore   int             `json:"affinity_score,omitempty"`
		AffinityGroupID string          `json:"affinity_group_id,omitempty"`
	}

	var flexItems []flexItem
	if err := json.Unmarshal([]byte(cleanJSON), &flexItems); err == nil {
		if len(flexItems) == 0 {
			return nil, nil
		}
		for _, f := range flexItems {
			// Why: Manually maps the flexible fields to the formal store.TodoItem structure, ensuring ID type consistency.
			item := store.TodoItem{
				State: f.State, Task: f.Task, Requester: f.Requester, Assignee: f.Assignee,
				AssignedTo: f.AssignedTo, AssignedAt: f.AssignedAt, SourceTS: f.SourceTS,
				Category: f.Category, Deadline: f.Deadline, AssigneeReason: f.AssigneeReason,
				IsContextQuery: f.IsContextQuery, Constraints: f.Constraints, Metadata: f.Metadata,
				AffinityScore: f.AffinityScore, AffinityGroupID: f.AffinityGroupID,
			}
			if f.ID != nil {
				switch v := f.ID.(type) {
				case float64:
					id := int(v)
					item.ID = &id
				case string:
					var id int
					if _, err := fmt.Sscanf(v, "%d", &id); err == nil {
						item.ID = &id
					}
				}
			}
			if item.Task != "" {
				items = append(items, item)
			}
		}
		if len(items) > 0 {
			return items, nil
		}
	}

	// Why: [Fallback] Only attempts map unmarshaling if the input is definitely a JSON object, avoiding "array into map" errors.
	if strings.HasPrefix(strings.TrimSpace(cleanJSON), "{") {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(cleanJSON), &obj); err == nil {
			for _, val := range obj {
				if err := json.Unmarshal(val, &items); err == nil && len(items) > 0 {
					return items, nil
				}
			}
		}
	}

	// Why: [Fallback] Handles legacy AI formats where tasks are encapsulated in an 'analysis' nesting structure.
	var analysisItems []struct {
		ID             string `json:"id"`
		Platform       string `json:"platform"`
		User           string `json:"user"`
		RequestContent string `json:"request_content"`
		Analysis       struct {
			Category   string `json:"category"`
			Priority   string `json:"priority"`
			ActionItem string `json:"action_item"`
			Response   string `json:"response"`
		} `json:"analysis"`
	}
	if err := json.Unmarshal([]byte(cleanJSON), &analysisItems); err == nil && len(analysisItems) > 0 && analysisItems[0].Analysis.ActionItem != "" {
		for _, ai := range analysisItems {
			items = append(items, store.TodoItem{
				SourceTS:  ai.ID,
				Task:      ai.Analysis.ActionItem,
				Category:  strings.ToLower(ai.Analysis.Category),
				Requester: ai.User,
				Assignee:  "me", // Analysis-style often implies the session user.
				State:     "new",
			})
		}
		return items, nil
	}
	return nil, fmt.Errorf("no items found in JSON or invalid format")
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
