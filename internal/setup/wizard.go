package setup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/poizdev/lookup/internal/completion"
	"github.com/poizdev/lookup/internal/config"
	"github.com/poizdev/lookup/internal/ui"
)

type validationMsg struct {
	provider   string
	generation uint64
	result     ValidationResult
}
type modelsMsg struct {
	provider   string
	generation uint64
	models     []ModelInfo
	err        error
}

type WizardModel struct {
	ctx                                                        context.Context
	cap                                                        ui.Capability
	styles                                                     *ui.Styles
	icons                                                      ui.IconSet
	currentStep, cursor                                        int
	width, height                                              int
	provider, apiKey, model, ollamaURL, ollamaModel, outputFmt string
	providerKeys                                               map[string]string
	skipDirs                                                   []string
	models                                                     []ModelInfo
	validation                                                 ValidationResult
	validationDone, validationRunning, fetchingModels          bool
	validationGeneration, modelGeneration                      uint64
	modelWarning                                               error
	cancelled, saved                                           bool
	detectedShell                                              completion.Shell
	installCompletion                                          bool
	completionInstaller                                        func(completion.Shell) error
	completionWarning, err                                     error
}

func NewWizardModel(installers ...func(completion.Shell) error) WizardModel {
	return newWizardModel(context.Background(), installers...)
}
func newWizardModel(ctx context.Context, installers ...func(completion.Shell) error) WizardModel {
	cap := ui.Detect(os.Stdout)
	m := WizardModel{ctx: ctx, cap: cap, styles: ui.NewStyles(ui.NewTheme(cap), cap), icons: ui.Icons(cap.Unicode), width: cap.Width, height: cap.Height, provider: "openai", providerKeys: map[string]string{}, outputFmt: "markdown", ollamaURL: defaultOllamaURL, ollamaModel: "llama3.1", skipDirs: append([]string(nil), defaultSkipDirs...), detectedShell: completion.DetectShell(), installCompletion: true}
	if len(installers) > 0 {
		m.completionInstaller = installers[0]
	}
	if dir, e := config.ConfigDir(); e == nil {
		if _, e = os.Stat(filepath.Join(dir, "config.toml")); e == nil {
			if cfg, loadErr := config.Load(config.CLIFlags{}); loadErr == nil {
				m.provider, m.model, m.outputFmt = cfg.AI.Provider, cfg.AI.Model, cfg.Output.Format
				m.ollamaURL, m.ollamaModel = cfg.AI.Ollama.BaseURL, cfg.AI.Ollama.Model
				m.skipDirs = append([]string(nil), cfg.Analysis.SkipDirs...)
				m.providerKeys["openai"], m.providerKeys["anthropic"], m.providerKeys["gemini"] = cfg.AI.OpenAI.APIKey, cfg.AI.Anthropic.APIKey, cfg.AI.Gemini.APIKey
				m.apiKey = m.providerKeys[m.provider]
			}
		}
	}
	m.resolveCursor()
	return m
}

func RunWizard(ctx context.Context, installer func(completion.Shell) error) error {
	if !ui.Detect(os.Stdout).IsTTY {
		return fmt.Errorf("lookup init requires an interactive terminal")
	}
	final, err := tea.NewProgram(newWizardModel(ctx, installer), tea.WithContext(ctx)).Run()
	if err != nil {
		return fmt.Errorf("wizard failed: %w", err)
	}
	m, ok := final.(WizardModel)
	if !ok {
		return fmt.Errorf("unexpected wizard model")
	}
	return m.err
}
func (m WizardModel) Init() tea.Cmd { return nil }
func (m WizardModel) activeSteps() []Step {
	steps := []Step{StepProvider}
	if m.provider == "ollama" {
		steps = append(steps, StepOllama)
	} else {
		steps = append(steps, StepAPIKey, StepValidate, StepModel)
	}
	return append(steps, StepOutput, StepSkipDirs, StepShellIntegration, StepReview)
}
func (m WizardModel) totalSteps() int { return len(m.activeSteps()) }
func (m WizardModel) currentStepType() Step {
	steps := m.activeSteps()
	if m.currentStep >= len(steps) {
		return StepReview
	}
	return steps[m.currentStep]
}

