package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
	"strings"
	"time"

	"github.com/google/generative-ai-go/genai"
)

type IdentityResolver struct {
	client *GeminiClient
}

func NewIdentityResolver(client *GeminiClient) *IdentityResolver {
	return &IdentityResolver{client: client}
}

type MergeCandidate struct {
	ContactA   int64   `json:"contact_id_a"`
	ContactB   int64   `json:"contact_id_b"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

// ProposeMerges scans the contact list and uses Gemini to find fuzzy matches.
// Why: Complements deterministic DSU with probabilistic AI-driven matching for ambiguous identities.
func (r *IdentityResolver) ProposeMerges(ctx context.Context, tenantEmail string) ([]MergeCandidate, error) {
	contacts, err := store.GetLinkedContacts(ctx, tenantEmail)
	if err != nil {
		return nil, err
	}

	if len(contacts) < 2 {
		return nil, nil
	}

	prompt := r.buildMergePrompt(contacts)
	model := r.client.client.GenerativeModel(r.client.analysisModel)
	model.SetTemperature(0.1) // Low temperature for deterministic-ish matching

	resp, err := generateWithRetry(ctx, model, genai.Text(prompt), 30*time.Second, 3)
	if err != nil {
		return nil, err
	}

	var candidates []MergeCandidate
	if len(resp.Candidates) > 0 && len(resp.Candidates[0].Content.Parts) > 0 {
		part := resp.Candidates[0].Content.Parts[0]
		if text, ok := part.(genai.Text); ok {
			cleanJSON := strings.TrimSpace(string(text))
			cleanJSON = strings.TrimPrefix(cleanJSON, "```json")
			cleanJSON = strings.TrimSuffix(cleanJSON, "```")
			if err := json.Unmarshal([]byte(cleanJSON), &candidates); err != nil {
				logger.Errorf("[Identity-X] Failed to parse AI merge proposal: %v", err)
				return nil, err
			}
		}
	}

	return candidates, nil
}

func (r *IdentityResolver) buildMergePrompt(contacts []store.ContactRecord) string {
	var sb strings.Builder
	sb.WriteString("Analyze the following list of contacts and identify pairs that likely represent the same physical person.\n")
	sb.WriteString("Return a JSON array of objects with 'contact_id_a', 'contact_id_b', 'confidence' (0.0-1.0), and 'reason'.\n\n")
	sb.WriteString("Contacts:\n")
	for _, c := range contacts {
		sb.WriteString(fmt.Sprintf("- ID: %d, Name: %s, CanonicalID: %s\n", c.ID, c.DisplayName, c.CanonicalID))
	}
	sb.WriteString("\nRules:\n")
	sb.WriteString("1. High confidence (>0.8) if names are almost identical (e.g. 'Jaejin Song' vs 'Song Jaejin').\n")
	sb.WriteString("2. Medium confidence (0.5-0.7) if they share a common alias or partial email match.\n")
	sb.WriteString("3. Only propose if confidence > 0.5.\n")
	return sb.String()
}
