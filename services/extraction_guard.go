package services

import (
	"context"
	"fmt"
	"message-consolidator/db"
	"message-consolidator/store"
	"message-consolidator/types"
	"sort"
	"strings"
	"unicode"
)

// GuardResult reports what the deterministic layer changed, for logging and learning.
type GuardResult struct {
	Kept       bool
	Demotions  []string // e.g. "category:FOO->TASK", "assignee:X->shared", "deadline_dropped", "source_ts_replaced"
	DropReason string   // set when Kept == false: "no_token_overlap" | "suppress_rule"
}

// ApplyExtractionGuard validates raw AI output against envelope facts before BuildTask.
// Why: primary prompt-injection defense -- external senders must not be able to assign
// tasks to arbitrary people or plant hallucinated deadlines/IDs via message text.
func ApplyExtractionGuard(ctx context.Context, p TaskBuildParams) (TaskBuildParams, GuardResult) {
	result := GuardResult{Kept: true}
	snapshotAIOriginal(&p)

	guardCategory(&p, &result)
	guardAssignee(ctx, &p, &result)
	guardDeadline(&p, &result)
	guardSourceTS(&p, &result)

	if !guardTaskOverlap(p) {
		result.Kept = false
		result.DropReason = "no_token_overlap"
		return p, result
	}
	if guardSuppressRule(ctx, p) {
		result.Kept = false
		result.DropReason = "suppress_rule"
	}
	return p, result
}

// snapshotAIOriginal records the untouched AI fields into item metadata before any
// demotion runs -- the diff baseline correction learning mines later. A MetadataSet
// failure must not drop the item, so we skip the snapshot and continue.
func snapshotAIOriginal(p *TaskBuildParams) {
	item := p.Item
	if item.Task == "" && item.Assignee == "" && item.Deadline == "" && item.Category == "" {
		return
	}
	snapshot := map[string]string{
		"task":     item.Task,
		"assignee": item.Assignee,
		"deadline": item.Deadline,
		"category": item.Category,
	}
	updated, err := MetadataSet(p.Item.Metadata, "ai_original", snapshot)
	if err != nil {
		return
	}
	p.Item.Metadata = updated
}

// guardCategory enforces the closed AI extraction category set (G1).
func guardCategory(p *TaskBuildParams, result *GuardResult) {
	if p.Item.Category == "" || types.IsValidTaskCategory(p.Item.Category) {
		return
	}
	result.Demotions = append(result.Demotions, fmt.Sprintf("category:%s->%s", p.Item.Category, types.CategoryTask))
	p.Item.Category = string(types.CategoryTask)
}

// guardAssignee enforces the confirmed grounding boundary (G2): envelope persons pass
// untouched; anyone else must be both quoted in the original text and known to contacts,
// otherwise the assignment demotes to shared.
func guardAssignee(ctx context.Context, p *TaskBuildParams, result *GuardResult) {
	assignee := strings.TrimSpace(p.Item.Assignee)
	if assignee == "" || strings.EqualFold(assignee, AssigneeShared) {
		return
	}
	if isEnvelopePerson(assignee, *p) {
		return
	}
	if !strings.Contains(strings.ToLower(p.OriginalText), strings.ToLower(assignee)) {
		p.Item.Assignee = AssigneeShared
		result.Demotions = append(result.Demotions, fmt.Sprintf("assignee:%s->shared", assignee))
		return
	}
	if contactsRecognize(ctx, p.UserEmail, assignee) {
		return
	}
	// Why: quoted in text but unknown to contacts -- distinct tag so correction learning
	// can mine this case for new-contact discovery.
	p.Item.Assignee = AssigneeShared
	result.Demotions = append(result.Demotions, "assignee_ungrounded")
}

// isEnvelopePerson reports whether name resolves to the envelope (mentions, sender,
// current user) rather than an AI-only extraction.
func isEnvelopePerson(name string, p TaskBuildParams) bool {
	for _, m := range p.ExplicitMentions {
		if strings.EqualFold(strings.TrimSpace(m), name) {
			return true
		}
	}
	if p.SenderRaw != "" && strings.EqualFold(p.SenderRaw, name) {
		return true
	}
	if p.SenderEmail != "" && strings.EqualFold(p.SenderEmail, name) {
		return true
	}
	return isSelfReference(name, p) || matchesAlias(name, p.Aliases)
}