func (m WizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, v.Width), max(1, v.Height)
		m.cap.Width, m.cap.Height = m.width, m.height
		m.styles = ui.NewStyles(ui.NewTheme(m.cap), m.cap)
		return m, nil
	case validationMsg:
		if v.provider != m.provider || v.generation != m.validationGeneration {
			return m, nil
		}
		m.validation, m.validationDone, m.validationRunning = v.result, true, false
		if m.validation.Valid {
			m = m.advance()
			m.fetchingModels = true
			m.modelGeneration++
			return m, fetchModelsCmd(m.ctx, m.provider, m.apiKey, m.modelGeneration)
		}
		return m, nil
	case modelsMsg:
		if v.provider != m.provider || v.generation != m.modelGeneration {
			return m, nil
		}
		m.fetchingModels, m.models, m.modelWarning = false, v.models, v.err
		if v.err != nil || len(m.models) == 0 {
			m.models = fallbackModels(m.provider)
		}
		if !containsModel(m.models, m.model) && len(m.models) > 0 {
			m.model = m.models[0].ID
		}
		m.resolveCursor()
		return m, nil
	case tea.KeyMsg:
		if v.Type == tea.KeyCtrlC {
			m.cancelled = true
			return m, tea.Quit
		}
		if m.saved {
			return m, tea.Quit
		}
		step := m.currentStepType()
		if v.Type == tea.KeyEsc {
			if step == StepValidate && m.validationRunning {
				m.validationGeneration++
				m.validationRunning = false
			}
			if m.currentStep > 0 {
				m.currentStep--
				m.resolveCursor()
			}
			return m, nil
		}
		if step == StepAPIKey {
			return m.updateSecret(v)
		}
		if step == StepOllama {
			return m.updateOllamaKey(v)
		}
		key := v.String()
		if key == "q" {
			m.cancelled = true
			return m, tea.Quit
		}
		if step == StepValidate && m.validationDone && !m.validation.Valid {
			switch key {
			case "r":
				return m.startValidation()
			case "e":
				m.currentStep--
				return m, nil
			case "c":
				if m.validation.Kind != ValidationAuthFailure {
					m = m.advance()
					m.models = fallbackModels(m.provider)
					m.modelWarning = m.validation.Error
					m.resolveCursor()
				}
				return m, nil
			}
		}
		if key == "up" || key == "k" {
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		}
		if key == "down" || key == "j" {
			if m.cursor < m.choiceCount(step)-1 {
				m.cursor++
			}
			return m, nil
		}
		if step == StepSkipDirs && key == " " {
			m.toggleSkipDir(m.cursor)
			return m, nil
		}
		if v.Type == tea.KeyEnter {
			return m.selectCurrent(step)
		}
	}
	return m, nil
}

