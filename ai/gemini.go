package ai

import (
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
	"os"
	"strings"
	"time"

	"github.com/google/generative-ai-go/genai"
	"github.com/whatap/go-api/trace"
	"google.golang.org/api/option"
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

type GeminiClient struct {
	client           *genai.Client
	analysisModel    string
	translationModel string
}

func NewGeminiClient(ctx context.Context, apiKey string, analysisModel, translationModel string) (*GeminiClient, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY is not set")
	}
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, err
	}
	if analysisModel == "" {
		analysisModel = "gemini-3-flash-preview"
	}
	if translationModel == "" {
		translationModel = "gemini-3.1-flash-lite-preview"
	}
	return &GeminiClient{
		client:           client,
		analysisModel:    analysisModel,
		translationModel: translationModel,
	}, nil
}

// SourceAnalyzer defines how to extract tasks from different message sources.
type SourceAnalyzer interface {
	GetSystemInstruction(language string) string
	GetUserPrompt(userEmail, conversationText string) string
	GetModelName(defaultModel string) string
	PreProcess(text string) string
}

// GmailAnalyzer handles task extraction from email threads.
type GmailAnalyzer struct{}

func (g *GmailAnalyzer) GetSystemInstruction(language string) string {
	return fmt.Sprintf(loadPrompt("gmail_system.prompt"), language)
}

func (g *GmailAnalyzer) GetUserPrompt(userEmail, conversationText string) string {
	return fmt.Sprintf(loadPrompt("gmail_user.prompt"), userEmail, conversationText)
}

func (g *GmailAnalyzer) GetModelName(defaultModel string) string {
	return defaultModel
}

func (g *GmailAnalyzer) PreProcess(text string) string {
	const maxChars = 15000 // Limit Gmail input to a reasonable size to save tokens
	if len(text) > maxChars {
		return text[:maxChars]
	}
	return text
}

// ChatAnalyzer handles task extraction from Slack/WhatsApp chats.
type ChatAnalyzer struct {
	Source string
}

func (c *ChatAnalyzer) GetSystemInstruction(language string) string {
	return fmt.Sprintf(loadPrompt("chat_system.prompt"), language)
}

func (c *ChatAnalyzer) GetUserPrompt(userEmail, conversationText string) string {
	return fmt.Sprintf(loadPrompt("chat_user.prompt"), c.Source, userEmail, conversationText)
}

func (c *ChatAnalyzer) GetModelName(defaultModel string) string {
	return defaultModel
}

func (c *ChatAnalyzer) PreProcess(text string) string {
	const maxChars = 30000 // Safely keep within reasonable token limits
	if len(text) > maxChars {
		logger.Warnf("[GEMINI] Chat text too long (%d chars), truncating to last %d", len(text), maxChars)
		return text[len(text)-maxChars:]
	}
	return text
}

// NotionAnalyzer handles task extraction from Notion pages and comments.
type NotionAnalyzer struct{}

func (n *NotionAnalyzer) GetSystemInstruction(language string) string {
	return fmt.Sprintf(loadPrompt("notion_system.prompt"), language)
}

func (n *NotionAnalyzer) GetUserPrompt(userEmail, documentText string) string {
	return fmt.Sprintf(loadPrompt("notion_user.prompt"), userEmail, documentText)
}

func (n *NotionAnalyzer) GetModelName(defaultModel string) string {
	return defaultModel
}

func (n *NotionAnalyzer) PreProcess(text string) string {
	// 향후 마크다운 제거나 특정 블록 필터링 로직을 이곳에 추가할 수 있습니다.
	return text
}

func (g *GeminiClient) getAnalyzer(source string) SourceAnalyzer {
	switch source {
	case "gmail":
		return &GmailAnalyzer{}
	case "slack", "whatsapp", "telegram": // Telegram은 기존 ChatAnalyzer를 그대로 재사용!
		return &ChatAnalyzer{Source: source}
	case "notion":
		return &NotionAnalyzer{} // 노션 전용 분석기 연결
	default:
		return nil
	}
}

