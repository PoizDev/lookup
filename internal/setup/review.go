package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/poizdev/lookup/internal/completion"
	"github.com/poizdev/lookup/internal/config"
)

type WizardConfig struct {
	Provider, APIKey, Model, OllamaURL, OllamaModel, OutputFmt string
	SkipDirs                                                   []string
	ProviderKeys                                               map[string]string
}

func SaveConfig(cfg WizardConfig) error {
	dir, err := config.ConfigDir()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err = os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("secure config dir: %w", err)
	}
	if cfg.OutputFmt == "" {
		cfg.OutputFmt = "markdown"
	}
	if cfg.OllamaURL == "" {
		cfg.OllamaURL = defaultOllamaURL
	}
	if cfg.OllamaModel == "" {
		cfg.OllamaModel = "llama3.1"
	}
	if len(cfg.SkipDirs) == 0 {
		cfg.SkipDirs = defaultSkipDirs
	}
	quoted := make([]string, len(cfg.SkipDirs))
	for i, d := range cfg.SkipDirs {
		quoted[i] = fmt.Sprintf("%q", d)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[ai]\nprovider = %q\nmodel = %q\n", cfg.Provider, cfg.Model)
	fmt.Fprintf(&b, "\n[ai.openai]\napi_key = %q\n", valueForProvider(cfg, "openai"))
	fmt.Fprintf(&b, "\n[ai.anthropic]\napi_key = %q\n", valueForProvider(cfg, "anthropic"))
	fmt.Fprintf(&b, "\n[ai.gemini]\napi_key = %q\n", valueForProvider(cfg, "gemini"))
	fmt.Fprintf(&b, "\n[ai.ollama]\nbase_url = %q\nmodel = %q\n", cfg.OllamaURL, cfg.OllamaModel)
	fmt.Fprintf(&b, "\n[output]\nformat = %q\nreport_path = \"lookup-report.md\"\nquiet = false\n", cfg.OutputFmt)
	fmt.Fprintf(&b, "\n[analysis]\nskip_dirs = [%s]\nmax_file_kb = 500\n", strings.Join(quoted, ", "))
	path := filepath.Join(dir, "config.toml")
	if err = os.WriteFile(path, []byte(b.String()), 0600); err != nil {
		return err
	}
	return os.Chmod(path, 0600)
}
func valueForProvider(cfg WizardConfig, provider string) string {
	if value := cfg.ProviderKeys[provider]; value != "" {
		return value
	}
	if cfg.Provider == provider {
		return cfg.APIKey
	}
	return ""
}
func FormatReview(cfg WizardConfig, shell ...interface{}) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Provider       %s\n", providerDisplayName(cfg.Provider))
	if cfg.Provider != "ollama" {
		fmt.Fprintf(&b, "API key        %s\n", maskKey(cfg.APIKey))
	}
	if cfg.Model != "" {
		fmt.Fprintf(&b, "Model          %s\n", cfg.Model)
	}
	if cfg.Provider == "ollama" {
		fmt.Fprintf(&b, "Ollama URL     %s\nOllama Model   %s\n", cfg.OllamaURL, cfg.OllamaModel)
	}
	title := strings.ToUpper(cfg.OutputFmt[:1]) + cfg.OutputFmt[1:]
	fmt.Fprintf(&b, "Output         %s\n", title)
	if cfg.OutputFmt == "markdown" {
		b.WriteString("Report         ./lookup-report.md\n")
	}
	fmt.Fprintf(&b, "Skip dirs      %d selected\n", len(cfg.SkipDirs))
	if len(shell) >= 2 {
		selected, _ := shell[0].(completion.Shell)
		install, _ := shell[1].(bool)
		if install {
			fmt.Fprintf(&b, "Shell          %s completion\n", selected)
		} else {
			b.WriteString("Shell          Not installed\n")
		}
	}
	return b.String()
}
