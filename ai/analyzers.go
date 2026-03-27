package ai

import (
	"fmt"
	"message-consolidator/logger"
)

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
	// Future enhancement: Add markdown stripping or block filtering logic here to optimize token usage for Notion documents.
	return text
}

func getAnalyzer(source string) SourceAnalyzer {
	switch source {
	case "gmail":
		return &GmailAnalyzer{}
	case "slack", "whatsapp", "telegram": // Reuse the existing ChatAnalyzer for Telegram as the message structure is similar.
		return &ChatAnalyzer{Source: source}
	case "notion":
		return &NotionAnalyzer{} // Connect the dedicated Notion analyzer to handle its specific document format.
	default:
		return nil
	}
}