func (g *GeminiClient) Analyze(ctx context.Context, email, conversationText string, language string, source string) ([]store.TodoItem, error) {
	if g == nil || g.client == nil {
		return nil, fmt.Errorf("Gemini client is not initialized")
	}

	if language == "" {
		language = "Korean"
	}

	analyzer := g.getAnalyzer(source)
	modelName := g.analysisModel
	if analyzer != nil {
		modelName = analyzer.GetModelName(g.analysisModel)
	}

	model := g.client.GenerativeModel(modelName)
	model.ResponseMIMEType = "application/json"
	// Token optimization: set explicit limits and low temperature for stability
	model.SetTemperature(0.1)
	model.SetMaxOutputTokens(1500) // Extracted tasks are usually concise JSON

	sysInst := `Extract tasks as JSON array (Return [] if no actionable task): [{"task", "requester", "assignee", "assigned_at", "source_ts", "deadline", "category"}]`
	if analyzer != nil {
		sysInst = analyzer.GetSystemInstruction(language)
	}
	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text(sysInst)},
	}

	userPrompt := conversationText
	if analyzer != nil {
		processedText := analyzer.PreProcess(conversationText)
		userPrompt = analyzer.GetUserPrompt(email, processedText)
	}

	logger.Infof("[GEMINI] Analyzing conversation (%s) in %s using model %s...", source, language, modelName)

	start := time.Now()
	resp, err := model.GenerateContent(ctx, genai.Text(userPrompt))
	elapsed := int(time.Since(start).Milliseconds())
	trace.Step(ctx, "Gemini-Analyze", "", elapsed, 0)

	if err != nil {
		logger.Errorf("[GEMINI] Analysis failed: %v", err)
		return nil, err
	}

	if resp.UsageMetadata != nil {
		pTokens := int(resp.UsageMetadata.PromptTokenCount)
		cTokens := int(resp.UsageMetadata.CandidatesTokenCount)
		store.AddTokenUsage(email, pTokens, cTokens)

		trace.Step(ctx, fmt.Sprintf("TokenUsage-Analyze (Prompt: %d, Comp: %d)", pTokens, cTokens), "", 0, 0)
	}

	var rawJSON string
	for _, part := range resp.Candidates[0].Content.Parts {
		if t, ok := part.(genai.Text); ok {
			rawJSON += string(t)
		}
	}

	cleanJSON := sanitizeJSON(rawJSON)
	if cleanJSON == "" || cleanJSON == "[]" {
		return nil, nil
	}

	var items []store.TodoItem
	if err := json.Unmarshal([]byte(cleanJSON), &items); err != nil {
		logger.Errorf("[GEMINI] JSON unmarshal failed: %v, RAW: %s", err, rawJSON)
		return nil, err
	}
	logger.Infof("[GEMINI] Successfully extracted %d tasks", len(items))
	return items, nil
}

func (g *GeminiClient) Translate(ctx context.Context, email string, tasks []store.TranslateRequest, language string) ([]store.TranslateRequest, error) {
	if g == nil || g.client == nil {
		return nil, fmt.Errorf("Gemini client is not initialized")
	}
	if len(tasks) == 0 {
		return nil, nil
	}

	model := g.client.GenerativeModel(g.translationModel)
	model.ResponseMIMEType = "application/json"
	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text(fmt.Sprintf(loadPrompt("translation_system.prompt"), language, language))},
	}

	logger.Debugf("[GEMINI] Translating %d tasks to %s...", len(tasks), language)

	start := time.Now()
	tasksJSON, _ := json.Marshal(tasks)
	resp, err := model.GenerateContent(ctx, genai.Text(string(tasksJSON)))
	elapsed := int(time.Since(start).Milliseconds())
	trace.Step(ctx, "Gemini-Translate", "", elapsed, 0)

	if err != nil {
		logger.Errorf("[GEMINI] Translation failed: %v", err)
		return nil, err
	}

	if resp.UsageMetadata != nil {
		pTokens := int(resp.UsageMetadata.PromptTokenCount)
		cTokens := int(resp.UsageMetadata.CandidatesTokenCount)
		store.AddTokenUsage(email, pTokens, cTokens)

		trace.Step(ctx, fmt.Sprintf("TokenUsage-Translate (Prompt: %d, Comp: %d)", pTokens, cTokens), "", 0, 0)
	}

	var rawJSON string
	for _, part := range resp.Candidates[0].Content.Parts {
		if text, ok := part.(genai.Text); ok {
			rawJSON += string(text)
		}
	}

	cleanJSON := sanitizeJSON(rawJSON)
	if cleanJSON == "" {
		return nil, fmt.Errorf("empty translation response")
	}

	var tr store.TranslateResponse
	if err := json.Unmarshal([]byte(cleanJSON), &tr); err != nil {
		logger.Errorf("[GEMINI] Translation JSON unmarshal failed: %v, RAW: %s", err, rawJSON)
		return nil, err
	}
	logger.Debugf("[GEMINI] Successfully translated %d items to %s", len(tr.Translations), language)
	return tr.Translations, nil
}

