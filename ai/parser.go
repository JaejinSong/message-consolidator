package ai

import (
	"errors"
	"strings"
)

// ErrInvalidFrontmatter는 프롬프트 메타데이터 형식이 잘못되었을 때 반환됩니다.
var ErrInvalidFrontmatter = errors.New("invalid prompt format: must start with ---")

// PromptMeta는 프롬프트의 버전 및 모델 라우팅 정보를 담습니다.
type PromptMeta struct {
	Name    string
	Version string
	Model   string
}

// ParsedPrompt는 런타임에 템플릿 엔진에 전달될 최종 객체입니다.
type ParsedPrompt struct {
	Meta PromptMeta
	Body string // 토큰 소모 대상 (Gemini API로 전송될 순수 텍스트)
}

// ParsePrompt는 30라인 이내, 2 depth 제약을 준수하여 구현되었습니다.
func ParsePrompt(content string) (*ParsedPrompt, error) {
	// Guard Clause 1: 공백 제거 및 최상단 구분자 강제 검증
	cleanContent := strings.TrimSpace(content)
	if !strings.HasPrefix(cleanContent, "---") {
		return nil, ErrInvalidFrontmatter
	}

	// Guard Clause 2: SplitN 적용 및 가비지 데이터 배제
	parts := strings.SplitN(cleanContent, "---", 3)
	if len(parts) < 3 || strings.TrimSpace(parts[0]) != "" {
		return nil, ErrInvalidFrontmatter
	}

	// 메타데이터 파싱 및 바디 할당
	meta := parseMetadata(parts[1])
	return &ParsedPrompt{Meta: meta, Body: strings.TrimSpace(parts[2])}, nil
}

// parseMetadata는 메타데이터 문자열을 PromptMeta 구조체로 파싱합니다.
func parseMetadata(raw string) PromptMeta {
	meta := PromptMeta{}
	for _, line := range strings.Split(raw, "\n") {
		kv := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(kv) < 2 {
			continue
		}

		assignField(strings.ToLower(strings.TrimSpace(kv[0])), strings.TrimSpace(kv[1]), &meta)
	}
	return meta
}

// assignField는 30라인 제약을 위해 분리된 헬퍼 함수로, 필드 값을 할당합니다.
func assignField(key, val string, meta *PromptMeta) {
	if key == "name" {
		meta.Name = val
	}
	if key == "version" {
		meta.Version = val
	}
	if key == "model" {
		meta.Model = val
	}
}
