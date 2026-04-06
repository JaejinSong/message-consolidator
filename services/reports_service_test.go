package services

import (
	"context"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"strings"
	"testing"
	"time"
)

func TestReportsService_CalculateGraph(t *testing.T) {
	store.ResetForTest()
	svc := &ReportsService{config: ReportConfig{CutoffSize: 8000}}
	messages := []store.ConsolidatedMessage{
		{Requester: "Alice", Assignee: "JJ", Source: "slack"},
		{Requester: "Alice", Assignee: "JJ", Source: "slack"},
		{Requester: "Bob", Assignee: "JJ", Source: "whatsapp"},
		{Requester: "JJ", Assignee: "Alice", Source: "slack"},
	}

	graphData := svc.generateVisualizationData("me@example.com", messages)

	// Verify nodes
	foundAlice := false
	foundJJ := false
	for _, n := range graphData.Nodes {
		// Why: IDs are normalized to lowercase by NormalizeWithCategory.
		if n.ID == "alice" {
			foundAlice = true
			if n.Category != "External" {
				t.Errorf("Alice category expected External, got %s", n.Category)
			}
			if n.Value != 3 { // Requester 2 + Assignee 1
				t.Errorf("Alice value expected 3, got %f", n.Value)
			}
		}
		if n.ID == "jj" {
			foundJJ = true
			if n.Category != "External" {
				t.Errorf("JJ category expected External, got %s", n.Category)
			}
			if n.Value != 4 { // Assignee 3 + Requester 1
				t.Errorf("JJ value expected 4, got %f", n.Value)
			}
		}
	}

	if !foundAlice || !foundJJ {
		t.Errorf("Alice or jj node missing (Nodes: %+v)", graphData.Nodes)
	}

	// Verify edges
	if len(graphData.Links) < 1 {
		t.Errorf("Links should not be empty")
	}
}

func TestReportsService_TruncatePayload(t *testing.T) {
	svc := &ReportsService{config: ReportConfig{CutoffSize: 1000}}
	messages := []store.ConsolidatedMessage{}
	for i := 0; i < 50; i++ {
		messages = append(messages, store.ConsolidatedMessage{
			Task:      "Test Task " + strings.Repeat("a", 100),
			Requester: "Sender",
			Assignee:  "Receiver",
			CreatedAt: time.Now(),
		})
	}

	summary, isTruncated := svc.PrepareLogsForAI("me@example.com", messages)
	// Why: The internal cutoff is strictly 8,000 bytes.
	if len([]byte(summary)) > 8000 {
		t.Errorf("Summary too long: %d bytes (limit 8000)", len([]byte(summary)))
	}
	if !isTruncated {
		t.Errorf("Expected isTruncated to be true for large payload, got false")
	}
}

func TestReportsService_TruncatePriority(t *testing.T) {
	svc := &ReportsService{config: ReportConfig{CutoffSize: 300}}
	now := time.Now()

	// 1 old, incomplete task
	oldIncomplete := store.ConsolidatedMessage{
		Task:      "URGENT OLD TASK",
		Requester: "System",
		Assignee:  "User",
		Done:      false,
		CreatedAt: now.Add(-48 * time.Hour),
	}

	messages := []store.ConsolidatedMessage{oldIncomplete}

	// 20 new, completed tasks (these should be truncated if limit is small)
	for i := 0; i < 20; i++ {
		messages = append(messages, store.ConsolidatedMessage{
			Task:      "Completed Task " + strings.Repeat("a", 100),
			Requester: "Sender",
			Assignee:  "Receiver",
			Done:      true,
			CreatedAt: now.Add(time.Duration(-i) * time.Hour),
		})
	}

	// Set limit to only allow about 2-3 lines
	summary, _ := svc.PrepareLogsForAI("me@example.com", messages)

	if !strings.Contains(summary, "URGENT OLD TASK") {
		t.Errorf("Critical incomplete old task was truncated, but it should have been prioritized")
	}

	if !strings.Contains(summary, "- [ ] URGENT OLD TASK") {
		t.Errorf("Task status mark should be [ ]; got summary: %s", summary)
	}
}

