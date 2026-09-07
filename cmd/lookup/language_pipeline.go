package main

import (
	"context"
	"os"

	"github.com/poizdev/lookup/internal/discovery"
	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language"
)

type languagePipelineResult struct {
	ParsedCount      int
	Collector        *language.CoverageCollector
	semanticEligible []language.FileID
}

func processLanguageFiles(
	ctx context.Context,
	files []discovery.DiscoveredFile,
	registry *language.Registry,
	builder *graph.Builder,
	warn func(string, error),
	increment func(),
) languagePipelineResult {
	result := languagePipelineResult{Collector: language.NewCoverageCollector()}
	for _, file := range files {
		func() {
			adapter := registry.GetByName(file.Language)
			capabilities := language.Capabilities{}
			if adapter != nil {
				capabilities = adapter.Capabilities()
			}
			fileID := result.Collector.Discover(file.Language, capabilities)
			if adapter == nil {
				return
			}
			src, err := os.ReadFile(file.Path)
			if err != nil {
				callWarning(warn, file.Path, err)
				return
			}
			if err := result.Collector.Attempt(fileID, language.StageParse); err != nil {
				callWarning(warn, file.Path, err)
				return
			}
			ast, err := adapter.Parse(ctx, src)
			if ast != nil {
				defer func() {
					if closeErr := ast.Close(); closeErr != nil {
						callWarning(warn, file.Path, closeErr)
					}
				}()
			}
			if err != nil || ast == nil || ast.HasErrors {
				if err == nil {
					err = errInvalidSyntaxTree
				}
				callWarning(warn, file.Path, err)
				return
			}
			if err := result.Collector.Succeed(fileID, language.StageParse); err != nil {
				callWarning(warn, file.Path, err)
				return
			}
			ast.FilePath = file.Path
			builder.AddFile(file.Path)
			if capabilities.Structural {
				if err := result.Collector.Attempt(fileID, language.StageStructural); err != nil {
					callWarning(warn, file.Path, err)
					return
				}
				builder.AddSymbols(adapter.ExtractSymbols(ast))
				builder.AddFunctions(adapter.ExtractFunctions(ast))
				builder.AddTypes(adapter.ExtractTypes(ast))
				builder.AddImports(adapter.ExtractImports(ast))
				builder.AddCalls(adapter.ExtractCalls(ast))
				if err := result.Collector.Succeed(fileID, language.StageStructural); err != nil {
					callWarning(warn, file.Path, err)
					return
				}
			}
			if capabilities.Semantic {
				document, err := adapter.(language.SemanticAdapter).ExtractSemantic(ast)
				if err != nil {
					callWarning(warn, file.Path, err)
					return
				}
				builder.AddDocument(document)
				result.semanticEligible = append(result.semanticEligible, fileID)
			}
			result.ParsedCount++
		}()
		callIncrement(increment)
	}
	return result
}

func (r *languagePipelineResult) CompleteSemantic() error {
	for _, fileID := range r.semanticEligible {
		if err := r.Collector.Attempt(fileID, language.StageSemantic); err != nil {
			return err
		}
		if err := r.Collector.Succeed(fileID, language.StageSemantic); err != nil {
			return err
		}
	}
	return nil
}

type pipelineError string

func (e pipelineError) Error() string { return string(e) }

const errInvalidSyntaxTree pipelineError = "parser returned a syntax tree containing errors"

func callWarning(warn func(string, error), path string, err error) {
	if warn != nil {
		warn(path, err)
	}
}

func callIncrement(increment func()) {
	if increment != nil {
		increment()
	}
}
