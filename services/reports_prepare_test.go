package services

import (
	"message-consolidator/store"
	"strings"
	"testing"
	"time"
)

func TestBuildActivityStatsHeader_StalledCount(t *testing.T) {
	activity := []Log{
		{Assignee: "Alice", AssigneeCanonical: "alice", Done: false},
		{Assignee: "Alice", AssigneeCanonical: "alice", Done: false},
		{Assignee: "Bob", AssigneeCanonical: "bob", Done: true},
	}
	stalled := []Log{
		{Task: "old1", Done: false},
		{Task: "old2", Done: false},
	}

	out := buildActivityStatsHeader(activity, stalled)

	if !strings.Contains(out, "3 activity") {
		t.Errorf("expected '3 activity'; got: %s", out)
	}
	if !strings.Contains(out, "2 active") {
		t.Errorf("expected '2 active'; got: %s", out)
	}
	if !strings.Contains(out, "1 done") {
		t.Errorf("expected '1 done'; got: %s", out)
	}
	if !strings.Contains(out, "| 2 stalled") {
		t.Errorf("expected '| 2 stalled'; got: %s", out)
	}
}

func TestBuildActivityStatsHeader_TypeBTrigger(t *testing.T) {
	// Alice holds 9 of 10 open tasks = 90% → trigger
	activity := make([]Log, 10)
	for i := range activity {
		activity[i] = Log{Assignee: "Alice", AssigneeCanonical: "alice", Done: false}
	}
	activity[9] = Log{Assignee: "Bob", AssigneeCanonical: "bob", Done: false}

	out := buildActivityStatsHeader(activity, nil)

	if !strings.Contains(out, "Type B trigger: alice") {
		t.Errorf("expected Type B trigger for alice; got: %s", out)
	}
	if !strings.Contains(out, "alice×9(90%)") {
		t.Errorf("expected alice×9(90%%); got: %s", out)
	}
}

func TestBuildActivityStatsHeader_NoTypeBTrigger(t *testing.T) {
	// 2 assignees evenly split = 50% each — exactly 50, not >40 (both are >40 but let's test ≤40)
	// Actually 50% > 40% triggers. Test with balanced 3-way split: 33% each → no trigger.
	activity := []Log{
		{AssigneeCanonical: "a", Done: false},
		{AssigneeCanonical: "a", Done: false},
		{AssigneeCanonical: "b", Done: false},
		{AssigneeCanonical: "b", Done: false},
		{AssigneeCanonical: "c", Done: false},
		{AssigneeCanonical: "c", Done: false},
	}

	out := buildActivityStatsHeader(activity, nil)

	if strings.Contains(out, "Type B trigger") {
		t.Errorf("should not emit Type B trigger for balanced load; got: %s", out)
	}
}

func TestBuildActivityStatsHeader_CrossSource(t *testing.T) {
	// Same customer "SIMASFIN" inferred from 3 distinct rooms
	activity := []Log{
		{Task: "Setup APM for SIMASFIN", Room: "SIMASFIN-POC", Done: false},
		{Task: "License for SIMASFIN", Room: "SIMASFIN-sales", Done: false},
		{Task: "Monitoring for SIMASFIN", Room: "SIMASFIN-support", Done: false},
	}

	out := buildActivityStatsHeader(activity, nil)

	if !strings.Contains(out, "# Cross-source:") {
		t.Errorf("expected Cross-source line for SIMASFIN across 3 rooms; got: %s", out)
	}
	if !strings.Contains(out, "SIMASFIN") {
		t.Errorf("expected SIMASFIN in cross-source; got: %s", out)
	}
}

func TestBuildActivityStatsHeader_CrossSourceBelowThreshold(t *testing.T) {
	// Same customer but only 2 distinct rooms → no cross-source line
	activity := []Log{
		{Task: "Setup for BankX", Room: "BankX-POC", Done: false},
		{Task: "License for BankX", Room: "BankX-sales", Done: false},
	}

	out := buildActivityStatsHeader(activity, nil)

	if strings.Contains(out, "# Cross-source:") {
		t.Errorf("should not emit Cross-source for only 2 rooms; got: %s", out)
	}
}