func TestReportsService_Normalization(t *testing.T) {
	store.ResetForTest()
	svc := &ReportsService{config: ReportConfig{CutoffSize: 8000}}
	messages := []store.ConsolidatedMessage{
		{Requester: "jj@whatap.io", Assignee: "JJ", Source: "slack"},
		{Requester: "JJ", Assignee: "jj@whatap.io", Source: "slack"},
	}

	// Case 1: Without aliases, they should be separate with domain-based labels
	graphData := svc.generateVisualizationData("jj@whatap.io", messages)
	// Nodes: "jj@whatap.io (Internal)", "JJ (External)"
	if len(graphData.Nodes) != 2 {
		t.Errorf("Expected 2 nodes without aliases, got %d", len(graphData.Nodes))
	}

	foundInternal := false
	foundExternal := false
	for _, n := range graphData.Nodes {
		if n.Category == "Internal" {
			foundInternal = true
		}
		if n.Category == "External" {
			foundExternal = true
		}
	}

	if !foundInternal || !foundExternal {
		t.Errorf("Internal or External category flag missing: %+v", graphData.Nodes)
	}
}

func TestReportsService_Labeling(t *testing.T) {
	store.ResetForTest()
	svc := &ReportsService{config: ReportConfig{CutoffSize: 8000}}
	messages := []store.ConsolidatedMessage{
		{Requester: "JJ", Assignee: "Alice", Source: "slack"},
	}

	// jj@whatap.io -> should be (Internal) by domain
	// JJ -> should be (External) by default
	// Alice -> should be (External) by default
	graphData := svc.generateVisualizationData("jj@whatap.io", messages)

	foundAlice := false
	for _, n := range graphData.Nodes {
		if n.ID == "alice" && n.Category == "External" {
			foundAlice = true
		}
	}
	if !foundAlice {
		t.Fatalf("Expected alice with category External, but not found. Nodes: %+v", graphData.Nodes)
	}
}

func TestReportsService_SankeyContract(t *testing.T) {
	store.ResetForTest()
	svc := &ReportsService{config: ReportConfig{CutoffSize: 8000}}

	messages := []store.ConsolidatedMessage{
		// 1. Normal link.
		{Requester: "alice@whatap.io", Assignee: "bob@whatap.io", Task: "task1"},
		// 2. Self-loop with mixed case (should be ignored).
		{Requester: "alice@whatap.io", Assignee: "Alice@whatap.io", Task: "self-loop-1"},
		// 3. Another mixed-case self-loop variant (should be ignored).
		{Requester: "bob@whatap.io", Assignee: "Bob@Whatap.io", Task: "self-loop-2"},
		// 4. Duplicate link with case difference (should be merged into one link).
		{Requester: "ALICE@whatap.io", Assignee: "bob@whatap.io", Task: "task2"},
		// 5. External user with no alias.
		{Requester: "external-user@gmail.com", Assignee: "alice@whatap.io", Task: "external-task"},
		// 6. External user with dots in name (for name extraction policy test).
		{Requester: "hady.partner@gmail.com", Assignee: "alice@whatap.io", Task: "external-task-2"},
	}

	graphData := svc.generateVisualizationData("alice@whatap.io", messages)

	// Check for self-loop removal.
	if len(graphData.Links) != 3 {
		t.Errorf("Expected 3 links after ignoring self-loops, got %d. Links: %+v", len(graphData.Links), graphData.Links)
	}
	for _, l := range graphData.Links {
		if l.Source == l.Target {
			t.Errorf("Self-loop detected in links: %+v", l)
		}
	}

	// Check ID unification (all IDs must be lowercase).
	nodesFound := make(map[string]bool)
	for _, n := range graphData.Nodes {
		if n.ID != strings.ToLower(n.ID) {
			t.Errorf("Node ID not lowercased: %s", n.ID)
		}
		nodesFound[n.ID] = true
	}

	// Check name fallback policy for external users.
	var externalUserNode, hadyPartnerNode Node
	foundExternal, foundHady := false, false
	for _, n := range graphData.Nodes {
		if n.ID == "external-user@gmail.com" {
			foundExternal, externalUserNode = true, n
		}
		if n.ID == "hady.partner@gmail.com" {
			foundHady, hadyPartnerNode = true, n
		}
	}
	if !foundExternal {
		t.Errorf("External node 'external-user@gmail.com' missing")
	}
	if externalUserNode.Name != "external-user@gmail.com (External)" {
		t.Errorf("Expected fallback name for external user to be 'external-user@gmail.com (External)', got '%s'", externalUserNode.Name)
	}

	if !foundHady {
		t.Errorf("External node 'hady.partner@gmail.com' missing")
	}
	// NOTE: The current logic in `NormalizeWithCategory` returns the full email as the name for unknown external users.
	// This test asserts the current, correct behavior.
	if hadyPartnerNode.Name != "hady.partner@gmail.com (External)" {
		t.Errorf("Expected fallback name for hady.partner to be 'hady.partner@gmail.com (External)', got '%s'", hadyPartnerNode.Name)
	}

	// Check link consistency (all source/target IDs must exist in nodes).
	for _, l := range graphData.Links {
		if !nodesFound[l.Source] {
			t.Errorf("Link source %s missing from nodes", l.Source)
		}
		if !nodesFound[l.Target] {
			t.Errorf("Link target %s missing from nodes", l.Target)
		}
	}
}

