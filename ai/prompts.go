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

// PromptName pins prompt filenames at compile time so a typo or removed file is caught
// during build instead of triggering a silent fallback to an empty ParsedPrompt at runtime.
// Why: LoadPrompt previously took a free-form string; refactors that renamed/removed a
// prompt file leaked through as runtime warnings + degraded LLM output. The typed alias
// also serves as the canonical inventory — adding a new prompt requires registering here.
type PromptName string

const (
	PromptBatchTranslator      PromptName = "batch_translator.prompt"
	PromptChatSystem           PromptName = "chat_system.prompt"
	PromptChatUser             PromptName = "chat_user.prompt"
	PromptCompletionCheck      PromptName = "completion_check.prompt"
	PromptGmailSystem          PromptName = "gmail_system.prompt"
	PromptGmailUser            PromptName = "gmail_user.prompt"
	PromptIdentityGroupMerge   PromptName = "identity_group_merge.prompt"
	PromptLiteFilter           PromptName = "lite_filter.prompt"
	PromptNotionSystem         PromptName = "notion_system.prompt"
	PromptNotionUser           PromptName = "notion_user.prompt"
	PromptReleaseNotesCombined PromptName = "release_notes_combined.prompt"
	PromptReportSummary        PromptName = "report_summary.prompt"
	PromptReportTranslator     PromptName = "report_translator.prompt"
	PromptTaskMergeSummary     PromptName = "task_merge_summary.prompt"
	PromptTaskTranslator       PromptName = "task_translator.prompt"
	PromptTranslationSystem    PromptName = "translation_system.prompt"
)

func LoadPrompt(name PromptName) *ParsedPrompt {
	filename := string(name)
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
