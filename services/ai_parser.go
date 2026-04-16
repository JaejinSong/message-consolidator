package services

import (
	"fmt"
	"regexp"
	"strings"
)

// ExtractJSONBlock finds the first JSON-like block or markdown code block, returns its content and the text with the block removed.
// Why: Enables clean separation of structured data and descriptive text for reports, handling AI backtick noise.
func ExtractJSONBlock(content string) (string, string, error) {
	// 1. Try to find ```json ... ``` or ``` ... ``` using regex
	re := regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)\\s*```")
	match := re.FindStringSubmatch(content)

	var jsonStr string
	var stripped string

	if len(match) > 1 {
		jsonStr = strings.TrimSpace(match[1])
		// Remove the entire block including markers from the summary text
		stripped = re.ReplaceAllString(content, "")
	} else {
		// 2. Fallback: Find the first '{' and last '}' to extract raw JSON object
		startIdx := strings.Index(content, "{")
		endIdx := strings.LastIndex(content, "}")
		if startIdx != -1 && endIdx != -1 && endIdx > startIdx {
			jsonStr = strings.TrimSpace(content[startIdx : endIdx+1])
			stripped = content[:startIdx] + content[endIdx+1:]
		} else {
			return "", content, fmt.Errorf("json block not found")
		}
	}

	// Post-process stripped text: remove triple newlines and trim
	processedStripped := strings.ReplaceAll(stripped, "\n\n\n", "\n\n")
	return jsonStr, strings.TrimSpace(processedStripped), nil
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
