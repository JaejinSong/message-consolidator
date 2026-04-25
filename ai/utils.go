package ai

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
	"strings"
)

type flexSubtask struct {
	Task         string         `json:"task"`
	AssigneeID   *store.UserID  `json:"assignee_id"`
	AssigneeName string         `json:"assignee_name"`
}

type flexItem struct {
	ID              interface{}     `json:"id"`
	State           string          `json:"state"`
	Status          string          `json:"status"`
	Reasoning       string          `json:"reasoning,omitempty"`
	Task            string          `json:"task"`
	Requester       string          `json:"requester"`
	RequesterID     *store.UserID   `json:"requester_id"`
	Assignee        string          `json:"assignee"`
	AssigneeID      *store.UserID   `json:"assignee_id"`
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
	Subtasks        []flexSubtask   `json:"subtasks,omitempty"`
}

func unmarshalAnalyze(cleanJSON, rawJSON, userEmail string, currentUserID store.UserID) ([]store.TodoItem, error) {
	cleanJSON = strings.TrimSpace(cleanJSON)
	if len(cleanJSON) < 2 {
		return nil, fmt.Errorf("empty JSON")
	}

	// 1. Try single object (New Consolidated Format)
	if strings.HasPrefix(cleanJSON, "{") {
		var f flexItem
		if err := json.Unmarshal([]byte(cleanJSON), &f); err == nil && f.Task != "" {
			return []store.TodoItem{mapFlexToTodo(f, currentUserID, userEmail)}, nil
		}
	}

	// 2. Try array (Legacy/Fallback Format), then 3. wrapped object {tasks|items: [...]}.
	flexItems := tryDecodeFlexItems(cleanJSON)

	if len(flexItems) > 0 {
		var items []store.TodoItem
		for _, f := range flexItems {
			if f.Task != "" || strings.ToLower(f.State) == "none" {
				items = append(items, mapFlexToTodo(f, currentUserID, userEmail))
			}
		}
		return items, nil
	}

	// Double check if it was explicitly an empty array []
	if strings.HasPrefix(cleanJSON, "[") && strings.HasSuffix(cleanJSON, "]") {
		return []store.TodoItem{}, nil
	}

	return nil, fmt.Errorf("no valid items found in JSON")
}

func tryDecodeFlexItems(cleanJSON string) []flexItem {
	var items []flexItem
	if err := json.Unmarshal([]byte(cleanJSON), &items); err == nil {
		return items
	}
	var wrapped struct {
		Tasks []flexItem `json:"tasks"`
		Items []flexItem `json:"items"`
	}
	if err := json.Unmarshal([]byte(cleanJSON), &wrapped); err != nil {
		return nil
	}
	if len(wrapped.Tasks) > 0 {
		return wrapped.Tasks
	}
	return wrapped.Items
}

func mapFlexToTodo(f flexItem, currentUserID store.UserID, userEmail string) store.TodoItem {
	assignee := f.Assignee
	if f.AssigneeID != nil && *f.AssigneeID == currentUserID {
		assignee = userEmail
	}
	requesterCanonical := ""
	if f.RequesterID != nil && *f.RequesterID == currentUserID {
		requesterCanonical = userEmail
	}

	item := store.TodoItem{
		State: f.State, Status: f.Status, Reasoning: f.Reasoning, Task: f.Task, Requester: f.Requester,
		RequesterCanonical: requesterCanonical,
		Assignee: assignee, AssignedTo: f.AssignedTo, AssignedAt: f.AssignedAt,
		SourceTS: f.SourceTS, Category: f.Category, Deadline: f.Deadline,
		AssigneeReason: f.AssigneeReason, IsContextQuery: f.IsContextQuery,
		Constraints: f.Constraints, Metadata: f.Metadata, AffinityScore: f.AffinityScore,
		AffinityGroupID: f.AffinityGroupID,
	}

	for _, s := range f.Subtasks {
		name := s.AssigneeName
		if s.AssigneeID != nil && *s.AssigneeID == currentUserID {
			name = userEmail
		}
		item.Subtasks = append(item.Subtasks, store.TodoSubtask{
			Task: s.Task, AssigneeID: s.AssigneeID, AssigneeName: name,
		})
	}

	if f.ID == nil {
		return item
	}

	// ID type normalization
	switch v := f.ID.(type) {
	case float64:
		id := store.MessageID(v)
		item.ID = &id
	case string:
		var raw int64
		if _, err := fmt.Sscanf(v, "%d", &raw); err == nil {
			id := store.MessageID(raw)
			item.ID = &id
		}
	}
	return item
}

func unmarshalTranslate(cleanJSON, rawJSON, language string) ([]store.TranslateRequest, error) {
	var translations []store.TranslateRequest
	if err := json.Unmarshal([]byte(cleanJSON), &translations); err != nil {
		fallback, fbErr := decodeTranslationFallback(cleanJSON)
		if fbErr != nil {
			logger.Errorf("[GEMINI] Translation JSON unmarshal fallback failed: %v", fbErr)
			logger.Errorf("[GEMINI] Translation JSON unmarshal failed: %v, RAW: %s", err, rawJSON)
			return nil, err
		}
		translations = fallback
	}
	logger.Debugf("[GEMINI] Successfully translated %d items to %s", len(translations), language)
	return translations, nil
}

//Why: Wrapped TranslateResponse is an alternate AI output shape; isolating the decode keeps unmarshalTranslate flat.
func decodeTranslationFallback(cleanJSON string) ([]store.TranslateRequest, error) {
	var tr store.TranslateResponse
	if err := json.Unmarshal([]byte(cleanJSON), &tr); err != nil {
		return nil, err
	}
	if len(tr.Translations) == 0 {
		return nil, fmt.Errorf("translation response wrapper empty")
	}
	return tr.Translations, nil
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

// CleanMarkdownText removes markdown code block markers and trims surrounding whitespace.
// Why: Provides a defensive layer against LLMs adding unsolicited markdown blocks, ensuring data integrity.
func CleanMarkdownText(input string) string {
	replacer := strings.NewReplacer(
		"```markdown", "",
		"```json", "",
		"```text", "",
		"```", "",
	)
	return strings.TrimSpace(replacer.Replace(input))
}

// sanitizeJSON cleans AI response from markdown code blocks and whitespace.
// Why: Orchestrates the multi-stage JSON extraction process while adhering to strict 30-line function limits.
func sanitizeJSON(s string) string {
	s = CleanMarkdownText(s)
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
