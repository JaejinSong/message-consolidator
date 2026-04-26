package config

import (
	"testing"
)

func TestFindDefAndRegistryShape(t *testing.T) {
	if def := FindDef("LOG_LEVEL"); def == nil || def.RestartRequired {
		t.Fatalf("LOG_LEVEL should exist and be runtime-reloadable, got %+v", def)
	}
	if def := FindDef("GEMINI_API_KEY"); def == nil || !def.Secret || !def.RestartRequired {
		t.Fatalf("GEMINI_API_KEY should be secret + restart-required, got %+v", def)
	}
	if def := FindDef("UNKNOWN_KEY_xyz"); def != nil {
		t.Fatalf("registry should reject unknown keys, got %+v", def)
	}
}

func TestValidateSetting(t *testing.T) {
	logDef := FindDef("LOG_LEVEL")
	if logDef == nil {
		t.Fatalf("LOG_LEVEL missing from registry")
	}
	cases := []struct {
		name    string
		def     *SettingDef
		input   string
		wantErr bool
	}{
		{"empty allowed", logDef, "", false},
		{"valid enum", logDef, "DEBUG", false},
		{"invalid enum", logDef, "TRACE", true},
		{"int valid", FindDef("ARCHIVE_DAYS"), "14", false},
		{"int invalid", FindDef("ARCHIVE_DAYS"), "not-a-number", true},
		{"duration valid", FindDef("MESSAGE_BATCH_WINDOW"), "10m", false},
		{"duration invalid", FindDef("MESSAGE_BATCH_WINDOW"), "abc", true},
		{"bool valid", FindDef("AUTH_DISABLED"), "true", false},
		{"bool invalid", FindDef("AUTH_DISABLED"), "maybe", true},
		{"nil def", nil, "x", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSetting(tc.def, tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateSetting(%q) err=%v, wantErr=%v", tc.input, err, tc.wantErr)
			}
		})
	}
}

func TestIsRuntimeReloadable(t *testing.T) {
	if IsRuntimeReloadable("UNKNOWN_KEY_xyz") {
		t.Errorf("unknown key must not be reloadable")
	}
	if !IsRuntimeReloadable("LOG_LEVEL") {
		t.Errorf("LOG_LEVEL should be runtime-reloadable")
	}
	if IsRuntimeReloadable("TURSO_DATABASE_URL") {
		t.Errorf("TURSO_DATABASE_URL should require restart")
	}
}
