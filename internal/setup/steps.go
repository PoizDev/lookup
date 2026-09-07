package setup

type Step int

const (
	StepProvider Step = iota
	StepAPIKey
	StepValidate
	StepModel
	StepOllama
	StepOutput
	StepSkipDirs
	StepShellIntegration
	StepReview
)

type StepInfo struct{ Title, Description string }

var stepInfoMap = map[Step]StepInfo{
	StepProvider: {"AI Provider", "Select your AI provider"}, StepAPIKey: {"API Key", "Enter your API key"}, StepValidate: {"Validation", "Verify your API key"}, StepModel: {"Model", "Select a model"}, StepOllama: {"Ollama Setup", "Configure the local endpoint and model"}, StepOutput: {"Output Format", "Choose the default report format"}, StepSkipDirs: {"Skip Directories", "Choose directories excluded from scans"}, StepShellIntegration: {"Shell Integration", "Install Lookup shell completion"}, StepReview: {"Review & Save", "Review and save your configuration"},
}
