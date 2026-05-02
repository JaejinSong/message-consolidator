package types

import (
	"net/mail"
	"reflect"
	"testing"
)

func TestExtractContacts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		header string
		want   []mail.Address
	}{
		{name: "Empty Header", header: "", want: nil},
		{
			name:   "Single Email Without Name",
			header: "alice@example.com",
			want:   []mail.Address{{Address: "alice@example.com"}},
		},
		{
			name:   "Quoted Name With Comma",
			header: `"Doe, Jane" <jane@example.com>, bob@example.com`,
			want: []mail.Address{
				{Name: "Doe, Jane", Address: "jane@example.com"},
				{Address: "bob@example.com"},
			},
		},
		{
			name:   "Plain Name Email Pair",
			header: "Alice <alice@example.com>, Bob <bob@example.com>",
			want: []mail.Address{
				{Name: "Alice", Address: "alice@example.com"},
				{Name: "Bob", Address: "bob@example.com"},
			},
		},
		{
			name:   "MIME Encoded Korean Name",
			header: "=?UTF-8?B?7ISx7J6s7KeE?= <jjsong@whatap.io>",
			want:   []mail.Address{{Name: "성재진", Address: "jjsong@whatap.io"}},
		},
		{
			name:   "Skips Empty Tokens In Fallback",
			header: "Alice <alice@example.com>,,, Bob <bob@example.com>",
			want: []mail.Address{
				{Name: "Alice", Address: "alice@example.com"},
				{Name: "Bob", Address: "bob@example.com"},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractContacts(tt.header)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractContacts(%q)\n  got  = %#v\n  want = %#v", tt.header, got, tt.want)
			}
		})
	}
}

func TestExtractContacts_FallbackParsesMalformedAngleBracket(t *testing.T) {
	t.Parallel()
	// Why: net/mail rejects unterminated angle brackets, so this exercises the
	// custom parseNameAndEmail fallback in the second pass.
	header := `"Carol" <carol@example.com`
	got := ExtractContacts(header)
	if len(got) != 1 || got[0].Address != "carol@example.com" {
		t.Fatalf("expected single contact carol@example.com, got %#v", got)
	}
}

func TestExtractNameFromEmail(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"Empty", "", ""},
		{"Bare Email", "alice@example.com", "alice@example.com"},
		{"Name With Email Prefers Name", "Alice <alice@example.com>", "Alice"},
		{"MIME Encoded Name", "=?UTF-8?B?7ISx7J6s7KeE?= <jjsong@whatap.io>", "성재진"},
		{
			name:   "Multi Recipient Returns First Display Name",
			header: "Alice <alice@example.com>, Bob <bob@example.com>",
			want:   "Alice",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ExtractNameFromEmail(tt.header); got != tt.want {
				t.Errorf("ExtractNameFromEmail(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}
