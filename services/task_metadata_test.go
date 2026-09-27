package services

import (
	"encoding/json"
	"testing"
)

// TestMetadataGetSetCharacterization pins today's MetadataGet/MetadataSet
// behavior for the inputs a future codec swap must not disturb: nil, "",
// "{}", malformed JSON, an existing key, and a nested value.
func TestMetadataGetSetCharacterization(t *testing.T) {
	t.Run("get nil meta returns false, no error", func(t *testing.T) {
		var out string
		found, err := MetadataGet(nil, "any", &out)
		if found || err != nil {
			t.Fatalf("got found=%v err=%v, want false, nil", found, err)
		}
	})

	t.Run("get empty string meta returns false, no error", func(t *testing.T) {
		var out string
		found, err := MetadataGet(json.RawMessage(""), "any", &out)
		if found || err != nil {
			t.Fatalf("got found=%v err=%v, want false, nil", found, err)
		}
	})

	t.Run("get from empty object returns false, no error", func(t *testing.T) {
		var out string
		found, err := MetadataGet(json.RawMessage("{}"), "any", &out)
		if found || err != nil {
			t.Fatalf("got found=%v err=%v, want false, nil", found, err)
		}
	})

	t.Run("get from malformed JSON returns error", func(t *testing.T) {
		var out string
		found, err := MetadataGet(json.RawMessage("{bad"), "any", &out)
		if found || err == nil {
			t.Fatalf("got found=%v err=%v, want false, non-nil error", found, err)
		}
	})

	t.Run("get existing key", func(t *testing.T) {
		var out string
		found, err := MetadataGet(json.RawMessage(`{"greeting":"hello"}`), "greeting", &out)
		if !found || err != nil || out != "hello" {
			t.Fatalf("got found=%v err=%v out=%q, want true, nil, hello", found, err, out)
		}
	})

	t.Run("get nested value", func(t *testing.T) {
		var out map[string]string
		found, err := MetadataGet(json.RawMessage(`{"field_sources":{"assignee":"manual"}}`), "field_sources", &out)
		if !found || err != nil || out["assignee"] != "manual" {
			t.Fatalf("got found=%v err=%v out=%v, want true, nil, {assignee:manual}", found, err, out)
		}
	})

	t.Run("set nil meta treated as empty object", func(t *testing.T) {
		out, err := MetadataSet(nil, "greeting", "hi")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, out, `{"greeting":"hi"}`)
	})

	t.Run("set empty string meta treated as empty object", func(t *testing.T) {
		out, err := MetadataSet(json.RawMessage(""), "greeting", "hi")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, out, `{"greeting":"hi"}`)
	})

	t.Run("set on empty object", func(t *testing.T) {
		out, err := MetadataSet(json.RawMessage("{}"), "greeting", "hi")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, out, `{"greeting":"hi"}`)
	})

	t.Run("set on malformed JSON discards and starts clean, no error", func(t *testing.T) {
		out, err := MetadataSet(json.RawMessage("{bad"), "greeting", "hi")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, out, `{"greeting":"hi"}`)
	})

	t.Run("set overwrites existing key, preserves others", func(t *testing.T) {
		out, err := MetadataSet(json.RawMessage(`{"greeting":"hello","other":1}`), "greeting", "hi")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, out, `{"greeting":"hi","other":1}`)
	})

	t.Run("set nested value", func(t *testing.T) {
		out, err := MetadataSet(json.RawMessage("{}"), "field_sources", map[string]string{"assignee": "manual"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, out, `{"field_sources":{"assignee":"manual"}}`)
	})
}

// assertJSONEqual compares two JSON blobs by decoded value, ignoring key order.
func assertJSONEqual(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	var gotVal, wantVal any
	if err := json.Unmarshal(got, &gotVal); err != nil {
		t.Fatalf("got is not valid JSON: %v (%s)", err, got)
	}
	if err := json.Unmarshal([]byte(want), &wantVal); err != nil {
		t.Fatalf("want is not valid JSON: %v", err)
	}
	gotJSON, _ := json.Marshal(gotVal)
	wantJSON, _ := json.Marshal(wantVal)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("got %s, want %s", got, want)
	}
}
