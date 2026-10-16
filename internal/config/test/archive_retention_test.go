package test

import (
	"cpa-usage-keeper/internal/config"
	"testing"
)

func TestRawRetentionConfig(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int
	}{{"", 0}, {"0", 0}, {"-1", 0}, {"30", 0}, {"89", 0}, {"90", 90}, {"180", 180}} {
		t.Run(tc.value, func(t *testing.T) {
			withIsolatedEnvFiles(t)
			t.Setenv("CPA_BASE_URL", "http://localhost:8317")
			t.Setenv("CPA_MANAGEMENT_KEY", "secret")
			t.Setenv("USAGE_RAW_RETENTION_DAYS", tc.value)
			cfg, err := config.LoadFromEnv()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.UsageRawRetentionDays != tc.want {
				t.Fatalf("got %d want %d", cfg.UsageRawRetentionDays, tc.want)
			}
		})
	}
}

// 热表窗口可调后，总保留期的下限跟随热表窗口而不是固定 90 天。
func TestRawRetentionConfigTracksHotWindow(t *testing.T) {
	for _, tc := range []struct {
		hot  string
		raw  string
		want int
	}{
		{"30", "30", 30},
		{"30", "29", 0},
		{"120", "90", 0},
		{"120", "120", 120},
	} {
		t.Run(tc.hot+"/"+tc.raw, func(t *testing.T) {
			withIsolatedEnvFiles(t)
			t.Setenv("CPA_BASE_URL", "http://localhost:8317")
			t.Setenv("CPA_MANAGEMENT_KEY", "secret")
			t.Setenv("USAGE_EVENTS_HOT_RETENTION_DAYS", tc.hot)
			t.Setenv("USAGE_RAW_RETENTION_DAYS", tc.raw)
			cfg, err := config.LoadFromEnv()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.UsageRawRetentionDays != tc.want {
				t.Fatalf("got %d want %d", cfg.UsageRawRetentionDays, tc.want)
			}
		})
	}
}

func TestStorageRetentionDefaultsAndValidation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		env        map[string]string
		wantHot    int
		wantErrors int
		wantHourly int
		wantErr    bool
	}{
		{name: "defaults", env: nil, wantHot: 90, wantErrors: 30, wantHourly: 90},
		{name: "overrides", env: map[string]string{"USAGE_EVENTS_HOT_RETENTION_DAYS": "60", "ERROR_EVENTS_RETENTION_DAYS": "7", "USAGE_OVERVIEW_HOURLY_RETENTION_DAYS": "30"}, wantHot: 60, wantErrors: 7, wantHourly: 30},
		{name: "cleanup disabled", env: map[string]string{"ERROR_EVENTS_RETENTION_DAYS": "0", "USAGE_OVERVIEW_HOURLY_RETENTION_DAYS": "0"}, wantHot: 90, wantErrors: 0, wantHourly: 0},
		{name: "hot window must be positive", env: map[string]string{"USAGE_EVENTS_HOT_RETENTION_DAYS": "0"}, wantErr: true},
		{name: "error retention must be non-negative", env: map[string]string{"ERROR_EVENTS_RETENTION_DAYS": "-1"}, wantErr: true},
		{name: "hourly retention must be non-negative", env: map[string]string{"USAGE_OVERVIEW_HOURLY_RETENTION_DAYS": "-1"}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withIsolatedEnvFiles(t)
			t.Setenv("CPA_BASE_URL", "http://localhost:8317")
			t.Setenv("CPA_MANAGEMENT_KEY", "secret")
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			cfg, err := config.LoadFromEnv()
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.UsageEventsHotRetentionDays != tc.wantHot || cfg.ErrorEventsRetentionDays != tc.wantErrors || cfg.UsageOverviewHourlyRetentionDays != tc.wantHourly {
				t.Fatalf("hot=%d errors=%d hourly=%d", cfg.UsageEventsHotRetentionDays, cfg.ErrorEventsRetentionDays, cfg.UsageOverviewHourlyRetentionDays)
			}
		})
	}
}
