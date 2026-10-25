package config

import (
	"log/slog"
	"strings"
	"testing"
)

const validToken = "0123456789abcdef0123456789abcdef"

func env(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestLoad(t *testing.T) {
	t.Run("applies the defaults of the spec", func(t *testing.T) {
		cfg, err := Load(env(map[string]string{"API_TOKEN": validToken}))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		want := Config{
			HTTPAddr:         ":8080",
			DataDir:          "/data",
			APIToken:         validToken,
			LogLevel:         slog.LevelInfo,
			AnalysisModel:    "claude-haiku-4-5",
			BatchSchedule:    "0 2 * * *",
			BackupSchedule:   "0 3 * * *",
			MonthlyBudgetUSD: 5,
		}
		if cfg != want {
			t.Errorf("Load() = %+v, want %+v", cfg, want)
		}
	})

	t.Run("reads every variable", func(t *testing.T) {
		cfg, err := Load(env(map[string]string{
			"API_TOKEN":          validToken,
			"HTTP_ADDR":          "127.0.0.1:9000",
			"DATA_DIR":           "/tmp/hippo",
			"LOG_LEVEL":          "debug",
			"ANTHROPIC_API_KEY":  "sk-ant-test",
			"ANALYSIS_MODEL":     "claude-haiku-4-5-20251001",
			"BATCH_SCHEDULE":     "30 1 * * *",
			"BACKUP_SCHEDULE":    "30 4 * * *",
			"MONTHLY_BUDGET_USD": "2.5",
		}))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.HTTPAddr != "127.0.0.1:9000" || cfg.DataDir != "/tmp/hippo" || cfg.LogLevel != slog.LevelDebug ||
			cfg.AnthropicAPIKey != "sk-ant-test" || cfg.AnalysisModel != "claude-haiku-4-5-20251001" ||
			cfg.BatchSchedule != "30 1 * * *" || cfg.BackupSchedule != "30 4 * * *" || cfg.MonthlyBudgetUSD != 2.5 {
			t.Errorf("Load() = %+v", cfg)
		}
	})

	invalid := []struct {
		name    string
		values  map[string]string
		message string
	}{
		{"rejects a missing token", map[string]string{}, "API_TOKEN"},
		{"rejects a token shorter than 32 bytes", map[string]string{"API_TOKEN": "short"}, "API_TOKEN"},
		{"rejects an unknown log level", map[string]string{"API_TOKEN": validToken, "LOG_LEVEL": "verbose"}, "LOG_LEVEL"},
		{"rejects a budget that is not a number", map[string]string{"API_TOKEN": validToken, "MONTHLY_BUDGET_USD": "five"}, "MONTHLY_BUDGET_USD"},
		{"rejects a negative budget", map[string]string{"API_TOKEN": validToken, "MONTHLY_BUDGET_USD": "-1"}, "MONTHLY_BUDGET_USD"},
		{"rejects a budget that is not a number value", map[string]string{"API_TOKEN": validToken, "MONTHLY_BUDGET_USD": "NaN"}, "MONTHLY_BUDGET_USD"},
		{"rejects an infinite budget", map[string]string{"API_TOKEN": validToken, "MONTHLY_BUDGET_USD": "Inf"}, "MONTHLY_BUDGET_USD"},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(env(tc.values))
			if err == nil {
				t.Fatal("Load: want an error, got nil")
			}
			if !strings.Contains(err.Error(), tc.message) {
				t.Errorf("Load: error %q does not name %s", err, tc.message)
			}
			if strings.Contains(err.Error(), validToken) {
				t.Errorf("Load: error %q leaks the token", err)
			}
		})
	}
}
