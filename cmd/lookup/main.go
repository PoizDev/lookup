package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/poizdev/lookup/internal/ai"
	"github.com/poizdev/lookup/internal/analyzer"
	"github.com/poizdev/lookup/internal/analyzer/generic/architecture"
	"github.com/poizdev/lookup/internal/analyzer/generic/security"
	"github.com/poizdev/lookup/internal/apperror"
	"github.com/poizdev/lookup/internal/completion"
	"github.com/poizdev/lookup/internal/config"
	"github.com/poizdev/lookup/internal/discovery"
	"github.com/poizdev/lookup/internal/finding"
	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/logger"
	"github.com/poizdev/lookup/internal/progress"
	"github.com/poizdev/lookup/internal/reporter"
	"github.com/poizdev/lookup/internal/reviewcontext"
	"github.com/poizdev/lookup/internal/scoring"
	"github.com/poizdev/lookup/internal/setup"
	"github.com/poizdev/lookup/internal/ui"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	var flags config.CLIFlags
	var debug bool
	cap := ui.Detect(os.Stdout)

	rootCmd := &cobra.Command{
		Use:   "lookup [path]",
		Short: "Lookup — AI-powered codebase intelligence engine",
		Long: `Lookup is an AI-powered codebase intelligence engine that scans your repository,
builds a unified code graph, and provides actionable insights about security,
correctness, architecture, performance, and maintainability.`,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			scanStarted := time.Now()
			// Determine target path
			targetPath := "."
			if len(args) > 0 {
				targetPath = args[0]
			}

			absPath, err := filepath.Abs(targetPath)
			if err != nil {
				return apperror.Wrap(apperror.KindPathNotFound, "resolve target path", err)
			}

			// Verify target exists
			info, err := os.Stat(absPath)
			if err != nil {
				return pathError("inspect target path", err)
			}
			if !info.IsDir() {
				return apperror.New(apperror.KindPathNotFound, fmt.Sprintf("target path is not a directory: %s", absPath))
			}

			// Initialize logger
			logger.Init(debug)
			defer logger.Sync()

			// Load config
			flags.TargetPath = absPath
			cfg, err := config.Load(flags)
			if err != nil {
				return err
			}

			// Ensure config dir exists (creates default config.toml if needed)
			if err := config.EnsureConfigDir(); err != nil {
				return err
			}

			// Create spinner
			outputCap := ui.Detect(cmd.OutOrStdout())
			machineOutput := cfg.Output.Format == "sarif" || cfg.Output.Format == "json"
			spin := progress.New(cfg.Output.Quiet || machineOutput, outputCap)
			spin.Start()
			defer spin.Stop()

			// ── Discovery Phase ───────────────────────────────────────
			spin.SetPhase(progress.PhaseDiscovery, "")

			walker, err := discovery.NewWalker(absPath, cfg.Analysis.SkipDirs, cfg.Analysis.MaxFileKB, cfg.Analysis.IncludeTests)
			if err != nil {
				spin.Stop()
				return pathError("initialize repository discovery", err)
			}

			files, stats, err := walker.Walk()
			if err != nil {
				spin.Stop()
				return pathError("scan repository", err)
			}

			spin.SetPhase(progress.PhaseDiscovery, fmt.Sprintf("%d files", stats.TotalFiles))

			// ── Parse Phase ───────────────────────────────────────────
			spin.SetPhase(progress.PhaseParsing, "")
			spin.SetProgress(0, len(files))

			// Register language adapters
			registry, err := newLanguageRegistry()
			if err != nil {
				return err
			}

			// Build graph
			builder := graph.NewBuilder()
			pipeline := processLanguageFiles(cmd.Context(), files, registry, builder, func(path string, err error) {
				logger.Warn("language analysis stage failed", zap.String("file", path), zap.Error(err))
			}, spin.Increment)

			// ── Build Graph Phase ─────────────────────────────────────
			spin.SetPhase(progress.PhaseBuilding, "")
			codeGraph := builder.Build()
			graphStats := codeGraph.Stats()

			// ── Analysis Phase ────────────────────────────────────────
			spin.SetPhase(progress.PhaseAnalyzing, "")

			// Create analysis engine with generic rules.
			engine, err := analyzer.NewEngineChecked(
				&security.HardcodedSecret{RepositoryRoot: absPath},
				&architecture.LayerViolation{},
			)
			if err != nil {
				return fmt.Errorf("register generic analysis rules: %w", err)
			}

			// Add language-specific rules from all semantic adapters.
			if err := engine.AddRegistryLanguageRules(registry); err != nil {
				return err
			}

			// Run all rules against the graph.
			potentialFindings, err := engine.RunChecked(codeGraph)
			if err != nil {
				return err
			}
			if err := pipeline.CompleteSemantic(); err != nil {
				return fmt.Errorf("record semantic language coverage: %w", err)
			}

			staticFindings := normalizePotentials(potentialFindings)
			filteredFindings := staticFindings

			// ── AI Review Phase ──────────────────────────────────────
			var aiWarn error
			var aiStats ai.ReviewStats
			if cfg.AI.ReviewMode != "off" && !cfg.AI.NoAI {
				if hasContextualReviewWork(staticFindings) {
					spin.SetPhase(progress.PhaseAIReview, "")
				}
				contextBuilder := reviewcontext.NewBuilder(codeGraph, reviewcontext.DefaultLimits())
				filteredFindings, aiStats = reviewNormalizedFindings(cmd.Context(), cfg.AI, filteredFindings, nil,
					func(err error) {
						aiWarn = err
						logger.Warn("AI review skipped or degraded", zap.Error(err))
					},
					func(status ai.ReviewProgress) {
						spin.SetDetail(fmt.Sprintf("%d selected · %d cached", status.Selected, status.Cached))
						spin.SetProgress(status.Done, status.Total)
					}, contextBuilder.Build)
			} else {
				filteredFindings, aiStats = reviewNormalizedFindings(cmd.Context(), cfg.AI, filteredFindings, nil, nil, nil)
			}
			if err := cmd.Context().Err(); err != nil {
				return err
			}
			analysisResult := finalizeFindings(filteredFindings)
			staticRisk, verifiedHealth := scoring.CalculateScoped(analysisResult)
			finalFindings := make([]finding.Finding, len(analysisResult.FinalFindings))
			for index := range analysisResult.FinalFindings {
				finalFindings[index] = analysisResult.FinalFindings[index].Finding
			}
			canonicalFindingCount := len(analysisResult.Candidates)
			finalFindings, err = reporter.Filter(finalFindings, cfg.Analysis.Severity, cfg.Analysis.Category)
			if err != nil {
				return err
			}

			// Stop spinner before printing results
			spin.Stop()

			if aiWarn != nil && !cfg.Output.Quiet {
				fmt.Fprintf(os.Stderr, "%s %v\n\n", ui.Icons(outputCap.Unicode).Warning, aiWarn)
			}

			result := &reporter.Result{
				RepoPath:         absPath,
				ScannedAt:        time.Now(),
				Duration:         time.Since(scanStarted),
				ToolVersion:      version,
				Scores:           *staticRisk.Scores,
				ScoreScope:       scoring.ScopeStaticRisk,
				StaticRisk:       staticRisk,
				VerifiedHealth:   verifiedHealth,
				Findings:         finalFindings,
				Candidates:       analysisResult.Candidates,
				LanguageCoverage: pipeline.Collector.Coverage(),
				Stats: reporter.ScanStats{
					FilesFound: stats.TotalFiles, FilesParsed: pipeline.ParsedCount, FilesSkipped: stats.SkippedFiles,
					GraphNodes: graphStats.NodeCount, GraphEdges: graphStats.EdgeCount, RulesRun: engine.RuleCount(),
				},
			}
			result.AIReview = &aiStats
			result.ReviewPlan = aiStats.Plan
			if aiStats.Plan != nil {
				summary := ai.SummarizeReview(analysisResult.Candidates, *aiStats.Plan, aiStats)
				if err := summary.Validate(); err != nil {
					return fmt.Errorf("invalid review summary: %w", err)
				}
				result.ReviewSummary = &summary
			}
			presentation := derivePresentationSummary(presentationInput{
				AI: cfg.AI, CanonicalFindingCount: canonicalFindingCount, VisibleFindingCount: len(finalFindings),
				FiltersActive: strings.TrimSpace(cfg.Analysis.Severity) != "" || strings.TrimSpace(cfg.Analysis.Category) != "",
				ReviewSummary: result.ReviewSummary, AIReview: result.AIReview, SignalCount: len(analysisResult.Candidates),
				Format: cfg.Output.Format, ReportPath: cfg.Output.ReportPath, Quiet: cfg.Output.Quiet, Capability: outputCap,
			})

			return renderResultWithPresentation(cfg.Output, result, cmd.OutOrStdout(), outputCap, presentation)
		},
	}

	bindRootFlags(rootCmd, &flags, &debug)

	// Version command
	versionCmd := newVersionCommand(version, commit, date)

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(newCompletionCommand(rootCmd))
	rootCmd.AddCommand(defaultUpdateCommand(version))
	rootCmd.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Interactive setup wizard for Lookup configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return setup.RunWizard(cmd.Context(), func(shell completion.Shell) error {
				_, err := installShellCompletion(rootCmd, shell)
				return err
			})
		},
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	passiveUpdate := startPassiveUpdateCheck(ctx, os.Args[1:], version, os.Stderr)
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprint(os.Stderr, ui.RenderError(cap, err))
		os.Exit(1)
	}
	finishPassiveUpdateCheck(passiveUpdate, os.Stderr)
}

