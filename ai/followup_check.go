package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/ai/core"
	"message-consolidator/logger"
	"strings"
	"time"

	"github.com/whatap/go-api/trace"
)

const (
	FollowupReminder = "REMINDER"
	FollowupNew      = "NEW"
)

// FollowupVerdict is the followup_check outcome for a message from the requester of an already-closed task.
type FollowupVerdict struct {
	Verdict string `json:"verdict"` // REMINDER, NEW
	Reason  string `json:"reason"`
}

// EvaluateFollowup decides whether a message is only a reminder of an already-closed task.
// Why: dedupe compares against active tasks only, so an automated reminder in a new thread
// resurrected a task the user had closed; a title-overlap gate cannot separate it from
// legitimate recurring work, so the call is delegated to the model.
func (g *AIClient) EvaluateFollowup(ctx context.Context, email, closedTask, closedOn, messageText string) (FollowupVerdict, error) {
	if g == nil || g.transport == nil {
		return FollowupVerdict{}, fmt.Errorf("AI client not initialized")
	}

	parsed := core.LoadPrompt(core.PromptFollowupCheck)
	rendered, err := parsed.Render(core.ExtractionContext{
		ParentTask:     closedTask,
		ParentDoneAt:   closedOn,
		MessagePayload: messageText,
		CurrentTime:    time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Locale:         "English",
	})
	if err != nil {
		return FollowupVerdict{}, fmt.Errorf("failed to render followup prompt: %w", err)
	}

	modelName := g.resolveModel(parsed, g.transition)
	req := LLMRequest{
		Model:       modelName,
		System:      rendered,
		Temperature: 0.1,
		// Why: thinking is on and reasoning tokens share the output budget (see EvaluateTaskTransition).
		MaxTokens: DefaultMaxTokens,
		JSONMode:  true,
		Thinking:  g.resolveThinking(parsed, g.transition),
	}
	logInput := fmt.Sprintf("CLOSED: %s\nCLOSED_ON: %s\nMESSAGE: %s", closedTask, closedOn, messageText)

	start := time.Now()
	resp, err := g.transport.Generate(ctx, req, 30*time.Second, 2)
	if err != nil {
		completionCheckLogger(g, "followup_check", logInput, "ERROR: "+err.Error())
		return FollowupVerdict{}, err
	}

	_ = trace.Step(ctx, g.tracePrefix+"-EvaluateFollowup", "", int(time.Since(start).Milliseconds()), 0)
	logTokenUsage(ctx, email, "EvaluateFollowup", modelName, "", 0, resp.Usage)

	return parseFollowupResponse(g, resp, email, logInput)
}

func parseFollowupResponse(g *AIClient, resp LLMResponse, email, logInput string) (FollowupVerdict, error) {
	if resp.FinishReason == "length" {
		logger.Warnf("[AI] EvaluateFollowup hit output limit: think=%d completion=%d budget=%d email=%s",
			resp.Usage.ReasoningTokens, resp.Usage.CompletionTokens, DefaultMaxTokens, email)
		err := fmt.Errorf("followup response truncated at the %d-token output budget", DefaultMaxTokens)
		completionCheckLogger(g, "followup_check", logInput, "ERROR: "+err.Error())
		return FollowupVerdict{}, err
	}

	var v FollowupVerdict
	if err := json.Unmarshal([]byte(core.SanitizeJSON(resp.Text)), &v); err != nil {
		parseErr := fmt.Errorf("failed to parse AI followup response: %w (raw: %s)", err, resp.Text)
		completionCheckLogger(g, "followup_check", logInput, "ERROR: "+parseErr.Error())
		return FollowupVerdict{}, parseErr
	}
	v.Verdict = strings.ToUpper(strings.TrimSpace(v.Verdict))
	if v.Verdict != FollowupReminder && v.Verdict != FollowupNew {
		err := fmt.Errorf("unknown followup verdict %q", v.Verdict)
		completionCheckLogger(g, "followup_check", logInput, "ERROR: "+err.Error())
		return FollowupVerdict{}, err
	}

	completionCheckLogger(g, "followup_check", logInput, resp.Text)
	return v, nil
}
