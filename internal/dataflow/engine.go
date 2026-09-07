// Package dataflow implements bounded deterministic taint propagation over semantic facts.
package dataflow

import (
	"sort"
	"strings"

	"github.com/poizdev/lookup/internal/semantic"
)

const (
	ConfidenceDirect          = 0.95
	ConfidenceLocal           = 0.90
	ConfidenceInterprocedural = 0.80
)

type EvidenceStrength string

const (
	EvidenceStrong   EvidenceStrength = "strong"
	EvidenceModerate EvidenceStrength = "moderate"
)

type Limits struct {
	MaxSteps     int
	MaxCallDepth int
}

func DefaultLimits() Limits {
	return Limits{MaxSteps: 16, MaxCallDepth: 2}
}

type Trace struct {
	Source          semantic.Fact
	Propagation     []semantic.Fact
	Sink            semantic.Fact
	Confidence      float64
	Strength        EvidenceStrength
	Interprocedural bool
}

type taintPath struct {
	propagation     []semantic.Fact
	callDepth       int
	interprocedural bool
	last            semantic.Location
	killed          bool
}

type stateHistory map[string][]taintPath

type functionTarget struct {
	file       string
	name       string
	parameters []string
}

type functionCatalog struct {
	byKey  map[string]functionTarget
	byName map[string][]functionTarget
}

func Analyze(index *semantic.Index, limits Limits) []Trace {
	if index == nil || limits.MaxSteps < 0 || limits.MaxCallDepth < 0 {
		return nil
	}
	sources := index.Facts(semantic.FactSource)
	propagations := index.Facts(semantic.FactPropagation)
	calls := index.Facts(semantic.FactCall)
	sinks := index.Facts(semantic.FactSink)
	sanitizers := index.Facts(semantic.FactSanitizer)
	functions := functionIndex(index.Documents())
	returnDependencies := returnDependencyIndex(index, functions)

	var traces []Trace
	for _, source := range sources {
		states := make(stateHistory)
		for _, output := range source.Outputs {
			writeState(states, stateKey(source.Location.File, source.Function, output), taintPath{last: source.Location})
		}

		for pass := 0; pass <= limits.MaxSteps+limits.MaxCallDepth; pass++ {
			changed := false
			for _, propagation := range propagations {
				path, ok := inputPathAt(states, propagation.Location.File, propagation.Function, propagation.Inputs, propagation.Location, sanitizers)
				callee, localCall := functions.resolve(propagation.Location.File, propagation.Operation)
				if propagation.Metadata["external_import"] == "true" {
					localCall = false
				}
				if localCall {
					if limits.MaxCallDepth == 0 {
						continue
					}
					path, ok = returnedArgumentPath(states, propagation, callee.parameters, returnDependencies[functionKey(callee.file, callee.name)], sanitizers)
					if ok {
						if path.callDepth >= limits.MaxCallDepth {
							ok = false
						} else {
							path.interprocedural = true
							path.callDepth++
						}
					}
				}
				if !ok || len(path.propagation) >= limits.MaxSteps {
					if !ok && len(propagation.Inputs) == 0 && propagation.Metadata["conditional"] != "true" {
						for _, output := range propagation.Outputs {
							writeState(states, stateKey(propagation.Location.File, propagation.Function, output), taintPath{last: propagation.Location, killed: true})
						}
					}
					continue
				}
				next := clonePath(path)
				next.propagation = append(next.propagation, propagation)
				next.last = propagation.Location
				for _, output := range propagation.Outputs {
					key := stateKey(propagation.Location.File, propagation.Function, output)
					if writeState(states, key, next) {
						changed = true
					}
				}
			}
			if limits.MaxCallDepth > 0 {
				for _, call := range calls {
					path, ok := inputPathAt(states, call.Location.File, call.Function, call.Inputs, call.Location, sanitizers)
					if !ok || path.callDepth >= limits.MaxCallDepth {
						continue
					}
					callee, resolved := functions.resolve(call.Location.File, call.Operation)
					if call.Metadata["external_import"] == "true" {
						resolved = false
					}
					if !resolved || len(callee.parameters) == 0 {
						continue
					}
					next := clonePath(path)
					next.callDepth++
					next.interprocedural = true
					next.propagation = append(next.propagation, call)
					next.last = semantic.Location{File: callee.file}
					for parameterIndex, parameter := range callee.parameters {
						argumentInputs := positionalInputs(call, parameterIndex)
						if len(argumentInputs) == 0 {
							break
						}
						if _, tainted := inputPathAt(states, call.Location.File, call.Function, argumentInputs, call.Location, sanitizers); !tainted {
							continue
						}
						key := stateKey(callee.file, callee.name, parameter)
						if writeState(states, key, next) {
							changed = true
						}
					}
				}
			}
			if !changed {
				break
			}
		}

		for _, sink := range sinks {
			path, ok := inputPathAt(states, sink.Location.File, sink.Function, sink.Inputs, sink.Location, sanitizers)
			if !ok {
				continue
			}
			trace := Trace{Source: source, Propagation: append([]semantic.Fact(nil), path.propagation...), Sink: sink, Confidence: ConfidenceDirect, Strength: EvidenceStrong, Interprocedural: path.interprocedural}
			switch {
			case path.interprocedural:
				trace.Confidence = ConfidenceInterprocedural
				trace.Strength = EvidenceModerate
			case len(path.propagation) > 0:
				trace.Confidence = ConfidenceLocal
			}
			traces = append(traces, trace)
		}
	}

	sort.SliceStable(traces, func(i, j int) bool {
		left, right := traces[i], traces[j]
		if left.Sink.Location.File != right.Sink.Location.File {
			return left.Sink.Location.File < right.Sink.Location.File
		}
		if left.Sink.Location.StartLine != right.Sink.Location.StartLine {
			return left.Sink.Location.StartLine < right.Sink.Location.StartLine
		}
		if left.Source.Location.StartLine != right.Source.Location.StartLine {
			return left.Source.Location.StartLine < right.Source.Location.StartLine
		}
		return left.Source.ID < right.Source.ID
	})
	return traces
}