func pathError(message string, err error) error {
	if os.IsPermission(err) {
		return apperror.Wrap(apperror.KindFilesystemPermission, message, err)
	}
	if os.IsNotExist(err) {
		return apperror.Wrap(apperror.KindPathNotFound, message, err)
	}
	return fmt.Errorf("%s: %w", message, err)
}

func newVersionCommand(buildVersion, buildCommit, buildDate string) *cobra.Command {
	var short bool
	cmd := &cobra.Command{Use: "version", Short: "Show version information", Args: cobra.NoArgs, Run: func(cmd *cobra.Command, args []string) {
		if short {
			fmt.Fprintln(cmd.OutOrStdout(), buildVersion)
			return
		}
		fmt.Fprint(cmd.OutOrStdout(), ui.RenderVersion(ui.Detect(cmd.OutOrStdout()), buildVersion, buildCommit, buildDate))
	}}
	cmd.Flags().BoolVar(&short, "short", false, "Print only the version number")
	return cmd
}

func bindRootFlags(rootCmd *cobra.Command, flags *config.CLIFlags, debug *bool) {
	rootCmd.Flags().StringVar(&flags.AIReview, "ai-review", "", "AI triage mode: smart, all, or off (default: smart)")
	rootCmd.Flags().BoolVar(&flags.NoAI, "no-ai", false, "Alias for --ai-review=off")
	rootCmd.Flags().StringVar(&flags.Provider, "provider", "", "Override AI provider (openai, anthropic, gemini, ollama)")
	rootCmd.Flags().StringVar(&flags.Model, "model", "", "Override AI model")
	rootCmd.Flags().StringVar(&flags.Output, "output", "", "Output format (terminal, json, markdown, sarif)")
	rootCmd.Flags().StringVar(&flags.Severity, "severity", "", "Filter: show only this severity and above")
	rootCmd.Flags().StringVar(&flags.Category, "category", "", "Filter: show only this category")
	rootCmd.Flags().BoolVar(&flags.Quiet, "quiet", false, "Only show overall score, no details")
	rootCmd.Flags().BoolVar(&flags.IncludeTests, "include-tests", false, "Include Go _test.go files in analysis")
	rootCmd.Flags().BoolVar(&flags.Detailed, "detailed", false, "Expand every finding in human-oriented output")
	rootCmd.Flags().IntVar(&flags.MaxReviewedFindings, "ai-max-findings", 0, "Maximum findings reviewed by AI per scan")
	rootCmd.Flags().IntVar(&flags.MaxProviderRequests, "ai-max-requests", 0, "Maximum AI provider requests per scan")
	rootCmd.Flags().IntVar(&flags.MaxEstimatedInputTokens, "ai-max-input-tokens", 0, "Maximum estimated AI input tokens per scan")
	rootCmd.Flags().IntVar(&flags.MaxOutputTokens, "ai-max-output-tokens", 0, "Maximum planned AI output tokens per scan")
	rootCmd.Flags().BoolVar(debug, "debug", false, "Enable debug logging")
}