func (g *GeminiClient) DoesReplyCompleteTask(ctx context.Context, email, taskText, replyText string) (bool, error) {
	if g == nil || g.client == nil {
		return false, fmt.Errorf("Gemini client is not initialized")
	}

	model := g.client.GenerativeModel(g.analysisModel)
	model.SetTemperature(0.0) // Deterministic
	model.SetMaxOutputTokens(10)

	prompt := fmt.Sprintf(loadPrompt("completion_check.prompt"), taskText, replyText)

	logger.Debugf("[GEMINI] Checking completion for task: %s", taskText)

	start := time.Now()
	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
	elapsed := int(time.Since(start).Milliseconds())
	trace.Step(ctx, "Gemini-CheckCompletion", "", elapsed, 0)

	if err != nil {
		return false, err
	}

	if resp.UsageMetadata != nil {
		pTokens := int(resp.UsageMetadata.PromptTokenCount)
		cTokens := int(resp.UsageMetadata.CandidatesTokenCount)
		store.AddTokenUsage(email, pTokens, cTokens)

		trace.Step(ctx, fmt.Sprintf("TokenUsage-CheckCompletion (Prompt: %d, Comp: %d)", pTokens, cTokens), "", 0, 0)
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return false, fmt.Errorf("empty response from Gemini")
	}

	var answer string
	for _, part := range resp.Candidates[0].Content.Parts {
		if t, ok := part.(genai.Text); ok {
			answer += string(t)
		}
	}

	answer = strings.ToUpper(strings.TrimSpace(answer))
	logger.Debugf("[GEMINI] Completion check result: %s", answer)

	return strings.HasPrefix(answer, "YES"), nil
}

func (g *GeminiClient) CheckTasksBatch(ctx context.Context, email, replyText string, tasks []store.ConsolidatedMessage) ([]int, error) {
	if g == nil || g.client == nil {
		return nil, fmt.Errorf("Gemini client is not initialized")
	}
	if len(tasks) == 0 {
		return nil, nil
	}

	model := g.client.GenerativeModel(g.analysisModel)
	model.SetTemperature(0.0)
	model.SetMaxOutputTokens(200)
	model.ResponseMIMEType = "application/json"

	var taskList strings.Builder
	for _, t := range tasks {
		taskList.WriteString(fmt.Sprintf("- ID: %d, Task: %s\n", t.ID, t.Task))
	}

	prompt := fmt.Sprintf(loadPrompt("batch_completion_check.prompt"), taskList.String(), replyText)

	logger.Debugf("[GEMINI] Batch checking %d tasks for reply: %s", len(tasks), replyText)

	start := time.Now()
	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
	elapsed := int(time.Since(start).Milliseconds())
	trace.Step(ctx, "Gemini-BatchCheckCompletion", "", elapsed, 0)

	if err != nil {
		return nil, err
	}

	if resp.UsageMetadata != nil {
		pTokens := int(resp.UsageMetadata.PromptTokenCount)
		cTokens := int(resp.UsageMetadata.CandidatesTokenCount)
		store.AddTokenUsage(email, pTokens, cTokens)

		trace.Step(ctx, fmt.Sprintf("TokenUsage-BatchCheck (Prompt: %d, Comp: %d)", pTokens, cTokens), "", 0, 0)
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("empty response from Gemini")
	}

	var rawJSON string
	for _, part := range resp.Candidates[0].Content.Parts {
		if t, ok := part.(genai.Text); ok {
			rawJSON += string(t)
		}
	}

	cleanJSON := sanitizeJSON(rawJSON)
	if cleanJSON == "" || cleanJSON == "[]" {
		return nil, nil
	}

	var completedIDs []int
	if err := json.Unmarshal([]byte(cleanJSON), &completedIDs); err != nil {
		logger.Errorf("[GEMINI] Batch JSON unmarshal failed: %v, RAW: %s", err, rawJSON)
		return nil, err
	}

	return completedIDs, nil
}

func DecodeBase64URL(data string) (string, error) {
	decoded, err := base64.URLEncoding.DecodeString(data)
	if err != nil {
		decoded, err = base64.StdEncoding.DecodeString(data)
		if err != nil {
			return "", err
		}
	}
	return string(decoded), nil
}

// sanitizeJSON cleans AI response from markdown code blocks and whitespace.
func sanitizeJSON(s string) string {
	s = strings.TrimSpace(s)
	// Remove markdown code blocks if any
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimSuffix(s, "```")
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
	}
	s = strings.TrimSpace(s)

	// Basic validation: must start with { or [ and end with } or ]
	if len(s) < 2 {
		return ""
	}
	// Strictly check for balanced outer brackets. If not balanced, we proceed to best-effort extraction.
	if (s[0] == '{' && s[len(s)-1] == '}') || (s[0] == '[' && s[len(s)-1] == ']') {
		return s
	}

	// Try to find the first and last brackets for a "best-effort" extraction
	start := strings.IndexAny(s, "[{")
	end := strings.LastIndexAny(s, "]}")
	if start != -1 && end != -1 && start < end {
		extracted := s[start : end+1]

		// [방어 로직] JSON 배열 잘림 복구 (Repair truncated JSON)
		// 문자열이 '[' 로 시작했는데 마지막으로 찾은 괄호가 '}' 인 경우,
		// 데이터가 중간에 잘려서 배열 닫기(']')가 누락된 상황이므로 강제로 닫아줍니다.
		if extracted[0] == '[' && extracted[len(extracted)-1] == '}' {
			extracted += "]"
		}

		return extracted
	}

	return ""
}