func (m WizardModel) startValidation() (tea.Model, tea.Cmd) {
	if m.validationRunning || strings.TrimSpace(m.apiKey) == "" {
		return m, nil
	}
	m.validationRunning, m.validationDone = true, false
	m.validationGeneration++
	return m, validateCmd(m.ctx, m.provider, m.apiKey, m.validationGeneration)
}
func (m WizardModel) updateSecret(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyEnter:
		if strings.TrimSpace(m.apiKey) != "" {
			m.providerKeys[m.provider] = m.apiKey
			m = m.advance()
			return m.startValidation()
		}
	case tea.KeyBackspace, tea.KeyDelete:
		m.apiKey = removeLastRune(m.apiKey)
	case tea.KeyRunes:
		m.apiKey += string(key.Runes)
	}
	return m, nil
}
func (m WizardModel) updateOllamaKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Type == tea.KeyEnter {
		if m.cursor == 0 {
			m.cursor = 1
			return m, nil
		}
		if m.ollamaModel == "" {
			m.ollamaModel = "llama3.1"
		}
		return m.advance(), nil
	}
	if key.Type == tea.KeyTab || key.Type == tea.KeyUp || key.Type == tea.KeyDown {
		m.cursor = 1 - m.cursor
		return m, nil
	}
	target := &m.ollamaURL
	if m.cursor == 1 {
		target = &m.ollamaModel
	}
	if key.Type == tea.KeyBackspace || key.Type == tea.KeyDelete {
		*target = removeLastRune(*target)
	} else if key.Type == tea.KeyRunes {
		*target += string(key.Runes)
	}
	return m, nil
}
func (m WizardModel) updateOllama(key string) (tea.Model, tea.Cmd) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	switch key {
	case "enter":
		msg.Type = tea.KeyEnter
	case "backspace":
		msg.Type = tea.KeyBackspace
	case "tab":
		msg.Type = tea.KeyTab
	case "up":
		msg.Type = tea.KeyUp
	case "down":
		msg.Type = tea.KeyDown
	}
	return m.updateOllamaKey(msg)
}

func (m *WizardModel) toggleSkipDir(index int) {
	if index < 0 || index >= len(defaultSkipDirs) {
		return
	}
	target := defaultSkipDirs[index]
	for i, dir := range m.skipDirs {
		if dir == target {
			m.skipDirs = append(m.skipDirs[:i], m.skipDirs[i+1:]...)
			return
		}
	}
	m.skipDirs = append(m.skipDirs, target)
}
func (m WizardModel) advance() WizardModel { m.currentStep++; m.resolveCursor(); return m }
func (m *WizardModel) resolveCursor() {
	m.cursor = 0
	switch m.currentStepType() {
	case StepProvider:
		for i, c := range providerChoices {
			if c.ID == m.provider {
				m.cursor = i
				return
			}
		}
	case StepModel:
		for i, c := range m.models {
			if c.ID == m.model {
				m.cursor = i
				return
			}
		}
	case StepOutput:
		for i, c := range outputChoices {
			if c.ID == m.outputFmt {
				m.cursor = i
				return
			}
		}
	case StepShellIntegration:
		if !m.installCompletion {
			m.cursor = m.choiceCount(StepShellIntegration) - 1
		}
	}
}
func (m WizardModel) choiceCount(step Step) int {
	switch step {
	case StepProvider:
		return len(providerChoices)
	case StepModel:
		return len(m.models)
	case StepOutput:
		return len(outputChoices)
	case StepSkipDirs:
		return len(defaultSkipDirs)
	case StepShellIntegration:
		if m.detectedShell == completion.Unknown {
			return 5
		}
		return 2
	}
	return 1
}

func (m WizardModel) selectCurrent(step Step) (tea.Model, tea.Cmd) {
	switch step {
	case StepProvider:
		selected := providerChoices[m.cursor].ID
		if selected != m.provider {
			m.providerKeys[m.provider] = m.apiKey
			m.provider, m.apiKey = selected, m.providerKeys[selected]
			m.validation, m.validationDone, m.validationRunning = ValidationResult{}, false, false
			m.validationGeneration++
			m.models, m.model, m.modelWarning, m.fetchingModels = nil, "", nil, false
			m.modelGeneration++
		}
		return m.advance(), nil
	case StepValidate:
		if !m.validationDone {
			return m.startValidation()
		}
	case StepModel:
		if len(m.models) == 0 {
			m.models = fallbackModels(m.provider)
		}
		m.model = m.models[min(m.cursor, len(m.models)-1)].ID
		return m.advance(), nil
	case StepOutput:
		m.outputFmt = outputChoices[m.cursor].ID
		return m.advance(), nil
	case StepSkipDirs:
		return m.advance(), nil
	case StepShellIntegration:
		if m.detectedShell == completion.Unknown {
			choices := []completion.Shell{completion.Bash, completion.Zsh, completion.Fish, completion.PowerShell}
			if m.cursor >= len(choices) {
				m.installCompletion = false
			} else {
				m.detectedShell, m.installCompletion = choices[m.cursor], true
			}
		} else {
			m.installCompletion = m.cursor == 0
		}
		return m.advance(), nil
	case StepReview:
		m.err = SaveConfig(m.config())
		m.saved = m.err == nil
		if m.saved && m.installCompletion && m.completionInstaller != nil {
			m.completionWarning = m.completionInstaller(m.detectedShell)
		}
		return m, nil
	}
	return m, nil
}

