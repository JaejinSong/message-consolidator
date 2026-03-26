package ai

import (
	"encoding/base64"
	"encoding/json"
	"message-consolidator/logger"
	"message-consolidator/store"
	"strings"
)

func unmarshalAnalyze(cleanJSON, rawJSON string) ([]store.TodoItem, error) {
	var items []store.TodoItem
	if err := json.Unmarshal([]byte(cleanJSON), &items); err != nil {
		// Fallback: Check if it's wrapped in an object {"tasks": [...]}, {"items": [...]}, or similar
		var obj map[string]json.RawMessage
		if err2 := json.Unmarshal([]byte(cleanJSON), &obj); err2 == nil {
			for _, val := range obj {
				if err3 := json.Unmarshal(val, &items); err3 == nil && len(items) > 0 {
					break
				}
			}
		}
		if len(items) == 0 {
			logger.Errorf("[GEMINI] JSON unmarshal failed: %v, RAW: %s", err, rawJSON)
			return nil, err
		}
	}
	logger.Infof("[GEMINI] Successfully extracted %d tasks", len(items))
	return items, nil
}

func unmarshalTranslate(cleanJSON, rawJSON, language string) ([]store.TranslateRequest, error) {
	var translations []store.TranslateRequest
	if err := json.Unmarshal([]byte(cleanJSON), &translations); err != nil {
		// Fallback: Check if it's wrapped in an object {"translations": [...]}
		var tr store.TranslateResponse
		if err2 := json.Unmarshal([]byte(cleanJSON), &tr); err2 == nil && len(tr.Translations) > 0 {
			translations = tr.Translations
		} else {
			if err2 != nil {
				logger.Errorf("[GEMINI] Translation JSON unmarshal fallback failed: %v", err2)
			}
			logger.Errorf("[GEMINI] Translation JSON unmarshal failed: %v, RAW: %s", err, rawJSON)
			return nil, err
		}
	}
	logger.Debugf("[GEMINI] Successfully translated %d items to %s", len(translations), language)
	return translations, nil
}

func DecodeBase64URL(data string) (string, error) {
	// 1. URL-safe 인코딩 (패딩 포함)
	decoded, err := base64.URLEncoding.DecodeString(data)
	if err == nil {
		return string(decoded), nil
	}

	// 2. URL-safe 인코딩 (패딩 생략) - 주로 웹/토큰에서 자주 발생
	decoded, err = base64.RawURLEncoding.DecodeString(data)
	if err == nil {
		return string(decoded), nil
	}

	// 3. 표준 인코딩 (패딩 포함)
	decoded, err = base64.StdEncoding.DecodeString(data)
	if err == nil {
		return string(decoded), nil
	}

	// 4. 표준 인코딩 (패딩 생략)
	decoded, err = base64.RawStdEncoding.DecodeString(data)
	if err != nil {
		return "", err
	}

	return string(decoded), nil
}

// sanitizeJSON cleans AI response from markdown code blocks and whitespace.
func sanitizeJSON(s string) string {
	s = strings.TrimSpace(s)

	// 1. 마크다운 코드 블록(```json 등) 파싱 시, 코드 블록 앞뒤에 붙은 불필요한 설명 텍스트 완벽 제거
	startIdx := strings.Index(s, "```json")
	if startIdx == -1 {
		startIdx = strings.Index(s, "```")
	}
	if startIdx != -1 {
		newlineIdx := strings.IndexByte(s[startIdx:], '\n')
		if newlineIdx != -1 {
			contentStart := startIdx + newlineIdx + 1
			endIdx := strings.Index(s[contentStart:], "```")
			if endIdx != -1 {
				s = s[contentStart : contentStart+endIdx]
			}
		}
	}

	s = strings.TrimSpace(s)

	// 2. 괄호 기반 최선 추출 (Best-effort extraction)
	start := strings.IndexAny(s, "[{")
	end := strings.LastIndexAny(s, "]}")
	if start != -1 && end != -1 && start < end {
		extracted := s[start : end+1]

		// [방어 로직] JSON 배열 잘림 복구 (Repair truncated JSON)
		if extracted[0] == '[' && extracted[len(extracted)-1] == '}' {
			extracted += "]"
		}

		return extracted
	}

	return ""
}
