package core

import (
	"embed"
	"message-consolidator/logger"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

//go:embed prompts/*.prompt
var promptFS embed.FS

// Why: Prod 모드(embed.FS) 로딩은 prompt당 1회로 충분 — 매 호출 frontmatter 파싱 +
// template 재컴파일 비용을 누적 부담하지 않게 한다. Dev 모드(filesystem hot-reload)
// 경로는 캐시를 우회해 prompt 수정이 즉시 반영되도록 유지.
var promptCache sync.Map // map[PromptName]*ParsedPrompt

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
	PromptLiteFilterUser       PromptName = "lite_filter_user.prompt"
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

	//Why: [Dev Mode] Attempts to read local prompt files directly from the filesystem to ensure immediate reflection of changes during development and regression testing.
	if content := tryLoadFromDisk(filename); content != "" {
		return parsePromptOrFallback(filename, content)
	}

	// Why: prod 경로는 embed.FS 결과가 빌드 동안 불변 — 첫 파싱 결과를 캐시.
	if v, ok := promptCache.Load(name); ok {
		return v.(*ParsedPrompt)
	}

	//Why: [Prod Mode] Falls back to the embedded prompt filesystem (embed.FS) if local files are inaccessible, ensuring the binary is self-contained for production.
	b, err := promptFS.ReadFile("prompts/" + filename)
	if err != nil {
		logger.Errorf("[GEMINI] Failed to load prompt file %s from embed FS: %v", filename, err)
		return &ParsedPrompt{} // Why: 실패는 캐시 안 함 — 일시 오류가 영구화되는 것 방지.
	}

	parsed := parsePromptOrFallback(filename, string(b))
	promptCache.Store(name, parsed)
	return parsed
}

func tryLoadFromDisk(filename string) string {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	localPath := filepath.Join(filepath.Dir(currentFile), "prompts", filename)
	b, err := os.ReadFile(localPath)
	if err != nil {
		return ""
	}
	return string(b)
}

func parsePromptOrFallback(filename, content string) *ParsedPrompt {
	parsed, err := ParsePrompt(content)
	if err != nil {
		logger.Warnf("[GEMINI] Failed to parse prompt frontmatter for %s: %v. Using as raw body.", filename, err)
		return &ParsedPrompt{Body: content}
	}
	return parsed
}
