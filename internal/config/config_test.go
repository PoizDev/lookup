package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/poizdev/lookup/internal/apperror"
)

func writeConfigFile(t *testing.T, content string) {
	t.Helper()
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	dir := filepath.Join(base, "lookup")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAcceptsExistingConfigWithoutRequestTimeouts(t *testing.T) {
	writeConfigFile(t, "[ai]\nprovider = \"ollama\"\n[ai.ollama]\nbase_url = \"http://localhost:11434\"\nmodel = \"qwen2.5-coder:7b\"\n")
	cfg, err := Load(CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AI.RequestTimeout != "" || cfg.AI.Ollama.RequestTimeout != "" {
		t.Fatalf("timeouts = %q / %q, want absent", cfg.AI.RequestTimeout, cfg.AI.Ollama.RequestTimeout)
	}
}

func TestLoadParsesValidGoRequestDurationsWithOllamaPrecedence(t *testing.T) {
	writeConfigFile(t, "[ai]\nrequest_timeout = \"30s\"\n[ai.ollama]\nrequest_timeout = \"3m\"\n")
	cfg, err := Load(CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := cfg.AI.EffectiveRequestTimeout("openai", 30*time.Second); err != nil || got != 30*time.Second {
		t.Fatalf("cloud timeout = %v, error = %v", got, err)
	}
	if got, err := cfg.AI.EffectiveRequestTimeout("ollama", 180*time.Second); err != nil || got != 3*time.Minute {
		t.Fatalf("Ollama timeout = %v, error = %v", got, err)
	}
}

func TestLoadRejectsInvalidRequestDuration(t *testing.T) {
	writeConfigFile(t, "[ai]\nrequest_timeout = \"eventually\"\n")
	_, err := Load(CLIFlags{})
	if err == nil || !strings.Contains(err.Error(), "ai.request_timeout") || !strings.Contains(err.Error(), "Go duration") {
		t.Fatalf("Load error = %v, want actionable ai.request_timeout error", err)
	}
}

func TestLoadRejectsNonPositiveRequestDurations(t *testing.T) {
	for _, value := range []string{"0s", "-1s"} {
		t.Run(value, func(t *testing.T) {
			writeConfigFile(t, "[ai.ollama]\nrequest_timeout = \""+value+"\"\n")
			_, err := Load(CLIFlags{})
			if err == nil || !strings.Contains(err.Error(), "ai.ollama.request_timeout") || !strings.Contains(err.Error(), "positive") {
				t.Fatalf("Load error = %v, want positive-duration error", err)
			}
		})
	}
}

func TestConfigDirUsesPlatformConfigDirectory(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	dir, err := ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "lookup"); dir != want {
		t.Fatalf("ConfigDir() = %q, want %q", dir, want)
	}
}

func TestEnsureConfigDirWritesPrivateConfig(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	if err := EnsureConfigDir(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(base, "lookup", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("config permissions = %o, want 600", got)
	}
	dirInfo, err := os.Stat(filepath.Join(base, "lookup"))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("config directory permissions = %o, want 700", got)
	}
}

func TestLoadReturnsTypedConfigurationError(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	dir := filepath.Join(base, "lookup")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("not = [valid"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(CLIFlags{})
	var typed *apperror.Error
	if !errors.As(err, &typed) || typed.Kind != apperror.KindConfiguration {
		t.Fatalf("Load error = %#v, want configuration error", err)
	}
}

func TestLoadIncludeTestsDefaultsToFalse(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg, err := Load(CLIFlags{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Analysis.IncludeTests {
		t.Fatal("IncludeTests = true, want false")
	}
}

func TestLoadIncludeTestsFromCLI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg, err := Load(CLIFlags{IncludeTests: true})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Analysis.IncludeTests {
		t.Fatal("IncludeTests = false, want true")
	}
}

func TestLoadModelOverrides(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg, err := Load(CLIFlags{Model: "custom-model"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AI.Model != "custom-model" || cfg.AI.Ollama.Model != "custom-model" {
		t.Fatalf("Model = %q, Ollama.Model = %q, want custom-model", cfg.AI.Model, cfg.AI.Ollama.Model)
	}
}

func TestAIReviewModeDefaultsToSmart(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg, err := Load(CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AI.ReviewMode != "smart" {
		t.Fatalf("ReviewMode = %q", cfg.AI.ReviewMode)
	}
}

func TestNoAIOverridesExplicitAIReviewMode(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg, err := Load(CLIFlags{AIReview: "all", NoAI: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AI.ReviewMode != "off" || !cfg.AI.NoAI {
		t.Fatalf("AI config = %#v", cfg.AI)
	}
}

func TestLoadRejectsUnknownAIReviewMode(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := Load(CLIFlags{AIReview: "sometimes"}); err == nil {
		t.Fatal("Load accepted unknown AI review mode")
	}
}

func TestReviewBudgetDefaultsAndCLIOverridesEnvironment(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LOOKUP_AI_MAX_REVIEWED_FINDINGS", "12")
	cfg, err := Load(CLIFlags{MaxReviewedFindings: 7})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AI.ReviewBudget.MaxReviewedFindings != 7 || cfg.AI.ReviewBudget.MaxProviderRequests != DefaultMaxProviderRequests || cfg.AI.ReviewBudget.MaxEstimatedInputTokens != DefaultMaxEstimatedInputTokens || cfg.AI.ReviewBudget.MaxOutputTokens != DefaultMaxOutputTokens {
		t.Fatalf("review budget = %#v", cfg.AI.ReviewBudget)
	}
}