func validateCmd(ctx context.Context, provider, key string, generation ...uint64) tea.Cmd {
	var id uint64
	if len(generation) > 0 {
		id = generation[0]
	}
	return func() tea.Msg { return validationMsg{provider, id, ValidateAPIKey(ctx, provider, key)} }
}
func fetchModelsCmd(ctx context.Context, provider, key string, generation ...uint64) tea.Cmd {
	var id uint64
	if len(generation) > 0 {
		id = generation[0]
	}
	return func() tea.Msg {
		models, err := FetchModels(ctx, provider, key)
		return modelsMsg{provider, id, models, err}
	}
}
func (m WizardModel) config() WizardConfig {
	keys := make(map[string]string, len(m.providerKeys))
	for provider, key := range m.providerKeys {
		keys[provider] = key
	}
	if m.provider != "ollama" {
		keys[m.provider] = m.apiKey
	}
	return WizardConfig{Provider: m.provider, APIKey: m.apiKey, Model: m.model, OllamaURL: m.ollamaURL, OllamaModel: m.ollamaModel, OutputFmt: m.outputFmt, SkipDirs: m.skipDirs, ProviderKeys: keys}
}

func (m WizardModel) View() string {
	if m.cancelled {
		return ""
	}
	if m.saved {
		return m.successView()
	}
	step := m.currentStepType()
	info := stepInfoMap[step]
	var b strings.Builder
	headerWidth := min(60, max(20, m.cap.Width-8))
	fmt.Fprintf(&b, "%s\n\n%02d / %02d   %s\n%s\n\n%s\n\n", m.styles.RenderHeading("LOOKUP"), m.currentStep+1, m.totalSteps(), m.styles.RenderHeading(info.Title), m.styles.Divider(headerWidth), info.Description)
	switch step {
	case StepProvider:
		for i, p := range providerChoices {
			m.renderOption(&b, i, p.ID == m.provider, p.Name, p.Description, "")
		}
	case StepAPIKey:
		b.WriteString("API key\n")
		if m.apiKey == "" {
			b.WriteString("> [paste or type your key]\n")
		} else {
			length := secretLength(m.apiKey)
			fmt.Fprintf(&b, "> %s\n\n%d characters entered\n", strings.Repeat("•", min(length, 24)), length)
		}
	case StepValidate:
		m.renderValidation(&b)
	case StepModel:
		if m.validation.Valid {
			fmt.Fprintf(&b, "%s Connected to %s\n\n", m.icons.Success, providerDisplayName(m.provider))
		}
		if m.fetchingModels {
			b.WriteString("Fetching available models...\n")
		}
		if m.modelWarning != nil {
			fmt.Fprintf(&b, "%s Live model list unavailable\n  Showing Lookup's built-in model list.\n\n", m.icons.Warning)
		}
		start, end := m.modelWindow()
		for i := start; i < end; i++ {
			c := m.models[i]
			m.renderOption(&b, i, c.ID == m.model, c.DisplayName, "", "")
		}
		if len(m.models) > 0 {
			fmt.Fprintf(&b, "%d of %d\n", m.cursor+1, len(m.models))
		}
	case StepOllama:
		m.renderTextField(&b, "URL", m.ollamaURL, "Ollama server endpoint", m.cursor == 0)
		m.renderTextField(&b, "Model", m.ollamaModel, "Local model name", m.cursor == 1)
	case StepOutput:
		for i, c := range outputChoices {
			label := ""
			if c.Recommended {
				label = "RECOMMENDED"
			}
			m.renderOption(&b, i, c.ID == m.outputFmt, c.Title, c.Description, label)
		}
	case StepSkipDirs:
		for i, d := range defaultSkipDirs {
			mark := "[ ]"
			if containsDir(m.skipDirs, d) {
				mark = "[x]"
			}
			fmt.Fprintf(&b, "%s %s %s\n", m.focusMark(i), mark, d)
		}
	case StepShellIntegration:
		m.renderShell(&b)
	case StepReview:
		b.WriteString(FormatReview(m.config(), m.detectedShell, m.installCompletion))
		if m.err != nil {
			fmt.Fprintf(&b, "\n%s Configuration could not be saved: %v\n", m.icons.Error, m.err)
		}
	}
	fmt.Fprintf(&b, "\n%s\n", wrapText(m.footer(step), max(20, min(64, m.cap.Width-6))))
	return m.styles.Box(b.String(), 68)
}
func (m WizardModel) renderOption(b *strings.Builder, index int, selected bool, title, description, label string) {
	current := ""
	if selected {
		current = m.styles.RenderSelected(m.icons.Selected + " Current")
	}
	if label != "" {
		if current != "" {
			current += "   "
		}
		current += m.styles.RenderMuted(label)
	}
	renderedTitle := title
	if index == m.cursor {
		renderedTitle = m.styles.RenderFocused(title)
	}
	fmt.Fprintf(b, "%s %s", m.focusMark(index), renderedTitle)
	if current != "" {
		fmt.Fprintf(b, "   %s", current)
	}
	b.WriteByte('\n')
	if description != "" {
		fmt.Fprintf(b, "  %s\n", m.styles.RenderMuted(wrapText(description, max(18, min(62, m.cap.Width-8)))))
	}
	b.WriteByte('\n')
}
func (m WizardModel) renderTextField(b *strings.Builder, label, value, description string, focused bool) {
	rail, cursor := "│", ""
	if !m.cap.Unicode {
		rail = "|"
	}
	renderedLabel := label
	if focused {
		rail = m.styles.RenderFocused(m.icons.Focus)
		cursor = "█"
		if !m.cap.Unicode {
			cursor = "_"
		}
		renderedLabel = m.styles.RenderFocused(label)
	}
	fmt.Fprintf(b, "%s\n%s %s%s\n  %s\n\n", renderedLabel, rail, value, cursor, m.styles.RenderMuted(description))
}

