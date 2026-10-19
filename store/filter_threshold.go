package store

import (
	"context"
	"strconv"
	"strings"
)

const (
	ClefThresholdKeyPrefix = "clef_threshold:"
	FilterThresholdMin     = 0.1
	FilterThresholdMax     = 0.5
)

func ClefThresholdKey(email string) string {
	return ClefThresholdKeyPrefix + email
}

// Why: a malformed or out-of-range stored value must fall back to the default, never widen the drop zone.
func ParseFilterThreshold(raw string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || v < FilterThresholdMin || v > FilterThresholdMax {
		return 0, false
	}
	return v, true
}

func GetUserClefThreshold(ctx context.Context, email string) (float64, bool) {
	raw, ok := GetSettingRaw(ctx, ClefThresholdKey(email))
	if !ok {
		return 0, false
	}
	return ParseFilterThreshold(raw)
}
