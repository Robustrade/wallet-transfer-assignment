package config

import (
	"strings"
	"testing"
)

// TestLoadRunMigrations pins the RUN_MIGRATIONS parsing: the value must be a
// real boolean (strconv.ParseBool), a misspelling is an error rather than a
// silent "off", and unset or empty means false.
func TestLoadRunMigrations(t *testing.T) {
	for _, tc := range []struct {
		value   string
		want    bool
		wantErr bool
	}{
		{"", false, false},
		{"false", false, false},
		{"0", false, false},
		{"true", true, false},
		{"TRUE", true, false},
		{"1", true, false},
		{"t", true, false},
		{"bogus", false, true},
		{"yes", false, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("RUN_MIGRATIONS", tc.value)
			cfg, err := Load()
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "RUN_MIGRATIONS") {
					t.Fatalf("Load() error = %v, want an invalid RUN_MIGRATIONS error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.RunMigrations != tc.want {
				t.Errorf("RunMigrations = %v, want %v", cfg.RunMigrations, tc.want)
			}
		})
	}
}
