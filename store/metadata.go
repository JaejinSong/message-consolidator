package store

import (
	"encoding/json"
	"time"
)

// Metadata JSON keys stored under messages.metadata. Single home for every key
// literal — raw SQL statements concatenate these into json path expressions, so
// a key rename is a one-line change here.
const (
	metaKeyCompletionCandidate       = "completion_candidate"
	metaKeyCompletionDismissedSource = "completion_dismissed_source"
	metaKeyCompletionDismissedAt     = "completion_dismissed_at"
	metaKeyExclusionCandidate        = "exclusion_candidate"
	metaKeyExclusionDismissedAt      = "exclusion_candidate_dismissed_at"
	metaKeyExcludedAutoRestoredAt    = "excluded_auto_restored_at"
	metaKeyRemindedPrefix            = "reminded_at_"
)

// metaKeyReminded builds the reminder-stamp key for a window ("24h", "1h",
// "excluded_digest", "undated_d3", ...).
func metaKeyReminded(window string) string { return metaKeyRemindedPrefix + window }

// MessageMetadata is the typed reader/writer over the messages.metadata blob.
// Backed by map[string]json.RawMessage so unknown keys survive a
// parse → Set → Marshal round-trip byte-identical.
type MessageMetadata struct {
	m map[string]json.RawMessage
}

// ParseMetadata never fails: empty or malformed input yields an empty set.
// Why: every legacy reader treated unparseable blobs as "no keys"; keep that contract.
func ParseMetadata(raw string) MessageMetadata {
	md := MessageMetadata{m: map[string]json.RawMessage{}}
	if raw == "" {
		return md
	}
	if err := json.Unmarshal([]byte(raw), &md.m); err != nil {
		md.m = map[string]json.RawMessage{}
	}
	return md
}

// Has reports whether key is present.
func (md MessageMetadata) Has(key string) bool {
	_, ok := md.m[key]
	return ok
}

// String returns the string value at key, or "" when absent or not a JSON string.
func (md MessageMetadata) String(key string) string {
	raw, ok := md.m[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// Time parses the RFC3339 string value at key.
func (md MessageMetadata) Time(key string) (time.Time, bool) {
	s := md.String(key)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// Decode unmarshals the value at key into out, reporting success.
// out is any because callers decode into their own struct types (e.g. CompletionCandidate).
func (md MessageMetadata) Decode(key string, out any) bool {
	raw, ok := md.m[key]
	if !ok {
		return false
	}
	return json.Unmarshal(raw, out) == nil
}

// Set stores v (JSON-marshaled) at key, leaving every other key untouched.
func (md MessageMetadata) Set(key string, v any) error { // any 사유: 값 타입이 키마다 다름 — json.Marshal 위임
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	md.m[key] = json.RawMessage(b)
	return nil
}

// Marshal serializes the full metadata set back to its storage form.
func (md MessageMetadata) Marshal() (string, error) {
	b, err := json.Marshal(md.m)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