// contactsRecognize guards the DB call the same way resolveRequester does -- no
// connection means the check cannot pass.
func contactsRecognize(ctx context.Context, email, name string) bool {
	// Why: NormalizeContactName echoes unknown names back, so only a true
	// resolution lookup can serve as the G2 grounding check.
	return store.ContactNameKnown(ctx, email, name)
}

// guardDeadline enforces text grounding for deadlines (G3): at least one meaningful
// token of the deadline expression must appear in the original text, or the value is
// dropped rather than the whole task.
func guardDeadline(p *TaskBuildParams, result *GuardResult) {
	deadline := strings.TrimSpace(p.Item.Deadline)
	if deadline == "" || deadlineGroundedInText(deadline, p.OriginalText) {
		return
	}
	p.Item.Deadline = ""
	p.Item.DeadlineDate = ""
	p.Item.DeadlineInferred = false
	result.Demotions = append(result.Demotions, "deadline_dropped")
}

func deadlineGroundedInText(deadline, text string) bool {
	lowerText := strings.ToLower(text)
	for _, token := range strings.Fields(deadline) {
		token = strings.ToLower(strings.Trim(token, ".,!?:;()[]\"'"))
		if len(token) < 2 {
			continue
		}
		if strings.Contains(lowerText, token) {
			return true
		}
	}
	return false
}

// guardSourceTS enforces existence of the AI-claimed source ID marker (G4). Payloads
// without [ID:...] markers (e.g. timestamp-only formats) are skipped.
func guardSourceTS(p *TaskBuildParams, result *GuardResult) {
	if p.Item.SourceTS == "" || p.OriginalText == "" || !strings.Contains(p.OriginalText, "[ID:") {
		return
	}
	marker := "[ID:" + p.Item.SourceTS
	if strings.Contains(p.OriginalText, marker) || strings.Contains(p.OriginalText, p.Item.SourceTS) {
		return
	}
	p.Item.SourceTS = p.SourceTS
	result.Demotions = append(result.Demotions, "source_ts_replaced")
}

// guardTaskOverlap enforces the task/text token overlap floor (G5). Reports true when
// the item should be kept.
func guardTaskOverlap(p TaskBuildParams) bool {
	if p.OriginalText == "" {
		return true
	}
	// Why: whitespace token overlap is unreliable for agglutinative Korean; a false
	// drop here is a silent failure, the worst kind -- skip G5 entirely for Hangul text.
	if containsHangul(p.OriginalText) {
		return true
	}
	return titleTokenOverlap(p.Item.Task, p.OriginalText) >= 1
}

func containsHangul(text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Hangul, r) {
			return true
		}
	}
	return false
}

// guardSuppressRule reports whether a learned suppress rule (G6) matches the message,
// i.e. every token of the rule signature is present in the message's token signature.
func guardSuppressRule(ctx context.Context, p TaskBuildParams) bool {
	if store.GetDB() == nil {
		return false
	}
	rules, err := db.New(store.GetDB()).ListActiveSuppressRules(ctx, p.UserEmail)
	if err != nil || len(rules) == 0 {
		return false
	}
	messageSig := suppressSignature(p.OriginalText)
	messageSet := make(map[string]struct{}, len(messageSig))
	for _, t := range messageSig {
		messageSet[t] = struct{}{}
	}
	for _, rule := range rules {
		if ruleSubsetOf(strings.Fields(rule.FromValue), messageSet) {
			return true
		}
	}
	return false
}

func ruleSubsetOf(ruleTokens []string, messageSet map[string]struct{}) bool {
	if len(ruleTokens) == 0 {
		return false
	}
	for _, t := range ruleTokens {
		if _, ok := messageSet[t]; !ok {
			return false
		}
	}
	return true
}

// suppressSignature computes a lowercase, deduped, sorted token signature (len >= 3)
// for matching a message against learned suppress rules.
func suppressSignature(text string) []string {
	seen := map[string]struct{}{}
	var tokens []string
	for _, raw := range strings.Fields(text) {
		t := strings.ToLower(strings.Trim(raw, ".,!?:;()[]\"'"))
		if len(t) < 3 {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		tokens = append(tokens, t)
	}
	sort.Strings(tokens)
	return tokens
}
