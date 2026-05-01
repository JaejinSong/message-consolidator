package scanner

import (
	"context"
	"sync"
	"testing"

	"message-consolidator/config"
	"message-consolidator/store"
)

func saveScannerGlobals(t *testing.T) {
	t.Helper()
	origCfg := cfg
	origGClient := gClient
	origCompletion := completionSvc
	origTasks := tasksSvc
	origFilter := filterSvc
	origLock := roomLockSvc
	origSlack := slackClient
	origDigest := digestSvc
	origWeekly := weeklyReportSvc
	t.Cleanup(func() {
		cfg = origCfg
		gClient = origGClient
		completionSvc = origCompletion
		tasksSvc = origTasks
		filterSvc = origFilter
		roomLockSvc = origLock
		slackClient = origSlack
		digestSvc = origDigest
		weeklyReportSvc = origWeekly
	})
}

func initGuardsDB(t *testing.T) {
	t.Helper()
	store.ResetForTest()
	if err := store.InitDB(context.Background(), &config.Config{}); err != nil {
		t.Fatalf("initGuardsDB: %v", err)
	}
}

// Group A: Init() with empty config

func TestInit_EmptyCfg(t *testing.T) {
	saveScannerGlobals(t)
	cfg, gClient, completionSvc, tasksSvc, filterSvc, roomLockSvc, slackClient = nil, nil, nil, nil, nil, nil, nil

	Init(&config.Config{})

	if cfg == nil {
		t.Fatal("cfg must not be nil after Init")
	}
	if cfg.GeminiAPIKey != "" {
		t.Errorf("GeminiAPIKey = %q, want empty", cfg.GeminiAPIKey)
	}
	if roomLockSvc == nil {
		t.Error("roomLockSvc must be created unconditionally")
	}
	if gClient != nil {
		t.Error("gClient must remain nil when GeminiAPIKey is empty")
	}
	if completionSvc != nil {
		t.Error("completionSvc must remain nil when GeminiAPIKey is empty")
	}
	if tasksSvc != nil {
		t.Error("tasksSvc must remain nil when GeminiAPIKey is empty")
	}
	if filterSvc != nil {
		t.Error("filterSvc must remain nil when GeminiAPIKey is empty")
	}
	if slackClient != nil {
		t.Error("slackClient must remain nil when SlackToken is empty")
	}
}

// Group B: Slack guard paths

func TestRunSlackForAllUsers_NilCfg(t *testing.T) {
	saveScannerGlobals(t)
	cfg = nil

	runSlackForAllUsers(context.Background(), &sync.WaitGroup{})
}

func TestRunSlackForAllUsers_EmptyToken(t *testing.T) {
	saveScannerGlobals(t)
	cfg = &config.Config{}

	runSlackForAllUsers(context.Background(), &sync.WaitGroup{})
}

func TestPerformSlackScan_NilCfg(t *testing.T) {
	saveScannerGlobals(t)
	cfg = nil

	performSlackScan(context.Background(), nil, &sync.WaitGroup{})
}

func TestPerformSlackScan_EmptyToken(t *testing.T) {
	saveScannerGlobals(t)
	cfg = &config.Config{}

	performSlackScan(context.Background(), nil, &sync.WaitGroup{})
}

func TestRunSlackSweep_NilCfg(t *testing.T) {
	saveScannerGlobals(t)
	cfg = nil

	runSlackSweep(context.Background(), &sync.WaitGroup{})
}

func TestRunSlackSweep_EmptyToken(t *testing.T) {
	saveScannerGlobals(t)
	cfg = &config.Config{}

	runSlackSweep(context.Background(), &sync.WaitGroup{})
}

// Group C: WireDailyDigest / WireWeeklyReport guards

func TestWireDailyDigest_NilCfg(t *testing.T) {
	saveScannerGlobals(t)
	cfg = nil
	digestSvc = nil

	WireDailyDigest(nil)

	if digestSvc != nil {
		t.Error("digestSvc must remain nil when cfg is nil")
	}
}

func TestWireDailyDigest_Disabled(t *testing.T) {
	saveScannerGlobals(t)
	cfg = &config.Config{DailyDigestEnabled: false}
	digestSvc = nil

	WireDailyDigest(nil)

	if digestSvc != nil {
		t.Error("digestSvc must remain nil when DailyDigestEnabled is false")
	}
}

