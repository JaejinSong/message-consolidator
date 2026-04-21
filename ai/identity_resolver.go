package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/store"
	"strings"
	"time"

	"github.com/google/generative-ai-go/genai"
)

// IdentityResolver uses Gemini to propose groups of contacts that are likely the same person.
type IdentityResolver struct {
	client *GeminiClient
}

func NewIdentityResolver(client *GeminiClient) *IdentityResolver {
	return &IdentityResolver{client: client}
}

// MergeGroup is the AI's proposed grouping: contact IDs that are likely the same person.
type MergeGroup struct {
	ContactIDs []int64 `json:"contact_ids"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

const identityChunkSize = 50

// ProposeGroups analyzes the given contacts and returns groups of likely-same-person contacts.
func (r *IdentityResolver) ProposeGroups(ctx context.Context, contacts []store.ContactRecord) ([]MergeGroup, error) {
	if len(contacts) < 2 {
		return nil, nil
	}

	if len(contacts) > identityChunkSize {
		return r.proposeInChunks(ctx, contacts)
	}

	return r.proposeChunk(ctx, contacts)
}

func (r *IdentityResolver) proposeInChunks(ctx context.Context, contacts []store.ContactRecord) ([]MergeGroup, error) {
	var all []MergeGroup
	for i := 0; i < len(contacts); i += identityChunkSize {
		end := i + identityChunkSize
		if end > len(contacts) {
			end = len(contacts)
		}
		groups, err := r.proposeChunk(ctx, contacts[i:end])
		if err != nil {
			return nil, err
		}
		all = append(all, groups...)
	}
	return all, nil
}

func (r *IdentityResolver) proposeChunk(ctx context.Context, contacts []store.ContactRecord) ([]MergeGroup, error) {
	prompt := buildGroupMergePrompt(contacts)
	model := r.client.client.GenerativeModel(r.client.analysisModel)
	model.SetTemperature(0.1)

	resp, err := generateWithRetry(ctx, model, genai.Text(prompt), 30*time.Second, 3)
	if err != nil {
		return nil, err
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return nil, nil
	}

	text, ok := resp.Candidates[0].Content.Parts[0].(genai.Text)
	if !ok {
		return nil, fmt.Errorf("unexpected AI response type")
	}

	clean := strings.TrimSpace(string(text))
	clean = strings.TrimPrefix(clean, "```json")
	clean = strings.TrimPrefix(clean, "```")
	clean = strings.TrimSuffix(clean, "```")
	clean = strings.TrimSpace(clean)

	var groups []MergeGroup
	if err := json.Unmarshal([]byte(clean), &groups); err != nil {
		return nil, fmt.Errorf("failed to parse AI group proposal: %w", err)
	}

	return groups, nil
}

func buildGroupMergePrompt(contacts []store.ContactRecord) string {
	var sb strings.Builder
	sb.WriteString("Analyze the following contacts and identify groups that are likely the same physical person.\n")
	sb.WriteString("Return ONLY a JSON array. Each element: {\"contact_ids\": [id1, id2, ...], \"confidence\": 0.0-1.0, \"reason\": \"...\"}\n\n")
	sb.WriteString("Contacts:\n")
	for _, c := range contacts {
		sb.WriteString(fmt.Sprintf("- id: %d, name: %q, canonical_id: %q\n", c.ID, c.DisplayName, c.CanonicalID))
	}
	sb.WriteString("\nMatching rules (all case-insensitive):\n")
	sb.WriteString("1. Name reordering: 'Jaejin Song' and 'Song Jaejin' are the same.\n")
	sb.WriteString("2. Korean-English transliteration: '송재진' and 'Jaejin Song' are the same person.\n")
	sb.WriteString("3. Parenthesized nickname: 'Jaejin Song (JJ)' matches 'Jaejin Song' and 'JJ'.\n")
	sb.WriteString("4. Email username hint: 'jjsong@whatap.io' suggests the person named 'JJ Song' or 'Jaejin Song'.\n")
	sb.WriteString("5. Partial name + same email domain strongly suggests same person.\n")
	sb.WriteString("Only include groups with confidence > 0.6 and at least 2 contact_ids.\n")
	sb.WriteString("Do NOT merge contacts that are clearly different people.\n")
	sb.WriteString("Return [] if no confident matches found.\n")
	return sb.String()
}
