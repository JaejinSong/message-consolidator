package scanner

import (
	"context"
	"errors"
	"sync"
	"testing"

	"message-consolidator/config"
)

type fakeReminderDispatcher struct {
	called                 int
	undatedCalled          int
	stalledReconfirmCalled int
	err                    error
}

func (f *fakeReminderDispatcher) DispatchDueSoon(_ context.Context) error {
	f.called++
	return f.err
}

func (f *fakeReminderDispatcher) DispatchUndated(_ context.Context) error {
	f.undatedCalled++
	return f.err
}

func (f *fakeReminderDispatcher) DispatchStalledReconfirm(_ context.Context) error {
	f.stalledReconfirmCalled++
	return f.err
}

func TestRunDeadlineReminder_NilSvc(t *testing.T) {
	origSvc := deps.reminderSvc
	origCfg := cfg
	t.Cleanup(func() { deps.reminderSvc = origSvc; cfg = origCfg })

	deps.reminderSvc = nil
	// Why: cfg is not set to avoid nil deref — the nil-svc guard returns before cfg is accessed.
	cfg = nil

	runDeadlineReminder(context.Background(), &sync.WaitGroup{})
}

func TestRunDeadlineReminder_Disabled(t *testing.T) {
	origSvc := deps.reminderSvc
	origCfg := cfg
	t.Cleanup(func() { deps.reminderSvc = origSvc; cfg = origCfg })

	fake := &fakeReminderDispatcher{}
	deps.reminderSvc = fake
	cfg = &config.Config{ReminderEnabled: false}

	runDeadlineReminder(context.Background(), &sync.WaitGroup{})

	if fake.called != 0 {
		t.Errorf("called = %d, want 0", fake.called)
	}
}

func TestRunDeadlineReminder_Dispatched(t *testing.T) {
	origSvc := deps.reminderSvc
	origCfg := cfg
	t.Cleanup(func() { deps.reminderSvc = origSvc; cfg = origCfg })

	fake := &fakeReminderDispatcher{}
	deps.reminderSvc = fake
	cfg = &config.Config{ReminderEnabled: true}

	runDeadlineReminder(context.Background(), &sync.WaitGroup{})

	if fake.called != 1 {
		t.Errorf("called = %d, want 1", fake.called)
	}
}

func TestRunDeadlineReminder_DispatchError(t *testing.T) {
	origSvc := deps.reminderSvc
	origCfg := cfg
	t.Cleanup(func() { deps.reminderSvc = origSvc; cfg = origCfg })

	fake := &fakeReminderDispatcher{err: errors.New("boom")}
	deps.reminderSvc = fake
	cfg = &config.Config{ReminderEnabled: true}

	runDeadlineReminder(context.Background(), &sync.WaitGroup{})

	if fake.called != 1 {
		t.Errorf("called = %d, want 1", fake.called)
	}
}

func TestRunStalledReconfirm_NilSvc(t *testing.T) {
	origSvc := deps.reminderSvc
	origCfg := cfg
	t.Cleanup(func() { deps.reminderSvc = origSvc; cfg = origCfg })

	deps.reminderSvc = nil
	// Why: cfg is not set to avoid nil deref — the nil-svc guard returns before cfg is accessed.
	cfg = nil

	runStalledReconfirm(context.Background(), &sync.WaitGroup{})
}

func TestRunStalledReconfirm_Disabled(t *testing.T) {
	origSvc := deps.reminderSvc
	origCfg := cfg
	t.Cleanup(func() { deps.reminderSvc = origSvc; cfg = origCfg })

	fake := &fakeReminderDispatcher{}
	deps.reminderSvc = fake
	cfg = &config.Config{ReminderEnabled: false}

	runStalledReconfirm(context.Background(), &sync.WaitGroup{})

	if fake.stalledReconfirmCalled != 0 {
		t.Errorf("stalledReconfirmCalled = %d, want 0", fake.stalledReconfirmCalled)
	}
}

func TestRunStalledReconfirm_Dispatched(t *testing.T) {
	origSvc := deps.reminderSvc
	origCfg := cfg
	t.Cleanup(func() { deps.reminderSvc = origSvc; cfg = origCfg })

	fake := &fakeReminderDispatcher{}
	deps.reminderSvc = fake
	cfg = &config.Config{ReminderEnabled: true}

	runStalledReconfirm(context.Background(), &sync.WaitGroup{})

	if fake.stalledReconfirmCalled != 1 {
		t.Errorf("stalledReconfirmCalled = %d, want 1", fake.stalledReconfirmCalled)
	}
}

func TestRunStalledReconfirm_DispatchError(t *testing.T) {
	origSvc := deps.reminderSvc
	origCfg := cfg
	t.Cleanup(func() { deps.reminderSvc = origSvc; cfg = origCfg })

	fake := &fakeReminderDispatcher{err: errors.New("boom")}
	deps.reminderSvc = fake
	cfg = &config.Config{ReminderEnabled: true}

	runStalledReconfirm(context.Background(), &sync.WaitGroup{})

	if fake.stalledReconfirmCalled != 1 {
		t.Errorf("stalledReconfirmCalled = %d, want 1", fake.stalledReconfirmCalled)
	}
}
