package channels

import (
	"context"
	"testing"

	"go.mau.fi/whatsmeow"
	waTypes "go.mau.fi/whatsmeow/types"
)

// TestWAAddressBookNamePreference pins the source order for a 1:1 chat name. Why FullName and
// BusinessName win: they come from the account holder's own address book, while PushName is
// whatever the counterparty set for themselves and is frequently a nickname or empty.
func TestWAAddressBookNamePreference(t *testing.T) {
	cases := []struct {
		name  string
		infos []waTypes.ContactInfo
		book  string
		push  string
	}{
		{
			name:  "no contacts",
			infos: nil,
		},
		{
			name:  "full name wins over push name on the same entry",
			infos: []waTypes.ContactInfo{{Found: true, FullName: "Hady Tandibali", PushName: "hady"}},
			book:  "Hady Tandibali",
			push:  "hady",
		},
		{
			name:  "business name when no full name",
			infos: []waTypes.ContactInfo{{Found: true, BusinessName: "Adira Finance", PushName: "adira-cs"}},
			book:  "Adira Finance",
			push:  "adira-cs",
		},
		{
			// An @lid chat is looked up under both the LID and the phone number, so the
			// address-book entry can arrive in either position.
			name: "address book entry found under the second candidate",
			infos: []waTypes.ContactInfo{
				{Found: true, PushName: "yoga"},
				{Found: true, FullName: "Yoga Wiranda"},
			},
			book: "Yoga Wiranda",
			push: "yoga",
		},
		{
			name:  "push name only",
			infos: []waTypes.ContactInfo{{Found: true, PushName: "Khairuz"}},
			push:  "Khairuz",
		},
		{
			name:  "redacted phone is not a name",
			infos: []waTypes.ContactInfo{{Found: true, RedactedPhone: "+60∙∙∙∙∙∙∙∙12"}},
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if got := waAddressBookName(c.infos); got != c.book {
				t.Errorf("waAddressBookName = %q, want %q", got, c.book)
			}
			if got := waPushName(c.infos); got != c.push {
				t.Errorf("waPushName = %q, want %q", got, c.push)
			}
		})
	}
}

// TestPhoneForWAJIDWithoutStore covers the degraded path: an @lid JID cannot be mapped without
// a LID store, and every other server is returned untouched rather than being probed.
func TestPhoneForWAJIDWithoutStore(t *testing.T) {
	ctx := context.Background()
	client := &whatsmeow.Client{}

	cases := []string{
		"279516505182402@lid",
		"60122362207@s.whatsapp.net",
		"120363000000000000@g.us",
	}
	for _, raw := range cases {
		jid, err := waTypes.ParseJID(raw)
		if err != nil {
			t.Fatalf("ParseJID(%q): %v", raw, err)
		}
		if got := phoneForWAJID(ctx, client, jid); got != jid {
			t.Errorf("phoneForWAJID(%q) = %q, want the input unchanged", raw, got)
		}
	}

	if infos := waContactInfos(ctx, client, waTypes.EmptyJID, waTypes.EmptyJID); infos != nil {
		t.Errorf("waContactInfos with no contact store = %v, want nil", infos)
	}
}

// TestGetGroupNameFallsBackToJIDUser pins the fallback for an unregistered account: the JID's
// user part, which is what every WhatsApp chat resolved to before contact naming existed.
func TestGetGroupNameFallsBackToJIDUser(t *testing.T) {
	m := NewWAManager()
	cases := []struct{ jid, want string }{
		{"279516505182402@lid", "279516505182402"},
		{"60122362207@s.whatsapp.net", "60122362207"},
		{"120363000000000000@g.us", "120363000000000000"},
		{"not-a-jid", "not-a-jid"},
	}
	for _, c := range cases {
		if got := m.GetGroupName("unregistered@example.com", c.jid); got != c.want {
			t.Errorf("GetGroupName(%q) = %q, want %q", c.jid, got, c.want)
		}
	}
}
