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
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ghislain95/hippo/backend/internal/schedule"
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
	// BatchSchedule is the time of the nightly batch, Paris wall-clock time (BATCH_SCHEDULE,
	// daily form "M H * * *" only, RG-30).
	BatchSchedule schedule.Daily
	// BackupSchedule is the time of the nightly backup, Paris wall-clock time (BACKUP_SCHEDULE,
	// daily form "M H * * *" only, like BatchSchedule).
	BackupSchedule schedule.Daily
	// BackupDir is the folder that holds the backups (BACKUP_DIR), fixed by the image and by
	// make dev. It is never the data folder, inside it nor around it: a backup would copy itself.
	BackupDir string
	// MonthlyBudgetUSD caps the AI spend of a calendar month (MONTHLY_BUDGET_USD, RG-81).
	MonthlyBudgetUSD float64
	// Prices are the standard prices of AnalysisModel, which price a batch's tokens (RG-80).
	Prices Prices
}

// Prices are the standard prices of the analysis model, in US dollars per million tokens
// (ANALYSIS_INPUT_PRICE_USD_PER_MTOK, ANALYSIS_OUTPUT_PRICE_USD_PER_MTOK). The batch discount
// and the cache ratios are applied in code (analysis.BatchCost, apply-batch-results design D12).
type Prices struct {
	InputPerMTok  float64
	OutputPerMTok float64
}

// Defaults, as written in docs/spec-v1.md and infra/.env.example.
const (
	defaultHTTPAddr         = ":8080"
	defaultDataDir          = "/data"
	defaultAnalysisModel    = "claude-haiku-4-5"
	defaultBatchSchedule    = "0 2 * * *"
	defaultBackupSchedule   = "0 3 * * *"
	defaultBackupDir        = "/backups"
	defaultMonthlyBudgetUSD = 5.0
	// Standard prices of Claude Haiku 4.5, checked on 2026-11-07 at
	// https://platform.claude.com/docs/en/about-claude/pricing (apply-batch-results D12).
	defaultInputPricePerMTok  = 1.0
	defaultOutputPricePerMTok = 5.0
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
		BackupDir:       value("BACKUP_DIR", defaultBackupDir),
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

	batch, err := schedule.ParseDaily(value("BATCH_SCHEDULE", defaultBatchSchedule))
	if err != nil {
		errs = append(errs, fmt.Errorf("BATCH_SCHEDULE: %w", err))
	}
	cfg.BatchSchedule = batch

	backup, err := schedule.ParseDaily(value("BACKUP_SCHEDULE", defaultBackupSchedule))
	if err != nil {
		errs = append(errs, fmt.Errorf("BACKUP_SCHEDULE: %w", err))
	}
	cfg.BackupSchedule = backup

	if err := checkBackupDir(cfg.BackupDir, cfg.DataDir); err != nil {
		errs = append(errs, err)
	}

	budget, err := parseBudget(getenv("MONTHLY_BUDGET_USD"))
	if err != nil {
		errs = append(errs, err)
	}
	cfg.MonthlyBudgetUSD = budget

	input, err := parsePrice("ANALYSIS_INPUT_PRICE_USD_PER_MTOK", getenv("ANALYSIS_INPUT_PRICE_USD_PER_MTOK"), defaultInputPricePerMTok)
	if err != nil {
		errs = append(errs, err)
	}
	output, err := parsePrice("ANALYSIS_OUTPUT_PRICE_USD_PER_MTOK", getenv("ANALYSIS_OUTPUT_PRICE_USD_PER_MTOK"), defaultOutputPricePerMTok)
	if err != nil {
		errs = append(errs, err)
	}
	cfg.Prices = Prices{InputPerMTok: input, OutputPerMTok: output}

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

// parsePrice reads the price variable name: fallback when empty, otherwise a finite number
// above zero. A price of zero would make every batch free and the budget unreachable (RG-81).
func parsePrice(name, raw string, fallback float64) (float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	price, err := strconv.ParseFloat(raw, 64)
	if err != nil || price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
		return 0, fmt.Errorf("%s %q: want a positive number of dollars per million tokens", name, raw)
	}
	return price, nil
}

// checkBackupDir refuses a backup folder that is the data folder, inside it or around it: a
// backup inside the data would back itself up, and one around it would hold the data. The
// check is lexical, on the cleaned absolute paths; the folders need not exist yet.
func checkBackupDir(backupDir, dataDir string) error {
	backup, err := filepath.Abs(backupDir)
	if err != nil {
		return fmt.Errorf("BACKUP_DIR: %w", err)
	}
	data, err := filepath.Abs(dataDir)
	if err != nil {
		return fmt.Errorf("DATA_DIR: %w", err)
	}
	if within(backup, data) || within(data, backup) {
		return errors.New("BACKUP_DIR must be outside DATA_DIR, and must not hold it")
	}
	return nil
}

// within tells whether the cleaned absolute path inner is outer or lies inside it.
func within(inner, outer string) bool {
	rel, err := filepath.Rel(outer, inner)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
