package store

import (
	"context"

	"message-consolidator/db"
	"message-consolidator/logger"
)

// decisionTextHeadRunes caps the stored excerpt. Why: enough to recognise the message
// when sampling, short enough that this stays telemetry rather than a second copy of the
// inbox.
const decisionTextHeadRunes = 240

// Decision stages and verdicts. Kept as constants so a query can group on them reliably.
const (
	DecisionStageFilter  = "filter"
	DecisionStageExtract = "extract"

	DecisionVerdictNoise = "noise"
	DecisionVerdictNone  = "none"
)

// ExtractionDecisionInput is what a drop site knows about the message it discarded.
type ExtractionDecisionInput struct {
	UserEmail string
	Stage     string
	Verdict   string
	Source    string
	Room      string
	SourceTS  string
	Category  string
	Text      string
}

// RecordExtractionDecision persists one dropped message for later sampling.
//
// Best-effort by design: a telemetry failure must never stop extraction, so the error is
// logged and swallowed rather than returned. Why: the whole point is to observe the
// suppression paths, and a recorder that can break the pipeline would be worse than the
// blind spot it closes.
func RecordExtractionDecision(ctx context.Context, in ExtractionDecisionInput) {
	if in.UserEmail == "" || in.Stage == "" || in.Verdict == "" {
		return
	}
	conn := GetDB()
	if conn == nil {
		return
	}
	err := db.New(conn).RecordExtractionDecision(ctx, db.RecordExtractionDecisionParams{
		UserEmail: in.UserEmail,
		Stage:     in.Stage,
		Verdict:   in.Verdict,
		Source:    in.Source,
		Room:      in.Room,
		SourceTs:  in.SourceTS,
		Category:  in.Category,
		TextHead:  truncateRunes(in.Text, decisionTextHeadRunes),
	})
	if err != nil {
		logger.Warnf("[DECISION] record %s/%s failed: %v", in.Stage, in.Verdict, err)
	}
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "..."
}
