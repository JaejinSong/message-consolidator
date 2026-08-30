package services

import (
	"encoding/json"
	"fmt"
	"message-consolidator/logger"
)

// MetadataSet returns meta with key set to value, treating nil/empty meta as {}.
// Why: malformed existing metadata (production has such rows) must not block the write --
// start from a clean object instead of failing the whole save.
func MetadataSet(meta json.RawMessage, key string, value any) (json.RawMessage, error) {
	obj := map[string]json.RawMessage{}
	if len(meta) > 0 {
		if err := json.Unmarshal(meta, &obj); err != nil {
			logger.Warnf("[METADATA] discarding malformed metadata before set %q: %v", key, err)
			obj = map[string]json.RawMessage{}
		}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return meta, fmt.Errorf("metadata set %q: %w", key, err)
	}
	obj[key] = encoded
	out, err := json.Marshal(obj)
	if err != nil {
		return meta, fmt.Errorf("metadata set %q: %w", key, err)
	}
	return json.RawMessage(out), nil
}

// MetadataGet unmarshals meta[key] into out; returns false if meta or key absent.
func MetadataGet(meta json.RawMessage, key string, out any) (bool, error) {
	if len(meta) == 0 {
		return false, nil
	}
	obj := map[string]json.RawMessage{}
	if err := json.Unmarshal(meta, &obj); err != nil {
		return false, fmt.Errorf("metadata get %q: %w", key, err)
	}
	raw, ok := obj[key]
	if !ok {
		return false, nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return false, fmt.Errorf("metadata get %q: %w", key, err)
	}
	return true, nil
}
