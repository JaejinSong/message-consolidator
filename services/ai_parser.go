package services

import (
	"fmt"
	"regexp"
	"strings"
)

// ExtractJSONBlock finds the first JSON-like block or markdown code block, returns its content and the text with the block removed.
// Why: Enables clean separation of structured data and descriptive text for reports, handling AI backtick noise.
func ExtractJSONBlock(content string) (string, string, error) {
	jsonStr, stripped, err := extractFencedJSON(content)
	if err != nil {
		jsonStr, stripped, err = extractBraceJSON(content)
	}
	if err != nil {
		return "", content, err
	}
	processedStripped := strings.ReplaceAll(stripped, "\n\n\n", "\n\n")
	return jsonStr, strings.TrimSpace(processedStripped), nil
}

//Why: Markdown ```json``` (or bare ```) is the AI's preferred container — try that first.
func extractFencedJSON(content string) (string, string, error) {
	re := regexp.MustCompile(`(?is)` + "```" + `(?:json)?\s*(.*?)\s*` + "```")
	match := re.FindStringSubmatch(content)
	if len(match) <= 1 {
		return "", "", fmt.Errorf("no fenced block")
	}
	return strings.TrimSpace(match[1]), re.ReplaceAllString(content, ""), nil
}

//Why: Fallback when fences are missing — locate the first '{'…'}' span, optionally anchored by the [Visualization Data] header.
func extractBraceJSON(content string) (string, string, error) {
	const vizHeader = "## [Visualization Data]"
	headerIdx := strings.LastIndex(content, vizHeader)
	searchArea := content
	if headerIdx != -1 {
		searchArea = content[headerIdx+len(vizHeader):]
	}
	startIdx := strings.Index(searchArea, "{")
	endIdx := strings.LastIndex(searchArea, "}")
	if startIdx == -1 || endIdx == -1 || endIdx <= startIdx {
		return "", "", fmt.Errorf("json block not found")
	}
	jsonStr := strings.TrimSpace(searchArea[startIdx : endIdx+1])
	stripped := content[:startIdx] + content[endIdx+1:]
	if headerIdx != -1 {
		stripped = content[:headerIdx] + searchArea[:startIdx] + searchArea[endIdx+1:]
	}
	return jsonStr, stripped, nil
}

// ExtractSection extracts text from a specific section header (e.g., "## [Operations & Strategy Overview]") until the next header.
func ExtractSection(content, sectionName string) string {
	startIdx := strings.Index(content, sectionName)
	if startIdx == -1 {
		return ""
	}

	// Move cursor to end of section name
	body := content[startIdx+len(sectionName):]
	nextHeader := strings.Index(body, "\n##")
	if nextHeader != -1 {
		return strings.TrimSpace(body[:nextHeader])
	}

	return strings.TrimSpace(body)
}
