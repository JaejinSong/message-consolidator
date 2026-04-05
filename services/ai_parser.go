package services

import (
	"fmt"
	"strings"
)

// ExtractJSONBlock finds the first ```json block, returns its content and the text with the block removed.
// Why: Enables clean separation of structured data and descriptive text for reports.
func ExtractJSONBlock(content string) (string, string, error) {
	startMark := "```json"
	endMark := "```"

	startIdx := strings.Index(content, startMark)
	if startIdx == -1 {
		return "", content, fmt.Errorf("json block start not found")
	}

	// Calculate indices for content after the start marker
	afterStart := content[startIdx+len(startMark):]
	endIdx := strings.Index(afterStart, endMark)
	if endIdx == -1 {
		return "", content, fmt.Errorf("json block end not found")
	}

	jsonStr := strings.TrimSpace(afterStart[:endIdx])
	// Combine text before the block and text after the block
	rawStripped := content[:startIdx] + afterStart[endIdx+len(endMark):]
	// Replace triple newlines with double newlines for cleaner text
	stripped := strings.ReplaceAll(rawStripped, "\n\n\n", "\n\n")
	return jsonStr, strings.TrimSpace(stripped), nil
}

// ExtractSection extracts text from a specific section header (e.g., "## [Executive Summary]") until the next header.
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