func TestBuildActivityStatsHeader_RoomCustomerLine(t *testing.T) {
	activity := []Log{
		// Gmail → "Other Tasks" (generic room, no task signal)
		{Task: "Plan review", Room: "Gmail", Done: false},
		// biz-global-indonesia + task "for Bank BNI" → task-based wins: "Bank BNI"
		{Task: "POC for Bank BNI", Room: "biz-global-indonesia", Done: false},
		// biz-global-malaysia + no task signal → room-based: "Malaysia Biz"
		{Task: "Strategy sync", Room: "biz-global-malaysia", Done: false},
	}

	out := buildActivityStatsHeader(activity, nil)

	if !strings.Contains(out, "# Room→Customer:") {
		t.Errorf("expected Room→Customer line; got: %s", out)
	}
	if !strings.Contains(out, "Gmail→Other Tasks") {
		t.Errorf("expected Gmail→Other Tasks mapping; got: %s", out)
	}
	// task-based inference wins over room name for biz-global-indonesia
	if !strings.Contains(out, "biz-global-indonesia→Bank BNI") {
		t.Errorf("expected biz-global-indonesia→Bank BNI (task-based inference); got: %s", out)
	}
	if !strings.Contains(out, "biz-global-malaysia→Malaysia Biz") {
		t.Errorf("expected biz-global-malaysia→Malaysia Biz (room-based fallback); got: %s", out)
	}
}

// TestBuildActivityStatsHeader_RoomCustomerLineOmitsUnresolved pins the two ways the map
// used to mislead the report model: an identity room→room entry, and a descriptive "for X"
// tail promoted to a customer label. Both must be absent, leaving the model to apply rule 4.
func TestBuildActivityStatsHeader_RoomCustomerLineOmitsUnresolved(t *testing.T) {
	activity := []Log{
		{Task: "Provide Excel file matching yesterday's request", Room: "Internal Puspakom WhaTap IFC"},
		{Task: "Confirm agent Inactive handling condition for FIF telemetry data gap", Room: "biz-global-tech"},
		{Task: "Share config files for cleanup/archive guidance", Room: "Project WhaTap x Netciti"},
		{Task: "Align AI/ML architecture for Hepsiburada meeting", Room: "279516505182402"},
		{Task: "Next quarter roadmap meeting", Room: "Digital Transformation"},
		{Task: "Issue V1 product license for PDRM Malaysia next-generation system PoC", Room: "Gmail"},
	}

	out := buildActivityStatsHeader(activity, nil)

	forbidden := []string{
		"Internal Puspakom WhaTap IFC→Internal Puspakom WhaTap IFC",
		"Digital Transformation→Digital Transformation",
		"→FIF telemetry data gap",
		"→cleanup/archive guidance",
		"Gmail→PDRM Malaysia next-generation system PoC",
	}
	for _, f := range forbidden {
		if strings.Contains(out, f) {
			t.Errorf("Room→Customer must not contain %q; got: %s", f, out)
		}
	}

	expected := []string{
		"Internal Puspakom WhaTap IFC→Puspakom",
		"Project WhaTap x Netciti→Netciti",
		"Gmail→Other Tasks",
		"279516505182402→Other Tasks",
	}
	for _, e := range expected {
		if !strings.Contains(out, e) {
			t.Errorf("expected Room→Customer to contain %q; got: %s", e, out)
		}
	}
}

// --- hasRiskKeyword ---