func TestReportsService_GenerateVisualizationData_WithAliases(t *testing.T) {
	// This test requires a database setup to test the integration with the store's aliasing.
	// We assume a test utility `testutil.SetupTestDB(store.InitDB, store.ResetForTest)` exists, similar to other service tests.
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	svc := &ReportsService{config: ReportConfig{CutoffSize: 8000}}
	tenantEmail := "admin@whatap.io"

	// 1. Create users
	userJJ, err := store.GetOrCreateUser(context.Background(), "jjsong@whatap.io", "Jaejin Song", "")
	if err != nil {
		t.Fatalf("Failed to create user jj: %v", err)
	}
	_, err = store.GetOrCreateUser(context.Background(), "alice@whatap.io", "Alice", "")
	if err != nil {
		t.Fatalf("Failed to create user alice: %v", err)
	}

	// 2. Add aliases to DB
	if err := store.AddUserAlias(context.Background(), userJJ.ID, "JJ"); err != nil {
		t.Fatalf("Failed to add user alias: %v", err)
	}
	if err := store.AddTenantAlias(context.Background(), tenantEmail, "Song, SongV2, jjsong@whatap.io", "Jaejin Song"); err != nil {
		t.Fatalf("Failed to add tenant alias: %v", err)
	}

	// 3. Manually refresh caches to load the new aliases from DB
	if err := store.LoadMetadata(); err != nil {
		t.Fatalf("Failed to load metadata into cache: %v", err)
	}

	// 4. Define messages using various aliases
	messages := []store.ConsolidatedMessage{
		{Requester: "JJ", Assignee: "Alice"},               // User Alias
		{Requester: "Song", Assignee: "alice@whatap.io"},   // Tenant Alias (name only)
		{Requester: "Jaejin Song", Assignee: "Alice"},      // Real Name
		{Requester: "jjsong@whatap.io", Assignee: "Alice"}, // Email
		{Requester: "SongV2", Assignee: "alice@whatap.io"}, // Tenant Alias (with email hint)
	}

	// 5. Generate graph data
	graphData := svc.generateVisualizationData(tenantEmail, messages)

	// 6. Assertions
	if len(graphData.Nodes) != 2 {
		t.Fatalf("Expected 2 nodes, but got %d. Nodes: %+v", len(graphData.Nodes), graphData.Nodes)
	}

	var nodeJJ, nodeAlice Node
	for _, n := range graphData.Nodes {
		switch n.ID {
		case "jjsong@whatap.io":
			nodeJJ = n
		case "alice@whatap.io":
			nodeAlice = n
		}
	}

	// Assert JJ's node
	if nodeJJ.ID == "" {
		t.Fatalf("Node for 'jjsong@whatap.io' not found")
	}
	if nodeJJ.Name != "Jaejin Song (Internal)" {
		t.Errorf("Expected JJ's name to be 'Jaejin Song (Internal)', got '%s'", nodeJJ.Name)
	}
	if nodeJJ.Value != 5 {
		t.Errorf("Expected JJ's value to be 5, got %f", nodeJJ.Value)
	}

	// Assert Alice's node
	if nodeAlice.ID == "" {
		t.Fatalf("Node for 'alice@whatap.io' not found")
	}
	if nodeAlice.Value != 5 {
		t.Errorf("Expected Alice's value to be 5, got %f", nodeAlice.Value)
	}

	// Assert Links
	if len(graphData.Links) != 1 {
		t.Fatalf("Expected 1 link, but got %d. Links: %+v", len(graphData.Links), graphData.Links)
	}
}

