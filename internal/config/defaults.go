package config

var (
	DefaultProvider                = "openai"
	DefaultModel                   = "gpt-4o-mini"
	DefaultOutputFormat            = "terminal"
	DefaultReportPath              = "lookup-report.md"
	DefaultMaxFileKB               = 500
	DefaultSkipDirs                = []string{"vendor", "node_modules", ".git", "dist", "build", ".idea", ".vscode"}
	DefaultOllamaBaseURL           = "http://localhost:11434"
	DefaultOllamaModel             = "llama3.1"
	DefaultMaxReviewedFindings     = 40
	DefaultMaxProviderRequests     = 6
	DefaultMaxEstimatedInputTokens = 60000
	DefaultMaxOutputTokens         = 16000
)
