// Package config reads the backend configuration from environment variables.
//
// Every variable is documented in infra/.env.example. Load fails on any missing or invalid
// value, so the binary never starts with a half-valid configuration.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
)

// MinAPITokenLength is the minimum length of API_TOKEN, in bytes (ADR-0016).
const MinAPITokenLength = 32

// Config is the validated backend configuration.
type Config struct {
	// HTTPAddr is the listen address of the HTTP server (HTTP_ADDR).
	HTTPAddr string
	// DataDir holds the SQLite database and the images (DATA_DIR).
	DataDir string
	// APIToken is the static bearer token shared with the app (API_TOKEN). Never logged.
	APIToken string
	// LogLevel is the minimum level of the JSON logs (LOG_LEVEL).
	LogLevel slog.Level
	// AnthropicAPIKey is the dedicated Anthropic key (ANTHROPIC_API_KEY). Never logged.
	// Optional until the analysis lands (E3).
	AnthropicAPIKey string
	// AnalysisModel is the Claude model used for the analysis (ANALYSIS_MODEL).
	AnalysisModel string
	// BatchSchedule is the cron expression of the nightly batch (BATCH_SCHEDULE).
	BatchSchedule string
	// BackupSchedule is the cron expression of the nightly backup (BACKUP_SCHEDULE).
	BackupSchedule string
	// MonthlyBudgetUSD caps the AI spend of a calendar month (MONTHLY_BUDGET_USD, RG-81).
	MonthlyBudgetUSD float64
}

// Defaults, as written in docs/spec-v1.md and infra/.env.example.
const (
	defaultHTTPAddr         = ":8080"
	defaultDataDir          = "/data"
	defaultAnalysisModel    = "claude-haiku-4-5"
	defaultBatchSchedule    = "0 2 * * *"
	defaultBackupSchedule   = "0 3 * * *"
	defaultMonthlyBudgetUSD = 5.0
)

// Load builds the configuration from getenv (os.Getenv in production, a map in tests).
func Load(getenv func(string) string) (Config, error) {
	value := func(name, fallback string) string {
		if v := strings.TrimSpace(getenv(name)); v != "" {
			return v
		}
		return fallback
	}

	cfg := Config{
		HTTPAddr:        value("HTTP_ADDR", defaultHTTPAddr),
		DataDir:         value("DATA_DIR", defaultDataDir),
		APIToken:        getenv("API_TOKEN"),
		AnthropicAPIKey: getenv("ANTHROPIC_API_KEY"),
		AnalysisModel:   value("ANALYSIS_MODEL", defaultAnalysisModel),
		BatchSchedule:   value("BATCH_SCHEDULE", defaultBatchSchedule),
		BackupSchedule:  value("BACKUP_SCHEDULE", defaultBackupSchedule),
	}

	var errs []error
	if len(cfg.APIToken) < MinAPITokenLength {
		errs = append(errs, fmt.Errorf("API_TOKEN must be at least %d bytes long", MinAPITokenLength))
	}

	level, err := parseLogLevel(value("LOG_LEVEL", "info"))
	if err != nil {
		errs = append(errs, err)
	}
	cfg.LogLevel = level

	budget, err := parseBudget(getenv("MONTHLY_BUDGET_USD"))
	if err != nil {
		errs = append(errs, err)
	}
	cfg.MonthlyBudgetUSD = budget

	if err := errors.Join(errs...); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}

func parseLogLevel(raw string) (slog.Level, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(raw)); err != nil {
		return slog.LevelInfo, fmt.Errorf("LOG_LEVEL %q: want debug, info, warn or error", raw)
	}
	return level, nil
}

func parseBudget(raw string) (float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultMonthlyBudgetUSD, nil
	}
	budget, err := strconv.ParseFloat(raw, 64)
	// NaN and Inf parse fine but would make every "spent >= budget" check false (RG-81).
	if err != nil || budget < 0 || math.IsNaN(budget) || math.IsInf(budget, 0) {
		return 0, fmt.Errorf("MONTHLY_BUDGET_USD %q: want a non-negative number of dollars", raw)
	}
	return budget, nil
}
