package ai

import (
	"fmt"
	"message-consolidator/logger"
)

// SourceAnalyzer defines how to extract tasks from different message sources.
type SourceAnalyzer interface {
	GetSystemInstruction(language string) string
	GetUserPrompt(userEmail, conversationText, existingTasksJSON string) string
	GetModelName(defaultModel string) string
	PreProcess(text string) string
}

// GmailAnalyzer handles task extraction from email threads.
type GmailAnalyzer struct{}

func (g *GmailAnalyzer) GetSystemInstruction(language string) string {
	return fmt.Sprintf(loadPrompt("gmail_system.prompt"), language)
}

func (g *GmailAnalyzer) GetUserPrompt(userEmail, conversationText, existingTasksJSON string) string {
	return fmt.Sprintf(loadPrompt("gmail_user.prompt"), userEmail, conversationText, existingTasksJSON)
}

func (g *GmailAnalyzer) GetModelName(defaultModel string) string {
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
}

func (c *ChatAnalyzer) GetSystemInstruction(language string) string {
	return fmt.Sprintf(loadPrompt("chat_system.prompt"), language)
}

func (c *ChatAnalyzer) GetUserPrompt(userEmail, conversationText, existingTasksJSON string) string {
	return fmt.Sprintf(loadPrompt("chat_user.prompt"), userEmail, conversationText, existingTasksJSON)
}

func (c *ChatAnalyzer) GetModelName(defaultModel string) string {
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

func (n *NotionAnalyzer) GetSystemInstruction(language string) string {
	return fmt.Sprintf(loadPrompt("notion_system.prompt"), language)
}

func (n *NotionAnalyzer) GetUserPrompt(userEmail, documentText, existingTasksJSON string) string {
	return fmt.Sprintf(loadPrompt("notion_user.prompt"), userEmail, documentText, existingTasksJSON)
}

func (n *NotionAnalyzer) GetModelName(defaultModel string) string {
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
