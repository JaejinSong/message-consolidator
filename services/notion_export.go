package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"message-consolidator/internal/whataphttpx"
	"net/http"
	"strings"
)

const (
	notionAPIBase    = "https://api.notion.com/v1"
	notionAPIVersion = "2022-06-28"
	notionBlockLimit = 100
	notionTextLimit  = 2000
)

// NotionExporter creates report pages in Notion.
type NotionExporter struct {
	token        string
	parentPageID string
	client       *http.Client
}

func NewNotionExporter(token, parentPageID string) *NotionExporter {
	return &NotionExporter{
		token:        token,
		parentPageID: parentPageID,
		client:       whataphttpx.Client(),
	}
}

func (n *NotionExporter) Enabled() bool {
	return n.token != "" && n.parentPageID != ""
}

// ExportReport creates a Notion child page from the report summary and returns its URL.
func (n *NotionExporter) ExportReport(ctx context.Context, title, content string) (string, error) {
	blocks := markdownToNotionBlocks(content)
	return n.createPageWithBlocks(ctx, title, blocks)
}

func (n *NotionExporter) createPageWithBlocks(ctx context.Context, title string, blocks []map[string]any) (string, error) {
	first := blocks
	rest := []map[string]any{}
	if len(blocks) > notionBlockLimit {
		first = blocks[:notionBlockLimit]
		rest = blocks[notionBlockLimit:]
	}

	pageID, err := n.createPage(ctx, title, first)
	if err != nil {
		return "", err
	}

	for i := 0; i < len(rest); i += notionBlockLimit {
		end := min(i+notionBlockLimit, len(rest))
		if err := n.appendBlocks(ctx, pageID, rest[i:end]); err != nil {
			return "", err
		}
	}

	return fmt.Sprintf("https://www.notion.so/%s", strings.ReplaceAll(pageID, "-", "")), nil
}

func (n *NotionExporter) createPage(ctx context.Context, title string, blocks []map[string]any) (string, error) {
	body := map[string]any{
		"parent": map[string]any{"page_id": n.parentPageID},
		"properties": map[string]any{
			"title": []map[string]any{
				{"text": map[string]any{"content": title}},
			},
		},
		"children": blocks,
	}

	resp, err := n.call(ctx, http.MethodPost, "/pages", body)
	if err != nil {
		return "", err
	}

	id, _ := resp["id"].(string)
	if id == "" {
		return "", fmt.Errorf("notion: page created but no ID returned")
	}
	return id, nil
}

func (n *NotionExporter) appendBlocks(ctx context.Context, pageID string, blocks []map[string]any) error {
	body := map[string]any{"children": blocks}
	_, err := n.call(ctx, http.MethodPatch, "/blocks/"+pageID+"/children", body)
	return err
}

func (n *NotionExporter) call(ctx context.Context, method, path string, body any) (map[string]any, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, notionAPIBase+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+n.token)
	req.Header.Set("Notion-Version", notionAPIVersion)
	req.Header.Set("Content-Type", "application/json")

	res, err := n.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("notion API error %d: %s", res.StatusCode, string(raw))
	}

	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("notion: decode response: %w", err)
	}
	return result, nil
}

// markdownToNotionBlocks parses markdown with state machine to handle multi-line code blocks.
func markdownToNotionBlocks(md string) []map[string]any {
	var blocks []map[string]any
	lines := strings.Split(md, "\n")
	inCode := false
	codeLang := ""
	var codeLines []string

	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			if !inCode {
				inCode = true
				codeLang = strings.TrimPrefix(line, "```")
				codeLines = nil
			} else {
				// End of code block — convert to table if JSON array, else code block.
				b := codeBlockToNotion(codeLang, codeLines)
				blocks = append(blocks, b...)
				inCode = false
				codeLang = ""
				codeLines = nil
			}
			continue
		}

		if inCode {
			codeLines = append(codeLines, line)
			continue
		}

		if b := lineToBlock(line); b != nil {
			blocks = append(blocks, b)
		}
	}

	// Unclosed code block fallback.
	if inCode && len(codeLines) > 0 {
		blocks = append(blocks, codeBlockToNotion(codeLang, codeLines)...)
	}

	return blocks
}