func TestReportsService_GenerateVisualizationData_AliasCollision(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	svc := &ReportsService{config: ReportConfig{CutoffSize: 8000}}
	tenantEmail := "admin@whatap.io"

	// 1. Create two different users with the same name.
	_, err = store.GetOrCreateUser(context.Background(), "alice.a@whatap.io", "Alice", "")
	if err != nil {
		t.Fatalf("Failed to create user alice.a: %v", err)
	}
	_, err = store.GetOrCreateUser(context.Background(), "alice.b@whatap.io", "Alice", "")
	if err != nil {
		t.Fatalf("Failed to create user alice.b: %v", err)
	}
	_, err = store.GetOrCreateUser(context.Background(), "charlie@whatap.io", "Charlie", "")
	if err != nil {
		t.Fatalf("Failed to create user charlie: %v", err)
	}

	// 2. Manually refresh caches.
	if err := store.LoadMetadata(); err != nil {
		t.Fatalf("Failed to load metadata into cache: %v", err)
	}

	// 3. Define messages using specific email IDs.
	messages := []store.ConsolidatedMessage{
		{Requester: "alice.a@whatap.io", Assignee: "charlie@whatap.io"},
		{Requester: "alice.b@whatap.io", Assignee: "charlie@whatap.io"},
	}

	// 4. Generate graph data.
	graphData := svc.generateVisualizationData(tenantEmail, messages)

	// 5. Assertions: It should create two separate nodes for each "Alice" because their IDs (emails) are different.
	if len(graphData.Nodes) != 3 {
		t.Fatalf("Expected 3 distinct nodes, but got %d. Nodes: %+v", len(graphData.Nodes), graphData.Nodes)
	}

	var nodeAliceA, nodeAliceB Node
	foundA, foundB := false, false
	for _, n := range graphData.Nodes {
		switch n.ID {
		case "alice.a@whatap.io":
			nodeAliceA, foundA = n, true
		case "alice.b@whatap.io":
			nodeAliceB, foundB = n, true
		}
	}

	if !foundA || !foundB {
		t.Fatalf("Expected to find two separate nodes for 'alice.a' and 'alice.b', but one or both are missing. Found A: %v, Found B: %v", foundA, foundB)
	}

	if nodeAliceA.Name != "Alice (Internal)" {
		t.Errorf("Expected node 'alice.a' to have name 'Alice (Internal)', got '%s'", nodeAliceA.Name)
	}
	if nodeAliceB.Name != "Alice (Internal)" {
		t.Errorf("Expected node 'alice.b' to have name 'Alice (Internal)', got '%s'", nodeAliceB.Name)
	}
}

func TestReportsService_GenerateVisualizationData_TenantIsolation(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	svc := &ReportsService{config: ReportConfig{CutoffSize: 8000}}
	tenantA := "tenant-a@company.com"
	tenantB := "tenant-b@company.com"

	// 1. Create a user and an alias for Tenant B.
	_, err = store.GetOrCreateUser(context.Background(), "jjsong@whatap.io", "Jaejin Song", "")
	if err != nil {
		t.Fatalf("Failed to create user jj: %v", err)
	}
	if err := store.AddTenantAlias(context.Background(), tenantB, "Song", "Jaejin Song"); err != nil {
		t.Fatalf("Failed to add tenant alias for tenant B: %v", err)
	}

	// 2. Manually refresh caches.
	if err := store.LoadMetadata(); err != nil {
		t.Fatalf("Failed to load metadata into cache: %v", err)
	}

	// 3. Define a message using the alias.
	messages := []store.ConsolidatedMessage{
		{Requester: "Song", Assignee: "someone-else@whatap.io"},
	}

	// 4. Generate graph data for Tenant A and assert that Tenant B's alias was NOT applied.
	graphDataA := svc.generateVisualizationData(tenantA, messages)
	foundGenericSong := false
	for _, n := range graphDataA.Nodes {
		if n.ID == "song" && n.Category == "External" {
			foundGenericSong = true
			break
		}
	}
	if !foundGenericSong {
		t.Fatalf("Expected a generic external node 'song' for tenant A, but it was resolved. Nodes: %+v", graphDataA.Nodes)
	}

	// 5. Generate graph data for Tenant B and assert that Tenant B's alias WAS applied.
	graphDataB := svc.generateVisualizationData(tenantB, messages)
	foundResolvedSong := false
	for _, n := range graphDataB.Nodes {
		if n.ID == "jjsong@whatap.io" && n.Name == "Jaejin Song (Internal)" {
			foundResolvedSong = true
			break
		}
	}
	if !foundResolvedSong {
		t.Fatalf("Expected 'Song' to be resolved to 'jjsong@whatap.io' for tenant B, but it was not. Nodes: %+v", graphDataB.Nodes)
	}
}
func TestReportsService_GenerateReport_MultiLanguage(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	// Mock Summarizer
	mockSummary := "Mocked English Report"
	// summarizer := NewFlashSingleSummarizer(nil) // (ID: c7755db8) Removed unused variable

	svc := NewReportsService(&mockSummarizer{
		generateFunc: func(ctx context.Context, logs string) (string, error) {
			return mockSummary, nil
		},
	}, nil, nil, ReportConfig{CutoffSize: 8000})

	tenantEmail := "test@example.com"
	startDate := "2026-03-29"
	endDate := "2026-03-29"
	fixedTime, _ := time.Parse("2006-01-02 15:04:05", "2026-03-29 10:00:00")

	// Pre-seed some messages
	_, err = store.GetDB().Exec("INSERT INTO messages (user_email, source, task, created_at, requester, assignee) VALUES (?, ?, ?, ?, ?, ?)",
		tenantEmail, "slack", "Task 1", fixedTime, "Alice", "JJ")
	if err != nil {
		t.Fatalf("Failed to seed message: %v", err)
	}

	// Since we can't easily mock the internal GeminiClient.TranslateReport call without a real object,
	report, err := svc.GenerateReport(context.Background(), tenantEmail, startDate, endDate, "en")
	if err != nil {
		t.Fatalf("GenerateReport failed: %v", err)
	}

	if report.ID == 0 {
		t.Error("Expected non-zero report ID")
	}

	// Verify English translation exists in DB
	translations, err := store.GetReportTranslations(context.Background(), report.ID)
	if err != nil {
		t.Fatalf("GetReportTranslations failed: %v", err)
	}

	summaryEN, foundEN := translations["en"]
	if !foundEN {
		t.Error("English translation ('en') not found in report_translations map")
	} else if summaryEN != mockSummary {
		t.Errorf("Expected summary %s, got %s", mockSummary, summaryEN)
	}
}