func returnDependencyIndex(index *semantic.Index, functions functionCatalog) map[string]map[int]bool {
	dependencies := make(map[string]map[int]bool)
	propagations := index.Facts(semantic.FactPropagation)
	returns := index.Facts(semantic.FactReturn)
	for key, target := range functions.byKey {
		symbolDependencies := make(map[string]map[int]bool)
		for parameterIndex, parameter := range target.parameters {
			symbolDependencies[parameter] = map[int]bool{parameterIndex: true}
		}
		for pass := 0; pass < len(propagations)+1; pass++ {
			changed := false
			for _, propagation := range propagations {
				if propagation.Location.File != target.file || propagation.Function != target.name {
					continue
				}
				for _, input := range propagation.Inputs {
					for parameterIndex := range symbolDependencies[input] {
						for _, output := range propagation.Outputs {
							if symbolDependencies[output] == nil {
								symbolDependencies[output] = make(map[int]bool)
							}
							if !symbolDependencies[output][parameterIndex] {
								symbolDependencies[output][parameterIndex] = true
								changed = true
							}
						}
					}
				}
			}
			if !changed {
				break
			}
		}
		for _, returned := range returns {
			if returned.Location.File != target.file || returned.Function != target.name {
				continue
			}
			for _, input := range returned.Inputs {
				for parameterIndex := range symbolDependencies[input] {
					if dependencies[key] == nil {
						dependencies[key] = make(map[int]bool)
					}
					dependencies[key][parameterIndex] = true
				}
			}
		}
	}
	return dependencies
}

func returnedArgumentPath(states stateHistory, propagation semantic.Fact, parameters []string, dependencies map[int]bool, sanitizers []semantic.Fact) (taintPath, bool) {
	for parameterIndex := range parameters {
		if !dependencies[parameterIndex] {
			continue
		}
		if path, ok := inputPathAt(states, propagation.Location.File, propagation.Function, positionalInputs(propagation, parameterIndex), propagation.Location, sanitizers); ok {
			return path, true
		}
	}
	return taintPath{}, false
}

