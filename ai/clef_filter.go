package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"message-consolidator/internal/whataphttpx"
	"message-consolidator/store"
)

const (
	clefMaxStateRunes  = 8000
	clefRequestTimeout = 30 * time.Second
	clefMaxBodyBytes   = 1 << 20
	clefMaxErrorRunes  = 200
	clefQuestionKey    = "verdict"
	clefActionable     = "actionable"
)

const clefInstructions = "This is a message received at work (Korean/English/Indonesian/Thai). Decide whether it should be processed for task extraction or ignored as noise."

var clefCriteria = map[string]string{
	"actionable":    "Work-related request, professional update, promise, or any actionable item for the recipient.",
	"chatter":       "Greeting, social small talk, or automated out-of-office reply.",
	"system":        "OTP/verification code, login alert, or non-actionable system ping (contact added, call missed).",
	"transactional": "Automated receipt, billing statement, delivery update, ride/travel summary.",
	"marketing":     "Newsletter, promotion, or social media notification, even with a call to action.",
	"ack":           "Casual acknowledgment like Ok/Thanks/Got it that does not indicate task resolution.",
	"policy":        "Standing instruction or informational announcement with no immediate action.",
	"digest":        "Scheduled briefing or roundup of news/metrics sent to a list, asking nothing of this recipient.",
}

type ClefFilter struct {
	apiKey    string
	endpoint  string
	model     string
	threshold float64
	client    *http.Client
}

type clefQuestion struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria"`
}

type clefRequest struct {
	Model     string                  `json:"model"`
	State     string                  `json:"state"`
	Questions map[string]clefQuestion `json:"questions"`
}

type clefAnswer struct {
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
}

type clefResponse struct {
	Answers map[string]clefAnswer `json:"answers"`
	Usage   struct {
		InputTokens int `json:"input_tokens"`
	} `json:"usage"`
	Error string `json:"error"`
}

func NewClefFilter(apiKey, baseURL, model string, threshold float64) (*ClefFilter, error) {
	if apiKey == "" {
		return nil, errors.New("clef filter: api key is empty")
	}
	return &ClefFilter{
		apiKey:    apiKey,
		endpoint:  strings.TrimRight(baseURL, "/") + "/systemone",
		model:     model,
		threshold: threshold,
		client:    whataphttpx.Client(),
	}, nil
}

// Why: errors are returned (not swallowed) so callers fail open and keep the message.
func (f *ClefFilter) IsNoise(ctx context.Context, email, source, text string) (bool, error) {
	resp, err := f.call(ctx, truncateRunes(text, clefMaxStateRunes))
	if err != nil {
		return false, err
	}
	answer, ok := resp.Answers[clefQuestionKey]
	if !ok {
		return false, fmt.Errorf("clef filter: response has no %q answer", clefQuestionKey)
	}
	actionable, ok := answer.Probabilities[clefActionable]
	if !ok {
		return false, fmt.Errorf("clef filter: response has no %q probability", clefActionable)
	}

	logTokenUsage(ctx, email, "LiteFilter", f.model, source, 0, LLMUsage{PromptTokens: resp.Usage.InputTokens})
	if actionable >= f.threshold {
		return false, nil
	}

	store.IncrementFilteredCount(email)
	store.RecordExtractionDecision(ctx, store.ExtractionDecisionInput{
		UserEmail: email,
		Stage:     store.DecisionStageFilter,
		Verdict:   store.DecisionVerdictNoise,
		Source:    source,
		Category:  topNonActionable(answer.Probabilities),
		Text:      text,
	})
	return true, nil
}

func (f *ClefFilter) call(ctx context.Context, state string) (*clefResponse, error) {
	body, err := json.Marshal(clefRequest{
		Model: f.model,
		State: state,
		Questions: map[string]clefQuestion{clefQuestionKey: {
			Type:         "choice",
			Instructions: clefInstructions,
			Criteria:     clefCriteria,
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("clef filter: marshal request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, clefRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("clef filter: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+f.apiKey)
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("clef filter: request failed: %w", err)
	}
	defer httpResp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(httpResp.Body, clefMaxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("clef filter: read response: %w", err)
	}
	var out clefResponse
	jsonErr := json.Unmarshal(raw, &out)
	if httpResp.StatusCode < 200 || httpResp.StatusCode > 299 {
		msg := out.Error
		if jsonErr != nil || msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		return nil, fmt.Errorf("clef filter: status %d: %s", httpResp.StatusCode, truncateRunes(msg, clefMaxErrorRunes))
	}
	if jsonErr != nil {
		return nil, fmt.Errorf("clef filter: decode response: %w", jsonErr)
	}
	return &out, nil
}

// Why: ties break alphabetically so the recorded category is deterministic.
func topNonActionable(probs map[string]float64) string {
	best, bestP := "", -1.0
	for k, p := range probs {
		if k == clefActionable {
			continue
		}
		if p > bestP || (p == bestP && k < best) {
			best, bestP = k, p
		}
	}
	return best
}
