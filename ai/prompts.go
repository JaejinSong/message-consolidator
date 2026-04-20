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

func LoadPrompt(filename string) *ParsedPrompt {
	var content string
	//Why: [Dev Mode] Attempts to read local prompt files directly from the filesystem to ensure immediate reflection of changes during development and regression testing.
	_, currentFile, _, ok := runtime.Caller(0)
	if ok {
		aiDir := filepath.Dir(currentFile)
		localPath := filepath.Join(aiDir, "prompts", filename)
		if b, err := os.ReadFile(localPath); err == nil {
			content = string(b)
		}
	}

	if content == "" {
		//Why: [Prod Mode] Falls back to the embedded prompt filesystem (embed.FS) if local files are inaccessible, ensuring the binary is self-contained for production.
		b, err := promptFS.ReadFile("prompts/" + filename)
		if err != nil {
			logger.Errorf("[GEMINI] Failed to load prompt file %s from embed FS: %v", filename, err)
			return &ParsedPrompt{} // Return empty prompt on failure
		}
		content = string(b)
	}

	// [New Logic]: Parse the prompt to extract metadata (e.g., model routing).
	parsed, err := ParsePrompt(content)
	if err != nil {
		logger.Warnf("[GEMINI] Failed to parse prompt frontmatter for %s: %v. Using as raw body.", filename, err)
		return &ParsedPrompt{Body: content} // Fallback to raw content if parsing fails
	}
	return parsed
}
