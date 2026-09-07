package setup

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/poizdev/lookup/internal/completion"
	"github.com/poizdev/lookup/internal/ui"
)

func keyRunes(value string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}
}

func updateWizard(t *testing.T, model WizardModel, msg tea.Msg) WizardModel {
	t.Helper()
	updated, _ := model.Update(msg)
	got, ok := updated.(WizardModel)
	if !ok {
		t.Fatalf("Update returned %T", updated)
	}
	return got
}

func newFirstRunWizard(t *testing.T) WizardModel {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	return NewWizardModel()
}

func TestAPIKeyInputAcceptsPasteAndTreatsNavigationLettersAsText(t *testing.T) {
	m := newFirstRunWizard(t)
	m.provider = "openai"
	m.currentStep = 1
	m = updateWizard(t, m, keyRunes("sk-long-qjk-ş-paste"))
	if m.apiKey != "sk-long-qjk-ş-paste" || m.cancelled || m.cursor != 0 {
		t.Fatalf("paste produced key=%q cancelled=%v cursor=%d", m.apiKey, m.cancelled, m.cursor)
	}
}

func TestAPIKeyBackspaceRemovesOneUnicodeRune(t *testing.T) {
	m := newFirstRunWizard(t)
	m.provider, m.apiKey, m.currentStep = "openai", "abcş", 1
	m = updateWizard(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	if m.apiKey != "abc" {
		t.Fatalf("backspace key=%q, want abc", m.apiKey)
	}
}

func TestAPIKeyEmptyEnterDoesNotAdvance(t *testing.T) {
	m := newFirstRunWizard(t)
	m.provider, m.currentStep = "openai", 1
	m = updateWizard(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.currentStep != 1 {
		t.Fatalf("empty key advanced to step %d", m.currentStep)
	}
}

func TestAPIKeyViewIsEmptyUntilInputAndNeverContainsPlaintext(t *testing.T) {
	m := newFirstRunWizard(t)
	m.provider, m.currentStep = "openai", 1
	if view := m.View(); strings.Contains(view, "••") || !strings.Contains(view, "paste or type your key") {
		t.Fatalf("empty API key view gives false input feedback:\n%s", view)
	}
	m.apiKey = "never-render-this-secret"
	view := m.View()
	if strings.Contains(view, m.apiKey) || !strings.Contains(view, "24 characters entered") {
		t.Fatalf("secret view leaked or omitted count:\n%s", view)
	}
}

func TestOptionQQuitsButTextQDoesNot(t *testing.T) {
	m := newFirstRunWizard(t)
	m = updateWizard(t, m, keyRunes("q"))
	if !m.cancelled {
		t.Fatal("q did not quit option screen")
	}
	m = newFirstRunWizard(t)
	m.provider, m.currentStep = "openai", 1
	m = updateWizard(t, m, keyRunes("q"))
	if m.cancelled || m.apiKey != "q" {
		t.Fatalf("q in text field cancelled=%v key=%q", m.cancelled, m.apiKey)
	}
}

func TestFirstRunDefaultsMarkdownAndExistingConfigRestoresCursors(t *testing.T) {
	m := newFirstRunWizard(t)
	if m.outputFmt != "markdown" {
		t.Fatalf("first-run output=%q, want markdown", m.outputFmt)
	}

	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	if err := os.MkdirAll(base+"/lookup", 0o700); err != nil {
		t.Fatal(err)
	}
	configText := "[ai]\nprovider='gemini'\nmodel='gemini-2.5-flash'\n[ai.gemini]\napi_key='gemini-secret'\n[output]\nformat='terminal'\n[analysis]\nskip_dirs=['vendor']\n"
	if err := os.WriteFile(base+"/lookup/config.toml", []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}
	m = NewWizardModel()
	if m.provider != "gemini" || m.cursor != 2 || m.outputFmt != "terminal" {
		t.Fatalf("restored provider=%q cursor=%d output=%q", m.provider, m.cursor, m.outputFmt)
	}
	m.currentStep = 4 // Output in the cloud-provider step sequence.
	m.cursor = 0
	m = updateWizard(t, m, tea.KeyMsg{Type: tea.KeyEsc})   // Model.
	m = updateWizard(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // Output again.
	if m.currentStepType() != StepOutput || m.cursor != 1 {
		t.Fatalf("output restore step=%v cursor=%d", m.currentStepType(), m.cursor)
	}
}

func TestProviderSwitchIsolatesSecretsAndInvalidatesDependentState(t *testing.T) {
	m := newFirstRunWizard(t)
	m.provider, m.apiKey = "openai", "OPENAI_KEY"
	m.providerKeys["openai"] = m.apiKey
	m.validationDone, m.validationRunning = true, true
	m.validation = ValidationResult{Valid: true}
	m.models, m.model = []ModelInfo{{ID: "gpt-old", DisplayName: "gpt-old"}}, "gpt-old"
	m.cursor = 2
	updated, _ := m.selectCurrent(StepProvider)
	m = updated.(WizardModel)
	if m.provider != "gemini" || m.apiKey != "" || m.validationDone || m.validationRunning || len(m.models) != 0 || m.model != "" {
		t.Fatalf("provider switch retained stale state: provider=%q key=%q validation=%+v running=%v models=%v model=%q", m.provider, m.apiKey, m.validation, m.validationRunning, m.models, m.model)
	}
}

func TestLateAsyncResponsesForPreviousProviderAreIgnored(t *testing.T) {
	m := newFirstRunWizard(t)
	m.provider, m.validationGeneration, m.modelGeneration = "gemini", 4, 7
	m = updateWizard(t, m, validationMsg{provider: "openai", generation: 3, result: ValidationResult{Valid: true}})
	m = updateWizard(t, m, modelsMsg{provider: "openai", generation: 7, models: []ModelInfo{{ID: "gpt-stale"}}})
	if m.validationDone || len(m.models) != 0 || m.model != "" {
		t.Fatalf("stale response mutated state: validation=%+v models=%v model=%q", m.validation, m.models, m.model)
	}
}

func TestValidationRequiresExplicitFallbackAndBlocksAuthBypass(t *testing.T) {
	for _, test := range []struct {
		name    string
		kind    ValidationErrorKind
		advance bool
	}{{"auth", ValidationAuthFailure, false}, {"rate limit", ValidationRateLimit, true}, {"network", ValidationNetworkError, true}} {
		t.Run(test.name, func(t *testing.T) {
			m := newFirstRunWizard(t)
			m.provider, m.currentStep, m.validationDone = "openai", 2, true
			m.validation = ValidationResult{Kind: test.kind, Error: errors.New("safe failure")}
			m = updateWizard(t, m, keyRunes("c"))
			if (m.currentStepType() == StepModel) != test.advance {
				t.Fatalf("kind %v model step=%v, want advance=%v", test.kind, m.currentStepType(), test.advance)
			}
		})
	}
}

func TestDuplicateValidationIsSuppressed(t *testing.T) {
	m := newFirstRunWizard(t)
	m.provider, m.apiKey, m.currentStep, m.validationRunning = "openai", "secret", 2, true
	updated, cmd := m.selectCurrent(StepValidate)
	if cmd != nil || !updated.(WizardModel).validationRunning {
		t.Fatal("duplicate validation command was started")
	}
}

func TestEscFromRunningValidationIgnoresLateResult(t *testing.T) {
	m := newFirstRunWizard(t)
	m.provider, m.apiKey, m.currentStep = "openai", "secret", 2
	m.validationRunning, m.validationGeneration = true, 5
	m = updateWizard(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = updateWizard(t, m, validationMsg{provider: "openai", generation: 5, result: ValidationResult{Valid: true}})
	if m.currentStepType() != StepAPIKey || m.validationDone {
		t.Fatalf("late validation hijacked back navigation: step=%v done=%v", m.currentStepType(), m.validationDone)
	}
}

func TestModelResponsePreservesConfiguredModelAndWarnsOnFallback(t *testing.T) {
	m := newFirstRunWizard(t)
	m.provider, m.model, m.currentStep, m.modelGeneration = "gemini", "gemini-2.5-flash", 3, 2
	m = updateWizard(t, m, modelsMsg{provider: "gemini", generation: 2, models: []ModelInfo{{ID: "gemini-2.5-pro", DisplayName: "pro"}, {ID: "gemini-2.5-flash", DisplayName: "flash"}}})
	if m.cursor != 1 || m.model != "gemini-2.5-flash" {
		t.Fatalf("configured model cursor=%d model=%q", m.cursor, m.model)
	}
	m = updateWizard(t, m, modelsMsg{provider: "gemini", generation: 2, err: errors.New("offline")})
	if m.modelWarning == nil || len(m.models) == 0 || !strings.Contains(m.View(), "built-in model list") {
		t.Fatalf("fallback warning/models missing: warning=%v models=%v", m.modelWarning, m.models)
	}
}

func TestSkipDirectoryFocusAndSelectionAreIndependent(t *testing.T) {
	m := newFirstRunWizard(t)
	m.provider, m.currentStep, m.cursor = "ollama", 3, 1
	before := containsDir(m.skipDirs, defaultSkipDirs[0])
	m = updateWizard(t, m, tea.KeyMsg{Type: tea.KeySpace})
	if containsDir(m.skipDirs, defaultSkipDirs[0]) != before || containsDir(m.skipDirs, defaultSkipDirs[1]) {
		t.Fatal("space toggled a non-focused item or failed to toggle focused item")
	}
	view := m.View()
	if !strings.Contains(view, "> [ ] node_modules") && !strings.Contains(view, "┃ [ ] node_modules") {
		t.Fatalf("focus and checkbox are not both visible:\n%s", view)
	}
}

func TestSavedStateRendersSuccessAndCompletionRecoveryBeforeQuit(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := NewWizardModel(func(completion.Shell) error { return errors.New("denied") })
	m.provider, m.model, m.currentStep = "ollama", "llama3.1", 5
	m.outputFmt, m.detectedShell, m.installCompletion = "markdown", completion.Zsh, true
	updated, cmd := m.selectCurrent(StepReview)
	m = updated.(WizardModel)
	if cmd != nil || !m.saved {
		t.Fatalf("save returned immediate quit=%v saved=%v", cmd != nil, m.saved)
	}
	view := m.View()
	for _, want := range []string{"Lookup is ready", "lookup-report.md", "lookup completion zsh", "lookup ."} {
		if !strings.Contains(view, want) {
			t.Errorf("success view missing %q:\n%s", want, view)
		}
	}
}

func TestNoColorASCIIViewKeepsFocusAndSelectionSemantic(t *testing.T) {
	m := newFirstRunWizard(t)
	m.cap = ui.Capability{Width: 80}
	m.styles = ui.NewStyles(ui.NewTheme(m.cap), m.cap)
	m.icons = ui.Icons(false)
	view := m.View()
	if strings.Contains(view, "\x1b[") || !strings.Contains(view, "> OpenAI") || !strings.Contains(view, "[x] Current") {
		t.Fatalf("ASCII/no-color state is unclear:\n%s", view)
	}
}

func TestOutputViewExplainsChoicesAndMarkdownPath(t *testing.T) {
	m := newFirstRunWizard(t)
	m.provider, m.currentStep = "ollama", 2
	m.resolveCursor()
	view := m.View()
	for _, want := range []string{"Markdown", "RECOMMENDED", "./lookup-report.md", "Terminal", "directly in the terminal", "JSON", "automation"} {
		if !strings.Contains(view, want) {
			t.Errorf("output view missing %q:\n%s", want, view)
		}
	}
}

func TestNarrowViewWrapsLongDescriptionsAndFooter(t *testing.T) {
	m := newFirstRunWizard(t)
	m.provider, m.currentStep = "ollama", 2
	m.cap = ui.Capability{Width: 36}
	m.styles = ui.NewStyles(ui.NewTheme(m.cap), m.cap)
	m.icons = ui.Icons(false)
	view := m.View()
	if strings.Contains(view, "Full analysis report in ./lookup-report.md") || strings.Contains(view, "Navigate   Enter Select   Esc Back") {
		t.Fatalf("narrow view did not wrap long copy:\n%s", view)
	}
}

func TestOutputStepStartsFocusedOnCurrentSelection(t *testing.T) {
	for _, test := range []struct {
		name, configOutput string
		wantCursor         int
	}{{"fresh Markdown", "", 0}, {"existing Markdown", "markdown", 0}, {"existing Terminal", "terminal", 1}} {
		t.Run(test.name, func(t *testing.T) {
			base := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", base)
			if test.configOutput != "" {
				if err := os.MkdirAll(base+"/lookup", 0o700); err != nil {
					t.Fatal(err)
				}
				text := "[ai]\nprovider='ollama'\n[output]\nformat='" + test.configOutput + "'\n"
				if err := os.WriteFile(base+"/lookup/config.toml", []byte(text), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			m := NewWizardModel()
			m.provider, m.currentStep = "ollama", 2
			m.resolveCursor()
			if m.cursor != test.wantCursor {
				t.Fatalf("cursor=%d, want %d", m.cursor, test.wantCursor)
			}
			lines := strings.Split(m.View(), "\n")
			found := false
			for _, line := range lines {
				if strings.Contains(line, "Current") && strings.Contains(line, map[int]string{0: "Markdown", 1: "Terminal"}[test.wantCursor]) && (strings.Contains(line, "┃") || strings.Contains(line, ">")) {
					found = true
				}
			}
			if !found {
				t.Fatalf("current output is not focused:\n%s", m.View())
			}
		})
	}
}

func TestAccentIsLimitedToFocusedMarkerAndTitle(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	m := newFirstRunWizard(t)
	m.cap = ui.Capability{Color: true, Unicode: true, Width: 80, Height: 24}
	m.styles = ui.NewStyles(ui.NewTheme(m.cap), m.cap)
	m.icons = ui.Icons(true)
	view := m.View()
	if !strings.Contains(view, m.styles.RenderFocused(m.icons.Focus)) {
		t.Fatalf("focused marker is not accented:\n%s", view)
	}
	if strings.Contains(view, m.styles.RenderFocused("LOOKUP")) {
		t.Fatalf("header incorrectly uses focused accent styling:\n%s", view)
	}
}

func TestOllamaFieldsLookEditableAndTabMovesVisibleFocus(t *testing.T) {
	m := newFirstRunWizard(t)
	m.provider, m.currentStep, m.cursor = "ollama", 1, 0
	urlView := m.View()
	if !strings.Contains(urlView, "http://localhost:11434") || !strings.Contains(urlView, "Ollama server endpoint") || (!strings.Contains(urlView, "█") && !strings.Contains(urlView, "_")) {
		t.Fatalf("URL field lacks editable focus affordance:\n%s", urlView)
	}
	m = updateWizard(t, m, tea.KeyMsg{Type: tea.KeyTab})
	modelView := m.View()
	if m.cursor != 1 || (!strings.Contains(modelView, "llama3.1█") && !strings.Contains(modelView, "llama3.1_")) || !strings.Contains(modelView, "Local model name") || modelView == urlView {
		t.Fatalf("Tab did not visibly focus model field:\n%s", modelView)
	}
	m = updateWizard(t, m, keyRunes("-qjk-未来"))
	if m.ollamaModel != "llama3.1-qjk-未来" {
		t.Fatalf("Ollama paste=%q", m.ollamaModel)
	}
}

func TestInvalidConfiguredModelFallsBackToFirstAvailableModel(t *testing.T) {
	m := newFirstRunWizard(t)
	m.provider, m.model, m.currentStep, m.modelGeneration = "gemini", "gemini-embedding-001", 3, 9
	m = updateWizard(t, m, modelsMsg{provider: "gemini", generation: 9, models: []ModelInfo{{ID: "gemini-2.5-flash", DisplayName: "gemini-2.5-flash"}, {ID: "gemini-2.5-pro", DisplayName: "gemini-2.5-pro"}}})
	if m.model != "gemini-2.5-flash" || m.cursor != 0 {
		t.Fatalf("invalid configured model remained selected: model=%q cursor=%d", m.model, m.cursor)
	}
}

func TestModelViewUsesBoundedWindowAndKeepsCurrentVisible(t *testing.T) {
	m := newFirstRunWizard(t)
	m.provider, m.currentStep, m.cursor = "gemini", 3, 8
	for i := 0; i < 14; i++ {
		id := fmt.Sprintf("gemini-model-%02d", i)
		m.models = append(m.models, ModelInfo{ID: id, DisplayName: id})
	}
	m.model = m.models[m.cursor].ID
	view := m.View()
	if !strings.Contains(view, "9 of 14") || !strings.Contains(view, m.model) || strings.Contains(view, "gemini-model-00") {
		t.Fatalf("model viewport is not bounded/current-aware:\n%s", view)
	}
}

func TestWindowSizeMsgStoresDimensionsAndRecalculatesModelWindow(t *testing.T) {
	m := newFirstRunWizard(t)
	m.provider, m.currentStep, m.cursor = "gemini", 3, 8
	for i := 0; i < 14; i++ {
		id := fmt.Sprintf("gemini-model-%02d", i)
		m.models = append(m.models, ModelInfo{ID: id, DisplayName: id})
	}
	m.model = m.models[m.cursor].ID

	m = updateWizard(t, m, tea.WindowSizeMsg{Width: 60, Height: 18})
	if m.width != 60 || m.height != 18 || m.cap.Width != 60 || m.cap.Height != 18 {
		t.Fatalf("WindowSizeMsg dimensions not stored: %+v", m.cap)
	}
	small := m.View()
	if !strings.Contains(small, m.model) || strings.Count(small, "gemini-model-") != 4 {
		t.Fatalf("small viewport incorrect:\n%s", small)
	}

	m = updateWizard(t, m, tea.WindowSizeMsg{Width: 90, Height: 32})
	large := m.View()
	if m.width != 90 || m.height != 32 || m.cap.Width != 90 || m.cap.Height != 32 || strings.Count(large, "gemini-model-") != 7 {
		t.Fatalf("large viewport not recalculated: %+v\n%s", m.cap, large)
	}
}

func TestModelFocusMovementScrollsWindowAndTinyTerminalIsSafe(t *testing.T) {
	m := newFirstRunWizard(t)
	m.provider, m.currentStep = "gemini", 3
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("gemini-model-%02d", i)
		m.models = append(m.models, ModelInfo{ID: id, DisplayName: id})
	}
	m.model = m.models[0].ID
	m = updateWizard(t, m, tea.WindowSizeMsg{Width: 20, Height: 1})
	for i := 0; i < 8; i++ {
		m = updateWizard(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}
	view := m.View()
	if m.cursor != 8 || !strings.Contains(view, "gemini-model-08") || strings.Contains(view, "gemini-model-00") || strings.Count(view, "gemini-model-") != 3 {
		t.Fatalf("tiny scrolling viewport incorrect:\n%s", view)
	}
}

func TestSuccessScreenIsBoundedSemanticCardWithASCIIFallback(t *testing.T) {
	m := newFirstRunWizard(t)
	m.saved, m.outputFmt = true, "markdown"
	m.cap = ui.Capability{Width: 44}
	m.styles = ui.NewStyles(ui.NewTheme(m.cap), m.cap)
	m.icons = ui.Icons(false)
	view := m.View()
	for _, want := range []string{"[OK] Lookup is ready", "Configuration", "lookup-report.md", "lookup .", "+"} {
		if !strings.Contains(view, want) {
			t.Errorf("success card missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "\x1b[") {
		t.Fatalf("NO_COLOR card contains ANSI:\n%q", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if len([]rune(line)) > 44 {
			t.Fatalf("success card overflows width: %q", line)
		}
	}
}

func TestSuccessNextCommandDoesNotReuseFocusAccent(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	m := newFirstRunWizard(t)
	m.saved, m.outputFmt = true, "markdown"
	m.cap = ui.Capability{Color: true, Unicode: true, Width: 80, Height: 24}
	m.styles = ui.NewStyles(ui.NewTheme(m.cap), m.cap)
	m.icons = ui.Icons(true)
	view := m.View()
	if strings.Contains(view, m.styles.RenderFocused("lookup .")) {
		t.Fatalf("success command uses focus accent:\n%s", view)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func TestWizardNetworkCommandsUseCallerContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	validationSawCancellation := false
	oldValidationClient := validationHTTPClient
	validationHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		validationSawCancellation = errors.Is(req.Context().Err(), context.Canceled)
		return nil, req.Context().Err()
	})}
	t.Cleanup(func() { validationHTTPClient = oldValidationClient })
	_ = validateCmd(ctx, "openai", "secret")()
	if !validationSawCancellation {
		t.Fatal("API-key validation did not receive the wizard context cancellation")
	}

	modelSawCancellation := false
	oldModelClient := modelHTTPClient
	modelHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		modelSawCancellation = errors.Is(req.Context().Err(), context.Canceled)
		return nil, req.Context().Err()
	})}
	t.Cleanup(func() { modelHTTPClient = oldModelClient })
	_ = fetchModelsCmd(ctx, "openai", "secret")()
	if !modelSawCancellation {
		t.Fatal("model fetch did not receive the wizard context cancellation")
	}
}

func TestStepSequencesDependOnProvider(t *testing.T) {
	tests := []struct {
		provider string
		want     []Step
	}{
		{"openai", []Step{StepProvider, StepAPIKey, StepValidate, StepModel, StepOutput, StepSkipDirs, StepShellIntegration, StepReview}},
		{"ollama", []Step{StepProvider, StepOllama, StepOutput, StepSkipDirs, StepShellIntegration, StepReview}},
	}
	for _, tt := range tests {
		m := NewWizardModel()
		m.provider = tt.provider
		got := m.activeSteps()
		if len(got) != len(tt.want) {
			t.Fatalf("%s steps = %v, want %v", tt.provider, got, tt.want)
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("%s step %d = %v, want %v", tt.provider, i, got[i], tt.want[i])
			}
		}
	}
}

func TestMaskKeyDoesNotExposeSecret(t *testing.T) {
	got := maskKey("sk-proj-abc123xyz")
	if got == "sk-proj-abc123xyz" || got[:4] != "sk-p" || got[len(got)-3:] != "xyz" {
		t.Fatalf("maskKey() = %q", got)
	}
}

func TestOllamaStepEditsURLBeforeModel(t *testing.T) {
	m := NewWizardModel()
	m.provider = "ollama"
	m.currentStep = 1
	m.cursor = 0
	m.ollamaURL = "http://host"
	updated, _ := m.updateOllama("backspace")
	got := updated.(WizardModel)
	if got.ollamaURL != "http://hos" || got.currentStep != 1 {
		t.Fatalf("URL edit produced URL=%q step=%d", got.ollamaURL, got.currentStep)
	}
	updated, _ = got.updateOllama("enter")
	got = updated.(WizardModel)
	if got.cursor != 1 || got.currentStep != 1 {
		t.Fatalf("first Enter should focus model, cursor=%d step=%d", got.cursor, got.currentStep)
	}
}

func TestShellIntegrationSelectionHandlesDetectionAndUnknownShell(t *testing.T) {
	m := NewWizardModel()
	m.detectedShell = completion.Zsh
	m.cursor = 1
	updated, _ := m.selectCurrent(StepShellIntegration)
	if got := updated.(WizardModel); got.installCompletion {
		t.Fatal("No selection installed completion")
	}

	m = NewWizardModel()
	m.detectedShell = completion.Unknown
	m.cursor = 2
	updated, _ = m.selectCurrent(StepShellIntegration)
	got := updated.(WizardModel)
	if !got.installCompletion || got.detectedShell != completion.Fish {
		t.Fatalf("unknown-shell selection = %q install=%v", got.detectedShell, got.installCompletion)
	}
}

func TestCompletionFailureDoesNotDiscardSavedConfiguration(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := NewWizardModel(func(completion.Shell) error { return errors.New("destination denied") })
	m.provider = "ollama"
	m.installCompletion = true
	m.detectedShell = completion.Fish
	updated, _ := m.selectCurrent(StepReview)
	got := updated.(WizardModel)
	if got.err != nil || !got.saved || got.completionWarning == nil {
		t.Fatalf("save error=%v saved=%v completion warning=%v", got.err, got.saved, got.completionWarning)
	}
}
