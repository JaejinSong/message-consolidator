package logger

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
)

func TestSetLevel(t *testing.T) {
	orig := currentLevel
	t.Cleanup(func() { currentLevel = orig })

	SetLevel("DEBUG")
	if currentLevel != LevelDebug {
		t.Errorf("currentLevel = %d, want %d (DEBUG)", currentLevel, LevelDebug)
	}
	SetLevel("WARN")
	if currentLevel != LevelWarn {
		t.Errorf("currentLevel = %d, want %d (WARN)", currentLevel, LevelWarn)
	}
	SetLevel("unknown") // should be no-op
	if currentLevel != LevelWarn {
		t.Errorf("unknown level changed currentLevel to %d", currentLevel)
	}
	SetLevel("error")
	if currentLevel != LevelError {
		t.Errorf("lowercase 'error' not handled, got %d", currentLevel)
	}
}

func TestGetLogDir_EnvOverride(t *testing.T) {
	t.Setenv("LOG_DIR", "/tmp/testlogs")
	if got := getLogDir(); got != "/tmp/testlogs" {
		t.Errorf("getLogDir = %q, want /tmp/testlogs", got)
	}
}

func TestGetLogDir_Default(t *testing.T) {
	t.Setenv("LOG_DIR", "")
	if got := getLogDir(); got != "/app/logs" {
		t.Errorf("getLogDir = %q, want /app/logs", got)
	}
}

func captureLog(fn func()) string {
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(orig)
	fn()
	return buf.String()
}

func TestDebugf_OutputWhenLevelDebug(t *testing.T) {
	orig := currentLevel
	t.Cleanup(func() { currentLevel = orig })
	currentLevel = LevelDebug

	out := captureLog(func() { Debugf("hello %s", "world") })
	if !strings.Contains(out, "[DEBUG]") || !strings.Contains(out, "hello world") {
		t.Errorf("Debugf output = %q, want [DEBUG] hello world", out)
	}
}

func TestDebugf_SuppressedWhenLevelInfo(t *testing.T) {
	orig := currentLevel
	t.Cleanup(func() { currentLevel = orig })
	currentLevel = LevelInfo

	out := captureLog(func() { Debugf("should not appear") })
	if strings.Contains(out, "should not appear") {
		t.Errorf("Debugf output leaked at INFO level: %q", out)
	}
}

func TestInfof_OutputWhenLevelInfo(t *testing.T) {
	orig := currentLevel
	t.Cleanup(func() { currentLevel = orig })
	currentLevel = LevelInfo

	out := captureLog(func() { Infof("info message %d", 42) })
	if !strings.Contains(out, "[INFO]") || !strings.Contains(out, "info message 42") {
		t.Errorf("Infof output = %q", out)
	}
}

func TestInfof_SuppressedWhenLevelWarn(t *testing.T) {
	orig := currentLevel
	t.Cleanup(func() { currentLevel = orig })
	currentLevel = LevelWarn

	out := captureLog(func() { Infof("should not appear") })
	if strings.Contains(out, "should not appear") {
		t.Errorf("Infof leaked at WARN level: %q", out)
	}
}

func TestWarnf_Output(t *testing.T) {
	orig := currentLevel
	t.Cleanup(func() { currentLevel = orig })
	currentLevel = LevelWarn

	out := captureLog(func() { Warnf("warn %s", "here") })
	if !strings.Contains(out, "[WARN]") || !strings.Contains(out, "warn here") {
		t.Errorf("Warnf output = %q", out)
	}
}

func TestErrorf_Output(t *testing.T) {
	orig := currentLevel
	t.Cleanup(func() { currentLevel = orig })
	currentLevel = LevelError

	out := captureLog(func() { Errorf("err %v", 99) })
	if !strings.Contains(out, "[ERROR]") || !strings.Contains(out, "err 99") {
		t.Errorf("Errorf output = %q", out)
	}
}

func TestLogDecision_OutputsJSON(t *testing.T) {
	orig := currentLevel
	t.Cleanup(func() { currentLevel = orig })
	currentLevel = LevelInfo

	out := captureLog(func() {
		LogDecision(DecisionLog{
			UserEmail: "u@x.com",
			Source:    "slack",
			State:     "new",
			Task:      "do thing",
		})
	})
	if !strings.Contains(out, "[DECISION]") || !strings.Contains(out, "u@x.com") {
		t.Errorf("LogDecision output = %q", out)
	}
}

func TestLogDecision_SetsTimestampWhenZero(t *testing.T) {
	orig := currentLevel
	t.Cleanup(func() { currentLevel = orig })
	currentLevel = LevelInfo

	out := captureLog(func() {
		LogDecision(DecisionLog{UserEmail: "a@b.com"})
	})
	if !strings.Contains(out, "timestamp") {
		t.Errorf("timestamp field missing: %q", out)
	}
}

func TestStartLogRotator_CancelExits(t *testing.T) {
	t.Setenv("LOG_DIR", t.TempDir())
	l := InitLogging()
	ctx, cancel := context.WithCancel(context.Background())
	StartLogRotator(ctx, l)
	cancel() // should cause goroutine to exit cleanly — no deadlock
}
