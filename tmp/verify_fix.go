package main

import (
	"fmt"
	"strings"
	"regexp"
)

// Mocking the behavior for standalone test
func mockGetNameByWhatsAppNumber(email, number string) string {
	if number == "58102435057696" {
		return "Andy Phan"
	}
	if number == "1949056666256901" {
		return "박요셉"
	}
	return ""
}

func resolveWAMentions(email, text string) string {
	re := regexp.MustCompile(`@([0-9]+)`)
	return re.ReplaceAllStringFunc(text, func(m string) string {
		number := m[1:]
		name := mockGetNameByWhatsAppNumber(email, number)
		if name != "" {
			return fmt.Sprintf("@%s", name)
		}
		return m
	})
}

func main() {
	text := "@58102435057696 @1949056666256901 we hv another Cambodia bank... "
	resolved := resolveWAMentions("test@example.com", text)
	fmt.Printf("Original: %s\n", text)
	fmt.Printf("Resolved: %s\n", resolved)

	if strings.Contains(resolved, "@Andy Phan") && strings.Contains(resolved, "@박요셉") {
		fmt.Println("SUCCESS: Mentions resolved correctly.")
	} else {
		fmt.Println("FAILURE: Mentions NOT resolved correctly.")
	}
}
