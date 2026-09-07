package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/poizdev/lookup/internal/apperror"
)

type Config struct {
	AI       AIConfig       `toml:"ai"`
	Output   OutputConfig   `toml:"output"`
	Analysis AnalysisConfig `toml:"analysis"`
}

type AIConfig struct {
	Provider       string             `toml:"provider"`
	Model          string             `toml:"model"`
	RequestTimeout string             `toml:"request_timeout"`
	OpenAI         OpenAIConfig       `toml:"openai"`
	Anthropic      AnthropicConfig    `toml:"anthropic"`
	Gemini         GeminiConfig       `toml:"gemini"`
	Ollama         OllamaConfig       `toml:"ollama"`
	NoAI           bool               `toml:"no_ai"`
	ReviewMode     string             `toml:"review_mode"`
	ReviewBudget   ReviewBudgetConfig `toml:"review_budget"`
}

type ReviewBudgetConfig struct {
	MaxReviewedFindings     int `toml:"max_reviewed_findings"`
	MaxProviderRequests     int `toml:"max_provider_requests"`
	MaxEstimatedInputTokens int `toml:"max_estimated_input_tokens"`
	MaxOutputTokens         int `toml:"max_output_tokens"`
}

type OpenAIConfig struct {
	APIKey string `toml:"api_key"`
}

type AnthropicConfig struct {
	APIKey string `toml:"api_key"`
}

type GeminiConfig struct {
	APIKey string `toml:"api_key"`
}

type OllamaConfig struct {
	BaseURL        string `toml:"base_url"`
	Model          string `toml:"model"`
	RequestTimeout string `toml:"request_timeout"`
}

func (cfg AIConfig) EffectiveRequestTimeout(provider string, fallback time.Duration) (time.Duration, error) {
	value, field := cfg.RequestTimeout, "ai.request_timeout"
	if strings.EqualFold(strings.TrimSpace(provider), "ollama") && strings.TrimSpace(cfg.Ollama.RequestTimeout) != "" {
		value, field = cfg.Ollama.RequestTimeout, "ai.ollama.request_timeout"
	}
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	return parsePositiveRequestTimeout(field, value)
}

func parsePositiveRequestTimeout(field, value string) (time.Duration, error) {
	duration, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return 0, apperror.New(apperror.KindConfiguration, fmt.Sprintf("%s must be a positive Go duration such as \"30s\", \"180s\", or \"3m\": %v", field, err))
	}
	if duration <= 0 {
		return 0, apperror.New(apperror.KindConfiguration, fmt.Sprintf("%s must be a positive Go duration such as \"30s\", \"180s\", or \"3m\"", field))
	}
	return duration, nil
}

type OutputConfig struct {
	Format     string `toml:"format"` // terminal | markdown | json | sarif
	ReportPath string `toml:"report_path"`
	Quiet      bool   `toml:"quiet"`
	Detailed   bool   `toml:"detailed"`
}

type AnalysisConfig struct {
	SkipDirs     []string `toml:"skip_dirs"`
	MaxFileKB    int      `toml:"max_file_kb"`
	Severity     string   `toml:"severity"` // filter: show only this severity and above
	Category     string   `toml:"category"` // filter: show only this category
	IncludeTests bool     `toml:"include_tests"`
}

type CLIFlags struct {
	Provider                string
	Model                   string
	Output                  string
	Severity                string
	Category                string
	Quiet                   bool
	NoAI                    bool
	AIReview                string
	IncludeTests            bool
	Detailed                bool
	MaxReviewedFindings     int
	MaxProviderRequests     int
	MaxEstimatedInputTokens int
	MaxOutputTokens         int
	TargetPath              string
}

