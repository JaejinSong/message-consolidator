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

const identityChunkSize = 20

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
	parsed := LoadPrompt(PromptIdentityGroupMerge)
	rendered, err := parsed.Render(ExtractionContext{
		MessagePayload: formatContactsForPrompt(contacts),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to render identity prompt: %w", err)
	}

	model := r.client.initModel(r.client.getEffectiveModel(parsed, r.client.analysisModel), 0.1, 0, "", "")
	resp, err := generateWithRetry(ctx, model, genai.Text(rendered), 300*time.Second, 2)
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

func formatContactsForPrompt(contacts []store.ContactRecord) string {
	var sb strings.Builder
	for _, c := range contacts {
		sb.WriteString(fmt.Sprintf("- id: %d, name: %q, canonical_id: %q\n", c.ID, c.DisplayName, c.CanonicalID))
	}
	return sb.String()
}