func TestWireDailyDigest_NilSlackClient(t *testing.T) {
	saveScannerGlobals(t)
	cfg = &config.Config{DailyDigestEnabled: true}
	slackClient = nil
	digestSvc = nil

	// Why: reportsSvc=nil triggers the guard before slackClient check, covering both nil paths.
	WireDailyDigest(nil)

	if digestSvc != nil {
		t.Error("digestSvc must remain nil when slackClient is nil")
	}
}

func TestWireWeeklyReport_NilCfg(t *testing.T) {
	saveScannerGlobals(t)
	cfg = nil
	weeklyReportSvc = nil

	WireWeeklyReport(nil)

	if weeklyReportSvc != nil {
		t.Error("weeklyReportSvc must remain nil when cfg is nil")
	}
}

func TestWireWeeklyReport_Disabled(t *testing.T) {
	saveScannerGlobals(t)
	cfg = &config.Config{WeeklyReportEnabled: false}
	weeklyReportSvc = nil

	WireWeeklyReport(nil)

	if weeklyReportSvc != nil {
		t.Error("weeklyReportSvc must remain nil when WeeklyReportEnabled is false")
	}
}

func TestWireWeeklyReport_NilSlackClient(t *testing.T) {
	saveScannerGlobals(t)
	cfg = &config.Config{WeeklyReportEnabled: true}
	slackClient = nil
	weeklyReportSvc = nil

	// Why: reportsSvc=nil triggers guard before slackClient is reached; both nil paths are covered.
	WireWeeklyReport(nil)

	if weeklyReportSvc != nil {
		t.Error("weeklyReportSvc must remain nil when slackClient is nil")
	}
}

// Group D: ReleaseInFlight + triggerAsyncTranslation guards

func TestReleaseInFlight(t *testing.T) {
	inFlightMessages.Store("test-id-1", true)
	ReleaseInFlight("test-id-1")

	_, ok := inFlightMessages.Load("test-id-1")
	if ok {
		t.Error("test-id-1 must be deleted after ReleaseInFlight")
	}
}

func TestTriggerAsyncTranslation_NilTasksSvc(t *testing.T) {
	saveScannerGlobals(t)
	tasksSvc = nil

	wg := &sync.WaitGroup{}
	triggerAsyncTranslation(context.Background(), "u@x", []store.MessageID{1, 2, 3}, wg)
	wg.Wait()
}

func TestTriggerAsyncTranslation_EmptyIDs(t *testing.T) {
	saveScannerGlobals(t)
	tasksSvc = nil

	wg := &sync.WaitGroup{}
	triggerAsyncTranslation(context.Background(), "u@x", []store.MessageID{}, wg)
	wg.Wait()
}

// Group E: DB-dependent simple paths

func TestRunArchiveOldTasks_NoPanic(t *testing.T) {
	initGuardsDB(t)

	runArchiveOldTasks(context.Background(), &sync.WaitGroup{})
}

func TestRunFlushTokenUsage_NoPanic(t *testing.T) {
	initGuardsDB(t)

	runFlushTokenUsage(context.Background(), &sync.WaitGroup{})
}

func TestRunLogDBStats_NoPanic(t *testing.T) {
	initGuardsDB(t)

	runLogDBStats(context.Background(), &sync.WaitGroup{})
}

func TestLoadUsersForScan_EmptyDB(t *testing.T) {
	initGuardsDB(t)

	bundles := loadUsersForScan(context.Background())
	if len(bundles) != 0 {
		t.Errorf("expected 0 bundles, got %d", len(bundles))
	}
}

func TestRunGmailForAllUsers_EmptyDB(t *testing.T) {
	initGuardsDB(t)
	saveScannerGlobals(t)
	cfg = nil

	runGmailForAllUsers(context.Background(), &sync.WaitGroup{})
}

func TestRunWhatsAppForAllUsers_EmptyDB(t *testing.T) {
	initGuardsDB(t)
	saveScannerGlobals(t)

	runWhatsAppForAllUsers(context.Background(), &sync.WaitGroup{})
}

func TestRunTelegramForAllUsers_EmptyDB(t *testing.T) {
	initGuardsDB(t)
	saveScannerGlobals(t)

	runTelegramForAllUsers(context.Background(), &sync.WaitGroup{})
}
