package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

var aiInferenceLogger *log.Logger

// InitAIInferenceLogger initializes the standard AI logger pointing to a dedicated file.
// Why: 명시적으로 호출될 때만 로그 파일을 생성하도록 하여, 유닛 테스트 시 여러 디렉토리에 파편화되어 생성되는 문제를 방지합니다.
func InitAIInferenceLogger() {
	// Why: Ensures the logs directory exists before initializing the logger to avoid "no such file or directory" errors in monitoring tools.
	_ = os.MkdirAll("logs", 0755)

	// Why: Initialize standard AI logger pointing to a dedicated file for ease of analysis and isolation from general application logs.
	lumberjackLogger := &lumberjack.Logger{
		Filename:   "logs/ai_inference.log",
		MaxSize:    100, // 100MB
		MaxBackups: 30,
		MaxAge:     30, // 30 days
		Compress:   true,
		LocalTime:  true,
	}

	multiWriter := io.MultiWriter(os.Stdout, lumberjackLogger)
	aiInferenceLogger = log.New(multiWriter, "[AI-LOG] ", log.Ldate|log.Ltime|log.Lshortfile)

	// Why: Forces immediate file creation so that monitoring agents (like WhaTap) can find the file even before the first AI inference occurs.
	aiInferenceLogger.Println("AI Inference Logger initialized")
}

// LogAIInferenceToFile records AI input/output to logs/ai_inference.log for prompt quality auditing.
// Why: Enables rapid debugging of extraction issues across diverse message sources (Slack, WhatsApp, etc.).
func LogAIInferenceToFile(source, originalText, rawResponse string) {
	if aiInferenceLogger == nil {
		return
	}

	entry := fmt.Sprintf("\n--- INFERENCE [%s] ---\nSOURCE: %s\nINPUT: %s\nRAW_RESPONSE: %s\n-----------------------",
		time.Now().Format(time.RFC3339),
		source,
		originalText,
		rawResponse)

	aiInferenceLogger.Println(entry)
}
