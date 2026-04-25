package ai

import (
	"message-consolidator/logger"
	"message-consolidator/types"
	"time"
)

// SourceAnalyzer defines how to extract tasks from different message sources.
// 메서드 4개 사유: 단일 strategy 단위(채널별 프롬프트/모델/전처리)로 모든 구현체가 4개를 동시에 제공해야 하므로 분리 시 응집성 손상.
type SourceAnalyzer interface {
	GetSystemInstruction(data ExtractionContext) string
	GetUserPrompt(data ExtractionContext) string
	GetModelName(defaultModel string) string
	PreProcess(text string) string
}

// GmailAnalyzer handles task extraction from email threads.
type GmailAnalyzer struct{}

func (g *GmailAnalyzer) GetSystemInstruction(data ExtractionContext) string {
	res, _ := LoadPrompt("gmail_system.prompt").Render(data)
	return res
}

func (g *GmailAnalyzer) GetUserPrompt(data ExtractionContext) string {
	res, _ := LoadPrompt("gmail_user.prompt").Render(data)
	return res
}

func (g *GmailAnalyzer) GetModelName(defaultModel string) string {
	if p := LoadPrompt("gmail_system.prompt"); p.Meta.Model != "" {
		return p.Meta.Model
	}
	return defaultModel
}

func (g *GmailAnalyzer) PreProcess(text string) string {
	const maxChars = 15000 //Why: Limits Gmail input to 15,000 characters to stay within reasonable token limits while preserving sufficient thread context.
	if len(text) > maxChars {
		return text[:maxChars]
	}
	return text
}

// ChatAnalyzer handles task extraction from Slack/WhatsApp chats.
type ChatAnalyzer struct {
	Source string
	Window time.Duration
}

func (c *ChatAnalyzer) GetSystemInstruction(data ExtractionContext) string {
	// [Dynamic Few-Shots] RAG-like selection based on input message payload.
	allShots := GetDefaultFewShots()
	data.FewShots = SelectFewShots(data.MessagePayload, allShots, 2)
	
	res, _ := LoadPrompt("chat_system.prompt").Render(data)
	return res
}

func (c *ChatAnalyzer) GetUserPrompt(data ExtractionContext) string {
	res, _ := LoadPrompt("chat_user.prompt").Render(data)
	return res
}

func (c *ChatAnalyzer) GetModelName(defaultModel string) string {
	if p := LoadPrompt("chat_system.prompt"); p.Meta.Model != "" {
		return p.Meta.Model
	}
	return defaultModel
}

func (c *ChatAnalyzer) PreProcess(text string) string {
	const maxChars = 30000 //Why: Truncates chat history to the last 30,000 characters to ensure the most recent context is sent to Gemini without exceeding token limits.
	if len(text) > maxChars {
		logger.Warnf("[GEMINI] Chat text too long (%d chars), truncating to last %d", len(text), maxChars)
		return text[len(text)-maxChars:]
	}
	return text
}

// NotionAnalyzer handles task extraction from Notion pages and comments.
type NotionAnalyzer struct{}

func (n *NotionAnalyzer) GetSystemInstruction(data ExtractionContext) string {
	res, _ := LoadPrompt("notion_system.prompt").Render(data)
	return res
}

func (n *NotionAnalyzer) GetUserPrompt(data ExtractionContext) string {
	res, _ := LoadPrompt("notion_user.prompt").Render(data)
	return res
}

func (n *NotionAnalyzer) GetModelName(defaultModel string) string {
	if p := LoadPrompt("notion_system.prompt"); p.Meta.Model != "" {
		return p.Meta.Model
	}
	return defaultModel
}

func (n *NotionAnalyzer) PreProcess(text string) string {
	//Why: [TODO] Add logic to remove markdown or filter specific Notion blocks to refine task extraction context.
	return text
}

func getAnalyzer(source string) SourceAnalyzer {
	switch source {
	case "gmail":
		return &GmailAnalyzer{}
	case "slack", "whatsapp", "telegram": //Why: Reuses the standard ChatAnalyzer for Telegram as the message structure and extraction logic are functionally identical to Slack/WhatsApp.
		return &ChatAnalyzer{Source: source}
	case "notion":
		return &NotionAnalyzer{} //Why: Routes Notion-specific extraction requests to the dedicated analyzer to handle its unique document and comment structures.
	default:
		return nil
	}
}

// GroupMessagesByTime slices a list of messages into batches based on sender and time proximity.
// Why: [Time-Topic Hybrid] Bundles rapid-fire messages from the same sender to provide better context to AI.
func GroupMessagesByTime(msgs []types.RawMessage, interval time.Duration) [][]types.RawMessage {
	if len(msgs) == 0 {
		return nil
	}
	var groups [][]types.RawMessage
	var current []types.RawMessage

	for i, msg := range msgs {
		if i == 0 || (msg.Sender == msgs[i-1].Sender && msg.Timestamp.Sub(msgs[i-1].Timestamp) <= interval) {
			current = append(current, msg)
			continue
		}
		groups = append(groups, current)
		current = []types.RawMessage{msg}
	}
	return append(groups, current)
}