func (m WizardModel) modelWindow() (int, int) {
	visible := max(3, min(7, m.height-14))
	if visible > len(m.models) {
		visible = len(m.models)
	}
	start := m.cursor - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > len(m.models) {
		start = len(m.models) - visible
	}
	if start < 0 {
		start = 0
	}
	return start, start + visible
}
func (m WizardModel) focusMark(index int) string {
	if index == m.cursor {
		return m.styles.RenderFocused(m.icons.Focus)
	}
	return " "
}
func (m WizardModel) renderValidation(b *strings.Builder) {
	name := providerDisplayName(m.provider)
	switch {
	case m.validationRunning:
		fmt.Fprintf(b, "Validating %s credentials...\n", name)
	case m.validationDone && m.validation.Valid:
		fmt.Fprintf(b, "%s Connected to %s\n", m.icons.Success, name)
	case m.validationDone:
		switch m.validation.Kind {
		case ValidationAuthFailure:
			fmt.Fprintf(b, "%s Invalid API key.\n\nR Retry   E Edit key\n", m.icons.Error)
		case ValidationRateLimit:
			fmt.Fprintf(b, "%s %s rate limit reached\n  The key could not be fully validated right now.\n\nR Retry   C Continue with built-in model list   E Edit key\n", m.icons.Warning, name)
		default:
			fmt.Fprintf(b, "%s Could not reach %s\n  The key could not be validated right now.\n\nR Retry   C Continue with built-in model list   E Edit key\n", m.icons.Warning, name)
		}
	default:
		b.WriteString("Press Enter to validate.\n")
	}
}
func (m WizardModel) renderShell(b *strings.Builder) {
	if m.detectedShell == completion.Unknown {
		b.WriteString("Detected\nUnknown shell\n\nSelect your shell:\n\n")
		for i, s := range []string{"bash", "zsh", "fish", "PowerShell", "Do not install"} {
			m.renderOption(b, i, false, s, "", "")
		}
		return
	}
	fmt.Fprintf(b, "Detected\n%s\n\nInstall Lookup completion?\n\n", m.detectedShell)
	m.renderOption(b, 0, m.installCompletion, "Yes", "", "Recommended")
	m.renderOption(b, 1, !m.installCompletion, "No", "", "")
}
func (m WizardModel) footer(step Step) string {
	switch step {
	case StepAPIKey:
		return "Enter Validate   Esc Back   Ctrl+C Quit"
	case StepOllama:
		return "Tab Next field   Enter Continue   Esc Back   Ctrl+C Quit"
	case StepSkipDirs:
		return "↑↓ Navigate   Space Toggle   Enter Continue   Esc Back"
	case StepValidate:
		if m.validationDone && !m.validation.Valid {
			if m.validation.Kind == ValidationAuthFailure {
				return "R Retry   E Edit key   Esc Back"
			}
			return "R Retry   C Use built-in models   E Edit key   Esc Back"
		}
		return "Enter Validate   Esc Back   Q Quit"
	case StepReview:
		return "Enter Save   Esc Back   Q Quit"
	default:
		return "↑↓ Navigate   Enter Select   Esc Back   Q Quit"
	}
}
func (m WizardModel) successView() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", m.styles.RenderSuccess(m.icons.Success+" Lookup is ready"))
	contentWidth := max(18, min(54, m.cap.Width-8))
	if dir, e := config.ConfigDir(); e == nil {
		fmt.Fprintf(&b, "Configuration\n%s\n\n", m.styles.RenderMuted(wrapPath(filepath.Join(dir, "config.toml"), contentWidth)))
	}
	if m.outputFmt == "markdown" {
		fmt.Fprintf(&b, "Default output\n%s\n\n", m.styles.RenderMuted("./lookup-report.md"))
	}
	if m.completionWarning != nil {
		fmt.Fprintf(&b, "%s %s completion could not be installed\n  Run manually:\n  lookup completion %s\n\n", m.icons.Warning, m.detectedShell, m.detectedShell)
	}
	fmt.Fprintf(&b, "Next\n  %s\n", m.styles.RenderStrong("lookup ."))
	return m.styles.Card(b.String(), 60) + "\n\n" + m.styles.RenderMuted("Press any key to close")
}
func providerDisplayName(id string) string {
	for _, p := range providerChoices {
		if p.ID == id {
			return p.Name
		}
	}
	return id
}
func containsDir(dirs []string, want string) bool {
	for _, d := range dirs {
		if d == want {
			return true
		}
	}
	return false
}

func containsModel(models []ModelInfo, want string) bool {
	for _, model := range models {
		if model.ID == want {
			return true
		}
	}
	return false
}

func wrapPath(value string, width int) string {
	runes := []rune(value)
	if width <= 0 || len(runes) <= width {
		return value
	}
	var b strings.Builder
	for len(runes) > width {
		b.WriteString(string(runes[:width]))
		b.WriteByte('\n')
		runes = runes[width:]
	}
	b.WriteString(string(runes))
	return b.String()
}

func wrapText(value string, width int) string {
	if width <= 0 {
		return value
	}
	words := strings.Fields(value)
	if len(words) == 0 {
		return ""
	}
	var b strings.Builder
	line := 0
	for _, word := range words {
		wordWidth := len([]rune(word))
		if line > 0 && line+1+wordWidth > width {
			b.WriteByte('\n')
			line = 0
		}
		if line > 0 {
			b.WriteByte(' ')
			line++
		}
		b.WriteString(word)
		line += wordWidth
	}
	return b.String()
}
