package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/ai/core"
	"message-consolidator/logger"
	"message-consolidator/store"
	"strings"
	"time"

	"github.com/whatap/go-api/trace"
)

// IdentityResolver uses the configured AI provider to propose groups of contacts that are likely the same person.
type IdentityResolver struct {
	client *AIClient
}

func NewIdentityResolver(client *AIClient) *IdentityResolver {
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
func (r *IdentityResolver) ProposeGroups(ctx context.Context, email string, contacts []store.ContactRecord) ([]MergeGroup, error) {
	if len(contacts) < 2 {
		return nil, nil
	}

	if len(contacts) > identityChunkSize {
		return r.proposeInChunks(ctx, email, contacts)
	}

	return r.proposeChunk(ctx, email, contacts)
}

func (r *IdentityResolver) proposeInChunks(ctx context.Context, email string, contacts []store.ContactRecord) ([]MergeGroup, error) {
	var all []MergeGroup
	for i := 0; i < len(contacts); i += identityChunkSize {
		end := i + identityChunkSize
		if end > len(contacts) {
			end = len(contacts)
		}
		groups, err := r.proposeChunk(ctx, email, contacts[i:end])
		if err != nil {
			return nil, err
		}
		all = append(all, groups...)
	}
	return all, nil
}

func (r *IdentityResolver) proposeChunk(ctx context.Context, email string, contacts []store.ContactRecord) ([]MergeGroup, error) {
	parsed := core.LoadPrompt(core.PromptIdentityGroupMerge)
	rendered, err := parsed.Render(core.ExtractionContext{
		MessagePayload: formatContactsForPrompt(contacts),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to render identity prompt: %w", err)
	}

	modelName := r.client.resolveModel(parsed, r.client.identity)
	req := LLMRequest{
		Model:       modelName,
		User:        rendered,
		Temperature: 0.1,
		Thinking:    r.client.resolveThinking(parsed, r.client.identity),
	}
	start := time.Now()
	resp, err := r.client.transport.Generate(ctx, req, 300*time.Second, 2)
	elapsedMs := time.Since(start).Milliseconds()
	logger.Infof("[RESOLUTION] identity resolve: %dms model=%s contacts=%d err=%v", elapsedMs, modelName, len(contacts), err)
	if err != nil {
		return nil, err
	}
	logTokenUsage(ctx, email, "IdentityResolve", modelName, "", 0, resp.Usage)
	_ = trace.Step(ctx, r.client.tracePrefix+"-IdentityResolve", "", int(elapsedMs), len(contacts))

	clean := strings.TrimSpace(resp.Text)
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
		fmt.Fprintf(&sb, "- id: %d, name: %q, canonical_id: %q\n", c.ID, c.DisplayName, c.CanonicalID)
	}
	return sb.String()
}