// Load loads the configuration with the following priority:
// CLI Flag > Environment Variable > TOML File > Defaults
func Load(flags CLIFlags) (*Config, error) {
	cfg := &Config{
		AI: AIConfig{
			Provider:     DefaultProvider,
			ReviewMode:   "smart",
			ReviewBudget: ReviewBudgetConfig{MaxReviewedFindings: DefaultMaxReviewedFindings, MaxProviderRequests: DefaultMaxProviderRequests, MaxEstimatedInputTokens: DefaultMaxEstimatedInputTokens, MaxOutputTokens: DefaultMaxOutputTokens},
			Ollama: OllamaConfig{
				BaseURL: DefaultOllamaBaseURL,
				Model:   DefaultOllamaModel,
			},
		},
		Output: OutputConfig{
			Format:     DefaultOutputFormat,
			ReportPath: DefaultReportPath,
		},
		Analysis: AnalysisConfig{
			SkipDirs:  DefaultSkipDirs,
			MaxFileKB: DefaultMaxFileKB,
		},
	}

	// Read from TOML file
	configDir, err := ConfigDir()
	if err != nil {
		return nil, apperror.Wrap(apperror.KindConfiguration, "determine configuration directory", err)
	}
	configPath := filepath.Join(configDir, "config.toml")
	if _, err := os.Stat(configPath); err == nil {
		if _, err := toml.DecodeFile(configPath, cfg); err != nil {
			return nil, apperror.Wrap(apperror.KindConfiguration, "decode configuration file", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, configFilesystemError("inspect configuration file", err)
	}

	// Read environment variables
	if p := os.Getenv("LOOKUP_PROVIDER"); p != "" {
		cfg.AI.Provider = p
	}
	if m := os.Getenv("LOOKUP_MODEL"); m != "" {
		cfg.AI.Model = m
		cfg.AI.Ollama.Model = m
	}
	if k := os.Getenv("LOOKUP_OPENAI_API_KEY"); k != "" {
		cfg.AI.OpenAI.APIKey = k
	}
	if k := os.Getenv("LOOKUP_ANTHROPIC_API_KEY"); k != "" {
		cfg.AI.Anthropic.APIKey = k
	}
	if k := os.Getenv("LOOKUP_GEMINI_API_KEY"); k != "" {
		cfg.AI.Gemini.APIKey = k
	}
	applyEnvInt("LOOKUP_AI_MAX_REVIEWED_FINDINGS", &cfg.AI.ReviewBudget.MaxReviewedFindings)
	applyEnvInt("LOOKUP_AI_MAX_PROVIDER_REQUESTS", &cfg.AI.ReviewBudget.MaxProviderRequests)
	applyEnvInt("LOOKUP_AI_MAX_INPUT_TOKENS", &cfg.AI.ReviewBudget.MaxEstimatedInputTokens)
	applyEnvInt("LOOKUP_AI_MAX_OUTPUT_TOKENS", &cfg.AI.ReviewBudget.MaxOutputTokens)

	// Override with CLI Flags
	if flags.Provider != "" {
		cfg.AI.Provider = flags.Provider
	}
	if flags.Model != "" {
		cfg.AI.Model = flags.Model
		cfg.AI.Ollama.Model = flags.Model
	}
	if flags.Output != "" {
		cfg.Output.Format = flags.Output
	}
	if flags.Severity != "" {
		cfg.Analysis.Severity = flags.Severity
	}
	if flags.Category != "" {
		cfg.Analysis.Category = flags.Category
	}
	if flags.Quiet {
		cfg.Output.Quiet = true
	}
	if flags.Detailed {
		cfg.Output.Detailed = true
	}
	if flags.NoAI {
		cfg.AI.NoAI = true
		cfg.AI.ReviewMode = "off"
	} else if flags.AIReview != "" {
		cfg.AI.ReviewMode = strings.ToLower(strings.TrimSpace(flags.AIReview))
	}
	if flags.IncludeTests {
		cfg.Analysis.IncludeTests = true
	}
	if flags.MaxReviewedFindings > 0 {
		cfg.AI.ReviewBudget.MaxReviewedFindings = flags.MaxReviewedFindings
	}
	if flags.MaxProviderRequests > 0 {
		cfg.AI.ReviewBudget.MaxProviderRequests = flags.MaxProviderRequests
	}
	if flags.MaxEstimatedInputTokens > 0 {
		cfg.AI.ReviewBudget.MaxEstimatedInputTokens = flags.MaxEstimatedInputTokens
	}
	if flags.MaxOutputTokens > 0 {
		cfg.AI.ReviewBudget.MaxOutputTokens = flags.MaxOutputTokens
	}

	if cfg.AI.NoAI {
		cfg.AI.ReviewMode = "off"
	}
	switch strings.ToLower(strings.TrimSpace(cfg.AI.ReviewMode)) {
	case "", "smart":
		cfg.AI.ReviewMode = "smart"
	case "all", "off":
		cfg.AI.ReviewMode = strings.ToLower(strings.TrimSpace(cfg.AI.ReviewMode))
	default:
		return nil, apperror.New(apperror.KindConfiguration, fmt.Sprintf("invalid AI review mode %q (want smart, all, or off)", cfg.AI.ReviewMode))
	}
	if strings.TrimSpace(cfg.AI.RequestTimeout) != "" {
		if _, err := parsePositiveRequestTimeout("ai.request_timeout", cfg.AI.RequestTimeout); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(cfg.AI.Ollama.RequestTimeout) != "" {
		if _, err := parsePositiveRequestTimeout("ai.ollama.request_timeout", cfg.AI.Ollama.RequestTimeout); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

func applyEnvInt(name string, target *int) {
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name))); err == nil && value > 0 {
		*target = value
	}
}

// ConfigDir returns Lookup's platform-native configuration directory.
func ConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("could not determine config directory: %w", err)
	}
	return filepath.Join(base, "lookup"), nil
}

// EnsureConfigDir creates Lookup's config directory and a private default config.
func EnsureConfigDir() error {
	configDir, err := ConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return configFilesystemError("create configuration directory", err)
	}
	if err := os.Chmod(configDir, 0o700); err != nil {
		return configFilesystemError("secure configuration directory", err)
	}

	configPath := filepath.Join(configDir, "config.toml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := os.WriteFile(configPath, []byte(defaultConfigTemplate), 0600); err != nil {
			return configFilesystemError("write default configuration file", err)
		}
	} else if err != nil {
		return configFilesystemError("inspect default configuration file", err)
	}
	if err := os.Chmod(configPath, 0600); err != nil {
		return configFilesystemError("secure configuration file", err)
	}
	return nil
}

func configFilesystemError(message string, err error) error {
	if os.IsPermission(err) {
		return apperror.Wrap(apperror.KindFilesystemPermission, message, err)
	}
	return apperror.Wrap(apperror.KindConfiguration, message, err)
}

const defaultConfigTemplate = `[ai]
provider = "openai"
model = "gpt-4o-mini"
review_mode = "smart"

[ai.review_budget]
max_reviewed_findings = 40
max_provider_requests = 6
max_estimated_input_tokens = 60000
max_output_tokens = 16000

[ai.openai]
# api_key = "your-openai-api-key"

[ai.anthropic]
# api_key = "your-anthropic-api-key"

[ai.gemini]
# api_key = "your-gemini-api-key"

[ai.ollama]
base_url = "http://localhost:11434"
model = "llama3.1"

[output]
format = "terminal"
report_path = "lookup-report.md"
quiet = false
detailed = false

[analysis]
skip_dirs = ["vendor", "node_modules", ".git", "dist", "build", ".idea", ".vscode"]
max_file_kb = 500
`
