package logger

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLogAIInferenceToFile(t *testing.T) {
	// Setup: Override LOG_DIR and explicitly initialize
	os.Setenv("LOG_DIR", "tmp/logs")
	defer os.Unsetenv("LOG_DIR")
	InitAIInferenceLogger()

	// Temporary Log File Path
	logFile := "tmp/logs/ai_inference.log"
	os.MkdirAll("tmp/logs", 0755)
	
	// Check if file exists and contains content, using a unique identifier to verify recent writes
	uniqueID := fmt.Sprintf("TEST-RUN-%d", time.Now().UnixNano())
	LogAIInferenceToFile("test_source", uniqueID, "{\"output\": \"test response\"}")

	// Wait briefly for disk write and potential handle refresh
	time.Sleep(200 * time.Millisecond)

	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	strContent := string(content)
	if !strings.Contains(strContent, "SOURCE: test_source") {
		t.Errorf("Log content missing source: %s", strContent)
	}
	if !strings.Contains(strContent, uniqueID) {
		t.Errorf("Log content missing input ID: %s", strContent)
	}
	if !strings.Contains(strContent, "RAW_RESPONSE: {\"output\": \"test response\"}") {
		t.Errorf("Log content missing raw response: %s", strContent)
	}
}
