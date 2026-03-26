package ai

import (
	"embed"
	"message-consolidator/logger"
	"os"
)

//go:embed prompts/*.prompt
var promptFS embed.FS

func loadPrompt(filename string) string {
	// 1. Dev Mode: 로컬 파일 시스템에서 직접 읽기를 시도합니다.
	// 이 덕분에 앱 재시작 없이 프롬프트 파일(.prompt) 수정이 즉시 반영됩니다.
	localPath := "ai/prompts/" + filename
	if b, err := os.ReadFile(localPath); err == nil {
		return string(b)
	}

	// 2. Prod Mode: 파일이 없으면 빌드 시 포함된(embed) 폴백 버전을 사용합니다.
	b, err := promptFS.ReadFile("prompts/" + filename)
	if err != nil {
		logger.Errorf("[GEMINI] Failed to load prompt file %s: %v", filename, err)
		return ""
	}
	return string(b)
}
