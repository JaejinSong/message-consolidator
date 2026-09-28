package channels

import (
	"testing"

	waTypes "go.mau.fi/whatsmeow/types"
)

func TestIsUsablePushName(t *testing.T) {
	cases := map[string]bool{
		"":        false,
		".":       false,
		" - ":     false,
		"…":       false,
		"Hady":    true,
		"재진":      true,
		"A.":      true,
		"007":     true,
		"🙂 Wulan": true,
	}
	for in, want := range cases {
		if got := isUsablePushName(in); got != want {
			t.Errorf("isUsablePushName(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestPickContactName_SkipsPunctuationPushName(t *testing.T) {
	c := waTypes.ContactInfo{PushName: ".", BusinessName: "Secupi"}
	if got := pickContactName(c); got != "Secupi" {
		t.Errorf("pickContactName = %q, want business name fallback", got)
	}
}
