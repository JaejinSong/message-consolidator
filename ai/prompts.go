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
	// 1. Dev Mode: Attempt to read the local file using an absolute path relative to this source file.
	// This ensures that local modifications are immediately reflected, regardless of the execution directory (e.g., during regression tests).
	_, currentFile, _, ok := runtime.Caller(0)
	if ok {
		aiDir := filepath.Dir(currentFile)
		localPath := filepath.Join(aiDir, "prompts", filename)
		if b, err := os.ReadFile(localPath); err == nil {
			return string(b)
		}
	}

	// 2. Prod Mode: If the local file is missing or the path cannot be resolved, fallback to the embedded version included during the build.
	b, err := promptFS.ReadFile("prompts/" + filename)
	if err != nil {
		logger.Errorf("[GEMINI] Failed to load prompt file %s from embed FS: %v", filename, err)
		return ""
	}
	return string(b)
}
