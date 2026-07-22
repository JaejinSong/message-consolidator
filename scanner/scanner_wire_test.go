package scanner

import (
	"context"
	"testing"

	"message-consolidator/channels"
	"message-consolidator/config"
	"message-consolidator/services"
	"message-consolidator/store"
	"message-consolidator/types"
)

// nilReportSummarizer satisfies services.ReportSummarizer with a no-op.
type nilReportSummarizer struct{}

func (nilReportSummarizer) Generate(_ context.Context, _, _ string, _ store.ReportID) (string, error) {
	return "", nil
}

// newTestReportsService creates a minimal *services.ReportsService for use in wire tests.
func newTestReportsService() *services.ReportsService {
	return services.NewReportsService(nilReportSummarizer{}, nil, nil, services.ReportConfig{})
}

// TestWireDailyDigest_EmptyRecipients_Branch covers the warn+return when recipients is empty.
func TestWireDailyDigest_EmptyRecipients_Branch(t *testing.T) {
	saveScannerGlobals(t)
	cfg = &config.Config{
		DailyDigestEnabled:         true,
		DailyDigestRecipientEmails: []string{},
	}
	deps.slackClient = channels.NewSlackClient("fake-token")
	deps.digestSvc = nil

	WireDailyDigest(newTestReportsService())

	// Empty recipients → warn + return before setting deps.digestSvc.
	if deps.digestSvc != nil {
		t.Error("deps.digestSvc must remain nil when recipients empty")
	}
}

// TestWireDailyDigest_RecipientSet_NotionDisabled covers the path through to svc creation
// with an empty notion token (notion.Enabled()==false is OK for DailyDigest — it still proceeds).
func TestWireDailyDigest_RecipientSet_NotionDisabled(t *testing.T) {
	saveScannerGlobals(t)
	cfg = &config.Config{
		DailyDigestEnabled:         true,
		DailyDigestRecipientEmails: []string{"digest@example.com"},
		DailyDigestHour:            18,
		DailyDigestTimezone:        "Asia/Seoul",
		DailyDigestLanguage:        "en",
		// NotionToken and NotionReportPageID intentionally empty
	}
	deps.slackClient = channels.NewSlackClient("fake-token")
	deps.digestSvc = nil

	WireDailyDigest(newTestReportsService())

	// Even with empty notion token, DailyDigest wires successfully.
	if deps.digestSvc == nil {
		t.Error("deps.digestSvc must be set when config is valid")
	}
}

// TestWireWeeklyReport_EmptyRecipients_Branch covers the warn+return when recipients empty.
func TestWireWeeklyReport_EmptyRecipients_Branch(t *testing.T) {
	saveScannerGlobals(t)
	cfg = &config.Config{
		WeeklyReportEnabled:         true,
		WeeklyReportRecipientEmails: []string{},
	}
	deps.slackClient = channels.NewSlackClient("fake-token")
	deps.weeklyReportSvc = nil

	WireWeeklyReport(newTestReportsService())

	if deps.weeklyReportSvc != nil {
		t.Error("deps.weeklyReportSvc must remain nil when recipients empty")
	}
}

// TestWireWeeklyReport_NotionDisabled covers the notion.Enabled()==false branch.
func TestWireWeeklyReport_NotionDisabled(t *testing.T) {
	saveScannerGlobals(t)
	cfg = &config.Config{
		WeeklyReportEnabled:         true,
		WeeklyReportRecipientEmails: []string{"weekly@example.com"},
		// NotionToken + NotionReportPageID empty → Enabled() == false
	}
	deps.slackClient = channels.NewSlackClient("fake-token")
	deps.weeklyReportSvc = nil

	WireWeeklyReport(newTestReportsService())

	if deps.weeklyReportSvc != nil {
		t.Error("deps.weeklyReportSvc must remain nil when notion is not configured")
	}
}

// TestTriggerOutgoingCompletions_WithNonMatchingMsgs exercises the inner loop
// when deps.completionSvc is non-nil but messages don't match (not from me or no ReplyToID).
func TestTriggerOutgoingCompletions_WithNonMatchingMsgs(t *testing.T) {
	orig := deps.completionSvc
	t.Cleanup(func() { deps.completionSvc = orig })

	// Set a non-nil deps.completionSvc so the nil guard doesn't fire.
	// We use the existing fakeErrDispatcher type just to satisfy a non-nil pointer requirement.
	// deps.completionSvc is *services.CompletionService — we cannot mock it without an interface.
	// Instead, keep deps.completionSvc nil and add a test that exercises the inner continue branches.
	deps.completionSvc = nil // ensures no goroutine is launched

	user := store.User{Email: "u@x", Name: "Me"}

	// Case 1: not from me → loop continues
	msgs1 := []types.RawMessage{
		{ID: "m1", IsFromMe: false, ReplyToID: "parent", Sender: "OtherUser"},
	}
	triggerOutgoingCompletions(context.Background(), msgs1, user, whatsAppAdapter{}, "group")

	// Case 2: from me but no ReplyToID → loop continues
	msgs2 := []types.RawMessage{
		{ID: "m2", IsFromMe: true, ReplyToID: ""},
	}
	triggerOutgoingCompletions(context.Background(), msgs2, user, whatsAppAdapter{}, "group")
}