func positionalInputs(fact semantic.Fact, argumentIndex int) []string {
	if argumentIndex < len(fact.ArgumentInputs) {
		return fact.ArgumentInputs[argumentIndex]
	}
	if argumentIndex < len(fact.Arguments) {
		return []string{strings.TrimSpace(fact.Arguments[argumentIndex])}
	}
	if argumentIndex < len(fact.Inputs) {
		return []string{fact.Inputs[argumentIndex]}
	}
	return nil
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func functionIndex(documents []*semantic.Document) functionCatalog {
	result := functionCatalog{byKey: make(map[string]functionTarget), byName: make(map[string][]functionTarget)}
	for _, document := range documents {
		if document == nil {
			continue
		}
		for _, function := range document.Functions {
			key := functionKey(document.Path, function.Name)
			if _, exists := result.byKey[key]; !exists {
				target := functionTarget{file: document.Path, name: function.Name, parameters: append([]string(nil), function.Parameters...)}
				result.byKey[key] = target
				baseName := lastSelector(function.Name)
				result.byName[baseName] = append(result.byName[baseName], target)
			}
		}
	}
	for name := range result.byName {
		sort.Slice(result.byName[name], func(i, j int) bool {
			left, right := result.byName[name][i], result.byName[name][j]
			if left.file != right.file {
				return left.file < right.file
			}
			return left.name < right.name
		})
	}
	return result
}

func (c functionCatalog) resolve(callerFile, operation string) (functionTarget, bool) {
	candidates := c.byName[lastSelector(operation)]
	if len(candidates) == 1 {
		return candidates[0], true
	}
	var local []functionTarget
	for _, candidate := range candidates {
		if candidate.file == callerFile {
			local = append(local, candidate)
		}
	}
	if len(local) == 1 {
		return local[0], true
	}
	return functionTarget{}, false
}

func inputPathAt(states stateHistory, file, function string, inputs []string, at semantic.Location, sanitizers []semantic.Fact) (taintPath, bool) {
	for _, input := range inputs {
		history := states[stateKey(file, function, input)]
		for index := len(history) - 1; index >= 0; index-- {
			path := history[index]
			if !locationAtOrBefore(path.last, at) {
				continue
			}
			if path.killed || sanitizedBetween(sanitizers, file, function, input, path.last, at) {
				break
			}
			return path, true
		}
	}
	return taintPath{}, false
}

func writeState(states stateHistory, key string, path taintPath) bool {
	history := states[key]
	for index := range history {
		if sameLocation(history[index].last, path.last) {
			if history[index].killed == path.killed && history[index].callDepth == path.callDepth && len(history[index].propagation) == len(path.propagation) {
				return false
			}
			history[index] = path
			states[key] = history
			return true
		}
	}
	states[key] = append(history, path)
	sort.SliceStable(states[key], func(i, j int) bool { return locationLess(states[key][i].last, states[key][j].last) })
	return true
}

func sameLocation(left, right semantic.Location) bool {
	return left.File == right.File && left.StartLine == right.StartLine && left.StartColumn == right.StartColumn
}

func locationLess(left, right semantic.Location) bool {
	if left.File != right.File {
		return left.File < right.File
	}
	if left.StartLine != right.StartLine {
		return left.StartLine < right.StartLine
	}
	return left.StartColumn < right.StartColumn
}

func sanitizedBetween(sanitizers []semantic.Fact, file, function, symbol string, after, before semantic.Location) bool {
	for _, sanitizer := range sanitizers {
		if sanitizer.Location.File != file || sanitizer.Function != function || !contains(sanitizer.Outputs, symbol) {
			continue
		}
		if locationAtOrBefore(after, sanitizer.Location) && locationAtOrBefore(sanitizer.Location, before) {
			return true
		}
	}
	return false
}

func locationAtOrBefore(left, right semantic.Location) bool {
	if left.File == "" || right.File == "" || left.File != right.File {
		return true
	}
	if left.StartLine <= 0 || right.StartLine <= 0 {
		return true
	}
	rightEndLine := right.EndLine
	if rightEndLine < right.StartLine {
		rightEndLine = right.StartLine
	}
	if left.StartLine != right.StartLine {
		return left.StartLine <= rightEndLine
	}
	// Nested Go expressions are evaluated inside an enclosing call whose AST
	// starts at an earlier column on the same line (for example c.Redirect(...,
	// c.Query(...))). Line equality is therefore ordered conservatively.
	return true
}

func stateKey(file, function, symbol string) string {
	return file + "\x00" + function + "\x00" + symbol
}

func functionKey(file, function string) string {
	return file + "\x00" + function
}

func clonePath(path taintPath) taintPath {
	path.propagation = append([]semantic.Fact(nil), path.propagation...)
	return path
}

func lastSelector(name string) string {
	name = strings.TrimSpace(name)
	if index := strings.LastIndex(name, "."); index >= 0 {
		return name[index+1:]
	}
	return name
}
