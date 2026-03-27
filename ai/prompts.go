package ai

import (
	"embed"
	"message-consolidator/logger"
	"os"
	"path/filepath"
	"runtime"
)

//go:embed prompts/*.prompt
var promptFS embed.FS

func loadPrompt(filename string) string {
	// 1. Dev Mode: 소스 파일 위치 기준 절대 경로로 로컬 파일 읽기를 시도합니다.
	// 이를 통해 어느 디렉토리에서 실행(예: tests/regression)하더라도 로컬 수정을 즉시 반영할 수 있습니다.
	_, currentFile, _, ok := runtime.Caller(0)
	if ok {
		aiDir := filepath.Dir(currentFile)
		localPath := filepath.Join(aiDir, "prompts", filename)
		if b, err := os.ReadFile(localPath); err == nil {
			return string(b)
		}
	}

	// 2. Prod Mode: 로컬 파일이 없거나 경로를 찾을 수 없으면 빌드 시 포함된(embed) 폴백 버전을 사용합니다.
	b, err := promptFS.ReadFile("prompts/" + filename)
	if err != nil {
		logger.Errorf("[GEMINI] Failed to load prompt file %s from embed FS: %v", filename, err)
		return ""
	}
	return string(b)
}
