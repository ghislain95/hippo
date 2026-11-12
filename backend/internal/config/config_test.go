package config

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/schedule"
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
			BatchSchedule:    schedule.Daily{Hour: 2, Minute: 0},
			BackupSchedule:   schedule.Daily{Hour: 3, Minute: 0},
			BackupDir:        "/backups",
			MonthlyBudgetUSD: 5,
			Prices:           Prices{InputPerMTok: 1, OutputPerMTok: 5},
		}
		if cfg != want {
			t.Errorf("Load() = %+v, want %+v", cfg, want)
		}
	})

	t.Run("reads every variable", func(t *testing.T) {
		cfg, err := Load(env(map[string]string{
			"API_TOKEN":                          validToken,
			"HTTP_ADDR":                          "127.0.0.1:9000",
			"DATA_DIR":                           "/tmp/hippo",
			"LOG_LEVEL":                          "debug",
			"ANTHROPIC_API_KEY":                  "sk-ant-test",
			"ANALYSIS_MODEL":                     "claude-haiku-4-5-20251001",
			"BATCH_SCHEDULE":                     "30 1 * * *",
			"BACKUP_SCHEDULE":                    "30 4 * * *",
			"BACKUP_DIR":                         "/tmp/hippo-backups",
			"MONTHLY_BUDGET_USD":                 "2.5",
			"ANALYSIS_INPUT_PRICE_USD_PER_MTOK":  "0.8",
			"ANALYSIS_OUTPUT_PRICE_USD_PER_MTOK": "4",
		}))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.HTTPAddr != "127.0.0.1:9000" || cfg.DataDir != "/tmp/hippo" || cfg.LogLevel != slog.LevelDebug ||
			cfg.AnthropicAPIKey != "sk-ant-test" || cfg.AnalysisModel != "claude-haiku-4-5-20251001" ||
			cfg.BatchSchedule != (schedule.Daily{Hour: 1, Minute: 30}) || cfg.BackupSchedule != (schedule.Daily{Hour: 4, Minute: 30}) || cfg.BackupDir != "/tmp/hippo-backups" || cfg.MonthlyBudgetUSD != 2.5 ||
			cfg.Prices != (Prices{InputPerMTok: 0.8, OutputPerMTok: 4}) {
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
		{"rejects a schedule that is not daily", map[string]string{"API_TOKEN": validToken, "BATCH_SCHEDULE": "0 2 1 * *"}, "BATCH_SCHEDULE"},
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

func TestBatchSchedule(t *testing.T) {
	t.Run("The default schedule is 02:00", func(t *testing.T) {
		cfg, err := Load(env(map[string]string{"API_TOKEN": validToken}))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.BatchSchedule != (schedule.Daily{Hour: 2, Minute: 0}) {
			t.Errorf("BatchSchedule = %+v, want 02:00", cfg.BatchSchedule)
		}
	})

	t.Run("A schedule that is not daily stops the start-up", func(t *testing.T) {
		for _, value := range []string{"0 2 * * 1", "*/5 * * * *", "0 25 * * *", "nightly"} {
			_, err := Load(env(map[string]string{"API_TOKEN": validToken, "BATCH_SCHEDULE": value}))
			if err == nil {
				t.Errorf("Load with BATCH_SCHEDULE %q: want an error", value)
				continue
			}
			if !strings.Contains(err.Error(), "BATCH_SCHEDULE") {
				t.Errorf("Load with BATCH_SCHEDULE %q: error %q does not name BATCH_SCHEDULE", value, err)
			}
		}
	})
}

func TestBackupSchedule(t *testing.T) {
	t.Run("The default backup schedule is 03:00", func(t *testing.T) {
		cfg, err := Load(env(map[string]string{"API_TOKEN": validToken}))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.BackupSchedule != (schedule.Daily{Hour: 3, Minute: 0}) {
			t.Errorf("BackupSchedule = %+v, want 03:00", cfg.BackupSchedule)
		}
	})

	t.Run("A backup schedule that is not daily stops the start-up", func(t *testing.T) {
		for _, value := range []string{"0 3 * * 1", "*/5 3 * * *", "3:00"} {
			_, err := Load(env(map[string]string{"API_TOKEN": validToken, "BACKUP_SCHEDULE": value}))
			if err == nil {
				t.Errorf("Load with BACKUP_SCHEDULE %q: want an error", value)
				continue
			}
			if !strings.Contains(err.Error(), "BACKUP_SCHEDULE") {
				t.Errorf("Load with BACKUP_SCHEDULE %q: error %q does not name BACKUP_SCHEDULE", value, err)
			}
		}
	})
}

func TestBackupDir(t *testing.T) {
	t.Run("The backup folder defaults to /backups", func(t *testing.T) {
		cfg, err := Load(env(map[string]string{"API_TOKEN": validToken}))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.BackupDir != "/backups" {
			t.Errorf("BackupDir = %q, want /backups", cfg.BackupDir)
		}
	})

	t.Run("A backup folder beside the data folder is accepted", func(t *testing.T) {
		for _, dirs := range [][2]string{
			{"/data", "/backups"},
			{"../data/dev", "../data/dev-backups"},
			{"/srv/data", "/srv/data-backups"},
		} {
			cfg, err := Load(env(map[string]string{"API_TOKEN": validToken, "DATA_DIR": dirs[0], "BACKUP_DIR": dirs[1]}))
			if err != nil {
				t.Errorf("Load with DATA_DIR %q and BACKUP_DIR %q: %v", dirs[0], dirs[1], err)
				continue
			}
			if cfg.BackupDir != dirs[1] {
				t.Errorf("BackupDir = %q, want %q", cfg.BackupDir, dirs[1])
			}
		}
	})

	t.Run("A backup folder that is, holds or is inside the data folder is refused", func(t *testing.T) {
		for _, backupDir := range []string{"/data", "/data/", "/data/backups", "/data/../data/x", "/", "/srv/..", "/data/assets/../.."} {
			_, err := Load(env(map[string]string{"API_TOKEN": validToken, "DATA_DIR": "/data", "BACKUP_DIR": backupDir}))
			if err == nil {
				t.Errorf("Load with BACKUP_DIR %q: want an error", backupDir)
				continue
			}
			if !strings.Contains(err.Error(), "BACKUP_DIR") {
				t.Errorf("Load with BACKUP_DIR %q: error %q does not name BACKUP_DIR", backupDir, err)
			}
		}
		// The parent of a nested data folder holds it as well.
		_, err := Load(env(map[string]string{"API_TOKEN": validToken, "DATA_DIR": "/srv/hippo/data", "BACKUP_DIR": "/srv/hippo"}))
		if err == nil || !strings.Contains(err.Error(), "BACKUP_DIR") {
			t.Errorf("Load with BACKUP_DIR the parent of DATA_DIR: error %v, want one naming BACKUP_DIR", err)
		}
	})
}

func TestPrices(t *testing.T) {
	t.Run("The prices default to the Haiku 4.5 prices", func(t *testing.T) {
		cfg, err := Load(env(map[string]string{"API_TOKEN": validToken}))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.AnalysisModel != "claude-haiku-4-5" || cfg.Prices != (Prices{InputPerMTok: 1, OutputPerMTok: 5}) {
			t.Errorf("model %q, prices %+v; want claude-haiku-4-5 at 1 and 5", cfg.AnalysisModel, cfg.Prices)
		}
	})

	t.Run("An invalid price stops the start-up", func(t *testing.T) {
		for _, name := range []string{"ANALYSIS_INPUT_PRICE_USD_PER_MTOK", "ANALYSIS_OUTPUT_PRICE_USD_PER_MTOK"} {
			for _, value := range []string{"0", "-1", "abc", "NaN", "Inf"} {
				_, err := Load(env(map[string]string{"API_TOKEN": validToken, name: value}))
				if err == nil {
					t.Errorf("Load with %s %q: want an error", name, value)
					continue
				}
				if !strings.Contains(err.Error(), name) {
					t.Errorf("Load with %s %q: error %q does not name the variable", name, value, err)
				}
			}
		}
	})
}
