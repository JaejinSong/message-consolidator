//go:build clef_live

// Live check against the real Clef decision endpoint. Excluded from the default build by
// the clef_live tag and skipped without a key:
//
//	CLEF_API_KEY=... go test -tags=clef_live ./ai/ -run TestLive_Clef -v
package ai

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLive_Clef_IsNoise(t *testing.T) {
	key := strings.TrimSpace(os.Getenv("CLEF_API_KEY"))
	if key == "" {
		key = strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY"))
	}
	if key == "" {
		t.Skip("CLEF_API_KEY / DEEPSEEK_API_KEY not set")
	}
	f, err := NewClefFilter(key, "https://ollama.com/v1", "clef:cloud", 0.3)
	if err != nil {
		t.Fatalf("NewClefFilter: %v", err)
	}

	cases := []struct {
		name      string
		text      string
		wantNoise bool
	}{
		{"korean request", "내일 오전까지 견적서 수정본 보내주실 수 있을까요?", false},
		{"english request", "Can you review the PoC license draft and send comments by Friday?", false},
		{"otp", "[Web발신] 인증번호 [482913]를 입력해주세요. 타인에게 절대 알려주지 마세요.", true},
		{"marketing", "🎉 가을 맞이 특가! 지금 가입하면 3개월 무료. 쿠폰 코드: FALL2026. 수신거부는 여기를 클릭하세요.", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			got, err := f.IsNoise(ctx, "live-test@example.com", "slack", tc.text)
			if err != nil {
				t.Fatalf("IsNoise: %v", err)
			}
			if got != tc.wantNoise {
				t.Errorf("IsNoise = %v, want %v", got, tc.wantNoise)
			}
		})
	}
}
