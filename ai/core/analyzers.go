package core

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
	SystemPrompt() PromptName // system prompt carrying this source's per-provider model/thinking frontmatter
	PreProcess(text string) string
}

// GmailAnalyzer handles task extraction from email threads.
type GmailAnalyzer struct{}

func (g *GmailAnalyzer) GetSystemInstruction(data ExtractionContext) string {
	res, _ := LoadPrompt(PromptGmailSystem).Render(data)
	return res
}

func (g *GmailAnalyzer) GetUserPrompt(data ExtractionContext) string {
	// Why: gmail seeds only, never chat seeds -- GetDefaultFewShots() is chat-shaped
	// (short IM-style exchanges) and would mislead email extraction. The gmail seed
	// pool exists because an empty pool let the first cold-start emails define the
	// share-vs-request boundary (Korean FYI mails extracted as personal tasks).
	allShots := append(GetDefaultGmailFewShots(), data.LearnedShots...)
	data.FewShots = SelectFewShotsForSource(data.MessagePayload, data.Source, allShots, 3)
	res, _ := LoadPrompt(PromptGmailUser).Render(data)
	return res
}

func (g *GmailAnalyzer) SystemPrompt() PromptName { return PromptGmailSystem }

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
	res, _ := LoadPrompt(PromptChatSystem).Render(data)
	return res
}

func (c *ChatAnalyzer) GetUserPrompt(data ExtractionContext) string {
	// Why: dynamic few-shots are selected per message, so rendering them in the user
	// prompt (not the system prompt) keeps chat_system a byte-stable prefix for
	// DeepSeek/Gemini prompt caching — input cache hits cost 1/50 of misses.
	allShots := append(GetDefaultFewShots(), data.LearnedShots...)
	// Why: bump 2->3 only when a learned shot is present, so a user-specific example
	// can surface alongside both seed examples instead of evicting one.
	limit := 2
	if len(data.LearnedShots) > 0 {
		limit = 3
	}
	data.FewShots = SelectFewShotsForSource(data.MessagePayload, data.Source, allShots, limit)

	res, _ := LoadPrompt(PromptChatUser).Render(data)
	return res
}

func (c *ChatAnalyzer) SystemPrompt() PromptName { return PromptChatSystem }

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
	res, _ := LoadPrompt(PromptNotionSystem).Render(data)
	return res
}

func (n *NotionAnalyzer) GetUserPrompt(data ExtractionContext) string {
	res, _ := LoadPrompt(PromptNotionUser).Render(data)
	return res
}

func (n *NotionAnalyzer) SystemPrompt() PromptName { return PromptNotionSystem }

func (n *NotionAnalyzer) PreProcess(text string) string {
	//Why: [TODO] Add logic to remove markdown or filter specific Notion blocks to refine task extraction context.
	return text
}

func GetAnalyzer(source string) SourceAnalyzer {
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

// maxGroupMessages caps a single extraction payload. Why: the gap rule alone lets one
// continuously busy room become an unbounded group; 29 messages is a conversation-sized
// chunk and leaves the 30k-char truncation as a backstop rather than the primary limit.
const maxGroupMessages = 29

// GroupMessagesByTime slices messages into batches by time proximity alone.
//
// It deliberately does NOT break on a change of speaker. Why: it used to, and that made
// the extractor blind to its own input format. A question and the answer that follows it
// seconds later landed in different passes, so the answer could never resolve the
// question -- measured on production WhatsApp, 1250 of 3042 consecutive message pairs
// inside the batch window (41%) were split by the speaker condition alone, and a real
// 2m17s exchange ("manager U/I up and running?" ... "8080") became five payloads and two
// orphan tasks the user then cancelled. The rest of the pipeline is already built for
// multi-speaker groups: buildWAPayload writes a sender per line, processChannelItems
// resolves each task's SenderRaw through msgMap[item.SourceTS], and the chat prompt's
// own few-shots (p7, p10-p13) are multi-speaker exchanges whose negotiated-outcome
// resolve rule was unreachable while this function split them apart.
//
// A burst from one speaker still coalesces, since consecutive messages fall inside the
// same window either way.
// Why: [Time-Topic Hybrid] Bundles rapid-fire messages from the same sender to provide better context to AI.
func GroupMessagesByTime(msgs []types.RawMessage, interval time.Duration) [][]types.RawMessage {
	if len(msgs) == 0 {
		return nil
	}
	var groups [][]types.RawMessage
	var current []types.RawMessage

	for i, msg := range msgs {
		if i == 0 {
			current = append(current, msg)
			continue
		}
		if msg.Timestamp.Sub(msgs[i-1].Timestamp) <= interval && len(current) < maxGroupMessages {
			current = append(current, msg)
			continue
		}
		groups = append(groups, current)
		current = []types.RawMessage{msg}
	}
	return append(groups, current)
}