// codeBlockToNotion converts a code block to Notion blocks.
// JSON arrays with uniform keys → table; everything else → code block.
func codeBlockToNotion(lang string, lines []string) []map[string]any {
	raw := strings.Join(lines, "\n")

	if strings.TrimSpace(lang) == "json" {
		if table := jsonArrayToTable(raw); table != nil {
			return []map[string]any{table}
		}
	}

	return []map[string]any{{
		"object": "block",
		"type":   "code",
		"code": map[string]any{
			"language":  notionLang(lang),
			"rich_text": richText(raw),
		},
	}}
}

// jsonArrayToTable converts a JSON array of flat objects into a Notion table block.
func jsonArrayToTable(raw string) map[string]any {
	var rows []map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &rows); err != nil || len(rows) == 0 {
		return nil
	}

	// Collect ordered keys from first row.
	keys := make([]string, 0)
	for k := range rows[0] {
		keys = append(keys, k)
	}

	tableRows := []map[string]any{tableRow(keys)} // header row
	for _, row := range rows {
		cells := make([]string, len(keys))
		for i, k := range keys {
			cells[i] = fmt.Sprintf("%v", row[k])
		}
		tableRows = append(tableRows, tableRow(cells))
	}

	return map[string]any{
		"object": "block",
		"type":   "table",
		"table": map[string]any{
			"table_width":       len(keys),
			"has_column_header": true,
			"has_row_header":    false,
			"children":          tableRows,
		},
	}
}

func tableRow(cells []string) map[string]any {
	notionCells := make([][]map[string]any, len(cells))
	for i, c := range cells {
		notionCells[i] = richText(c)
	}
	return map[string]any{
		"object": "block",
		"type":   "table_row",
		"table_row": map[string]any{
			"cells": notionCells,
		},
	}
}

func notionLang(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "json":
		return "json"
	case "go":
		return "go"
	case "js", "javascript":
		return "javascript"
	case "ts", "typescript":
		return "typescript"
	default:
		return "plain text"
	}
}

func lineToBlock(line string) map[string]any {
	switch {
	case strings.HasPrefix(line, "### "):
		return headingBlock(3, line[4:])
	case strings.HasPrefix(line, "## "):
		return headingBlock(2, line[3:])
	case strings.HasPrefix(line, "# "):
		return headingBlock(1, line[2:])
	case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* "):
		return bulletBlock(line[2:])
	case strings.TrimSpace(line) == "":
		return nil
	default:
		return paragraphBlock(line)
	}
}

func headingBlock(level int, text string) map[string]any {
	key := fmt.Sprintf("heading_%d", level)
	return map[string]any{
		"object": "block",
		"type":   key,
		key:      map[string]any{"rich_text": richText(text)},
	}
}

func bulletBlock(text string) map[string]any {
	return map[string]any{
		"object":             "block",
		"type":               "bulleted_list_item",
		"bulleted_list_item": map[string]any{"rich_text": richText(text)},
	}
}

func paragraphBlock(text string) map[string]any {
	return map[string]any{
		"object":    "block",
		"type":      "paragraph",
		"paragraph": map[string]any{"rich_text": richText(text)},
	}
}

// richText splits long text into ≤2000-char segments (Notion limit).
func richText(text string) []map[string]any {
	if text == "" {
		return []map[string]any{{"text": map[string]any{"content": ""}}}
	}
	var parts []map[string]any
	for len(text) > 0 {
		chunk := text
		if len(chunk) > notionTextLimit {
			chunk = text[:notionTextLimit]
		}
		parts = append(parts, map[string]any{"text": map[string]any{"content": chunk}})
		text = text[len(chunk):]
	}
	return parts
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