func TestHasRiskKeyword(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"Verifying every case on my end isn't scalable", true},
		{"This is a blocker for the release", true},
		{"stuck waiting for approval", true},
		{"Everything looks good, no issues", true}, // "issue" matches in "issues"
		{"Normal progress update", false},
		{"", false},
		{"The task is complete", false},
	}
	for _, c := range cases {
		got := hasRiskKeyword(c.text)
		if got != c.want {
			t.Errorf("hasRiskKeyword(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

// --- inferCustomer ---

func TestInferCustomerFromTask(t *testing.T) {
	cases := []struct {
		task string
		want string
	}{
		{"WhaTap APM Tools POC for Bank BNI", "Bank BNI"},
		{"Discuss POC for Canadia Bank (Cambodia)", "Canadia Bank"},
		{"Monitoring for SIMASFIN", "SIMASFIN"},
		{"Internal planning meeting", ""},
		{"", ""},
		// Descriptive tails must not become customers: these three shipped into the
		// Room->Customer map as customer labels and the report model trusted them.
		{"Confirm agent Inactive handling condition for FIF telemetry data gap", ""},
		{"Share yard.conf, keeper.conf and project.conf for cleanup/archive guidance", ""},
		{"Issue V1 product license for PDRM Malaysia next-generation system PoC", ""},
		{"Prepare quotation for the renewal", ""},
	}
	for _, c := range cases {
		got := inferCustomerFromTask(c.task)
		if got != c.want {
			t.Errorf("inferCustomerFromTask(%q) = %q, want %q", c.task, got, c.want)
		}
	}
}

func TestInferCustomerFromRoom(t *testing.T) {
	cases := []struct {
		room string
		want string
	}{
		{"biz-global-malaysia", "Malaysia Biz"},
		{"biz-global-thailand", "Thailand Biz"},
		{"SIMASFIN-POC", "SIMASFIN"},
		{"Canadia-Whatap", "Canadia"},
		{"Gmail", "Other Tasks"},
		{"gmail-strategy", "Other Tasks"},
		{"slack", "Other Tasks"},
		{"Inbox", "Other Tasks"},
		{"", "Other Tasks"},
		// Vendor scaffolding stripped so the counterparty surfaces.
		{"Adira - Whatap Tech", "Adira"},
		{"PDRM POC - MSB | IFC | WhaTap", "PDRM"},
		{"Internal Puspakom WhaTap IFC", "Puspakom"},
		{"Project WhaTap x Netciti", "Netciti"},
		{"WiiTech / WhaTap", "WiiTech"},
		{"Whatap - Canadia POC", "Canadia"},
		// Unresolved: nothing to strip (empty means "keep it out of the map"), or the room
		// is a bare channel id.
		{"Digital Transformation", ""},
		{"WhaTap Internal", ""},
		// WhatsApp @lid chats arrive as a bare numeric id with no display name in our data,
		// so they bucket explicitly rather than inviting inference from the digits.
		{"279516505182402", "Other Tasks"},
		{"60122362207", "Other Tasks"},
	}
	for _, c := range cases {
		got := inferCustomerFromRoom(c.room)
		if got != c.want {
			t.Errorf("inferCustomerFromRoom(%q) = %q, want %q", c.room, got, c.want)
		}
	}
}

// --- sortStalledByAge ---

func TestSortStalledByAge(t *testing.T) {
	now := time.Now()
	logs := []Log{
		{Task: "new", CreatedAt: now.AddDate(0, 0, -2)},  // 2 days old → fewer working days
		{Task: "old", CreatedAt: now.AddDate(0, 0, -14)}, // 14 days old → more working days
		{Task: "mid", CreatedAt: now.AddDate(0, 0, -7)},  // 7 days old → middle
	}
	sortStalledByAge(logs)

	if logs[0].Task != "old" {
		t.Errorf("oldest task should be first after sortStalledByAge; got %s", logs[0].Task)
	}
	if logs[2].Task != "new" {
		t.Errorf("newest task should be last; got %s", logs[2].Task)
	}
}

// --- formatLogLine [RISK-CAND] integration ---

func TestFormatLogLine_RiskCandMarker(t *testing.T) {
	svc := &ReportsService{config: ReportConfig{CutoffSize: DefaultReportCutoffSize}, isTest: true}

	withRisk := store.ConsolidatedMessage{
		Task:         "Verify scalability",
		Category:     "TASK",
		Requester:    "Alice",
		Assignee:     "Bob",
		Done:         false,
		OriginalText: "Verifying every case on my end isn't scalable",
	}
	withoutRisk := store.ConsolidatedMessage{
		Task:         "Send weekly update",
		Category:     "TASK",
		Requester:    "Alice",
		Assignee:     "Bob",
		Done:         false,
		OriginalText: "All items reviewed and sent",
	}
	doneWithRisk := store.ConsolidatedMessage{
		Task:         "Fix blocker",
		Category:     "TASK",
		Requester:    "Alice",
		Assignee:     "Bob",
		Done:         true, // done → no evidence → no RISK-CAND
		OriginalText: "This was a blocker for deployment",
	}

	lineRisk := svc.formatLogLine("me@example.com", withRisk)
	lineNoRisk := svc.formatLogLine("me@example.com", withoutRisk)
	lineDone := svc.formatLogLine("me@example.com", doneWithRisk)

	if !strings.Contains(lineRisk, "[RISK-CAND]") {
		t.Errorf("expected [RISK-CAND] for risk evidence; got: %s", lineRisk)
	}
	if strings.Contains(lineNoRisk, "[RISK-CAND]") {
		t.Errorf("unexpected [RISK-CAND] for non-risk evidence; got: %s", lineNoRisk)
	}
	if strings.Contains(lineDone, "[RISK-CAND]") {
		t.Errorf("done task must not carry [RISK-CAND] (evidence skipped); got: %s", lineDone)
	}
}