type mockSummarizer struct {
	generateFunc func(ctx context.Context, logs string) (string, error)
}

func (m *mockSummarizer) Generate(ctx context.Context, logs string) (string, error) {
	return m.generateFunc(ctx, logs)
}

func TestReportsService_CacheHit(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	callCount := 0
	svc := NewReportsService(&mockSummarizer{
		generateFunc: func(ctx context.Context, logs string) (string, error) {
			callCount++
			return "AI Generated Report", nil
		},
	}, nil, nil, ReportConfig{CutoffSize: 8000})

	email := "user@example.com"
	start, end := "2024-01-01", "2024-01-07"

	// Pre-seed a message to avoid "no content" error if any
	fixedTime, _ := time.Parse("2006-01-02", start)
	_, _ = store.GetDB().Exec("INSERT INTO messages (user_email, source, task, created_at, requester, assignee) VALUES (?, ?, ?, ?, ?, ?)",
		email, "slack", "Task 1", fixedTime, "Alice", "JJ")

	// 1. First Call: Should hit AI
	ctx := context.Background()
	_, err = svc.GenerateReport(ctx, email, start, end, "ko")
	if err != nil {
		t.Fatalf("First call failed: %v", err)
	}
	if callCount != 1 {
		t.Errorf("Expected callCount 1, got %d", callCount)
	}

	// 2. Second Call: Should hit Cache (DB)
	_, err = svc.GenerateReport(ctx, email, start, end, "ko")
	if err != nil {
		t.Fatalf("Second call failed: %v", err)
	}
	if callCount != 1 {
		t.Errorf("Expected callCount 1 (cache hit), got %d", callCount)
	}
}

func TestReportsService_GenerateReport_OnlyRequestedLanguage(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	svc := NewReportsService(&mockSummarizer{
		generateFunc: func(ctx context.Context, logs string) (string, error) {
			return "AI Generated Summary", nil
		},
	}, nil, nil, ReportConfig{CutoffSize: 8000})

	email := "user@example.com"
	start := "2026-04-01"
	// Pre-seed message
	fixedTime, _ := time.Parse("2006-01-02", start)
	_, _ = store.GetDB().Exec("INSERT INTO messages (user_email, source, task, created_at, requester, assignee) VALUES (?, ?, ?, ?, ?, ?)",
		email, "slack", "Task 1", fixedTime, "Alice", "JJ")

	ctx := context.Background()
	report, err := svc.GenerateReport(ctx, email, start, start, "ko")
	if err != nil {
		t.Fatalf("GenerateReport failed: %v", err)
	}

	// Verify only 'ko' is in translations map (or at least only one lang if not 'en')
	// Since ProcessOnDemandTranslation is called for 'ko', and I didn't mock TranslationService, 
	// I should at least check that only 'ko' is present in the returned object's Translations map.
	if len(report.Translations) != 1 {
		t.Errorf("Expected exactly 1 translation, got %d: %+v", len(report.Translations), report.Translations)
	}
	if _, ok := report.Translations["ko"]; !ok {
		t.Errorf("Expected 'ko' translation to be present")
	}
	
	// Ensure other languages like 'id' or 'th' are NOT present
	if _, ok := report.Translations["id"]; ok {
		t.Error("Unexpected 'id' translation found")
	}
}
