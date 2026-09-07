package reporter

import (
	"encoding/json"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/poizdev/lookup/internal/finding"
)

type SARIF struct {
	writer   io.Writer
	repoPath string
}

func NewSARIF(writer io.Writer, repoPath string) *SARIF { return &SARIF{writer, repoPath} }
func (s *SARIF) Report(result *Result) error {
	toolVersion := result.ToolVersion
	if toolVersion == "" {
		toolVersion = "dev"
	}
	doc := sarifDocument{Schema: "https://docs.oasis-open.org/sarif/sarif/v2.1.0/errata01/os/schemas/sarif-schema-2.1.0.json", Version: "2.1.0", Runs: []sarifRun{{Tool: sarifTool{Driver: sarifDriver{Name: "Lookup", SemanticVersion: toolVersion, Rules: s.rules(result.Findings)}}, Results: s.results(result.Findings)}}}
	enc := json.NewEncoder(s.writer)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}
func (s *SARIF) rules(findings []finding.Finding) []sarifRule {
	byID := map[string]finding.Finding{}
	for _, f := range findings {
		if _, ok := byID[f.RuleID]; !ok {
			byID[f.RuleID] = f
		}
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	rules := make([]sarifRule, 0, len(ids))
	for _, id := range ids {
		f := byID[id]
		rules = append(rules, sarifRule{ID: id, Name: f.Title, ShortDescription: sarifMessage{f.Title}, DefaultConfiguration: sarifRuleConfig{sarifLevel(f.Severity)}, Properties: sarifRuleProperties{string(f.Category)}})
	}
	return rules
}
func (s *SARIF) results(findings []finding.Finding) []sarifResult {
	ordered := append([]finding.Finding(nil), findings...)
	sort.SliceStable(ordered, func(i, j int) bool {
		left, right := ordered[i], ordered[j]
		if left.RuleID != right.RuleID {
			return left.RuleID < right.RuleID
		}
		leftLocation, rightLocation := "", ""
		if len(left.Evidence.AffectedFiles) > 0 {
			leftLocation = left.Evidence.AffectedFiles[0]
		}
		if len(right.Evidence.AffectedFiles) > 0 {
			rightLocation = right.Evidence.AffectedFiles[0]
		}
		if leftLocation != rightLocation {
			return leftLocation < rightLocation
		}
		return left.ID < right.ID
	})
	results := make([]sarifResult, 0, len(ordered))
	for _, f := range ordered {
		message := f.Reason
		if message == "" {
			message = f.Title
		}
		r := sarifResult{RuleID: f.RuleID, Level: sarifLevel(f.Severity), Message: sarifMessage{message}}
		if f.Location != nil && f.Location.File != "" {
			location := sarifLocation{PhysicalLocation: &sarifPhysicalLocation{ArtifactLocation: sarifArtifactLocation{s.relative(f.Location.File)}}}
			if f.Location.StartLine > 0 {
				location.PhysicalLocation.Region = &sarifRegion{f.Location.StartLine}
			}
			r.Locations = append(r.Locations, location)
		}
		for _, affected := range f.Evidence.AffectedFiles {
			if f.Location != nil && sameAffectedFile(affected, f.Location.File) {
				continue
			}
			file, line := parseFileLine(affected)
			location := sarifLocation{PhysicalLocation: &sarifPhysicalLocation{ArtifactLocation: sarifArtifactLocation{s.relative(file)}}}
			if line > 0 {
				location.PhysicalLocation.Region = &sarifRegion{line}
			}
			r.Locations = append(r.Locations, location)
		}
		if len(f.Evidence.Steps) > 0 {
			flow := sarifCodeFlow{ThreadFlows: []sarifThreadFlow{{}}}
			for _, step := range f.Evidence.Steps {
				message := strings.TrimSpace(step.Message)
				if step.Kind != "" {
					message = step.Kind + ": " + message
				}
				location := sarifLocation{Message: &sarifMessage{message}}
				if step.File != "" {
					location.PhysicalLocation = &sarifPhysicalLocation{ArtifactLocation: sarifArtifactLocation{s.relative(step.File)}}
					if step.Line > 0 {
						location.PhysicalLocation.Region = &sarifRegion{step.Line}
					}
				}
				flow.ThreadFlows[0].Locations = append(flow.ThreadFlows[0].Locations, sarifThreadFlowLocation{Location: location})
			}
			r.CodeFlows = []sarifCodeFlow{flow}
		} else if len(f.Evidence.CallChain) > 0 {
			flow := sarifCodeFlow{ThreadFlows: []sarifThreadFlow{{}}}
			for _, call := range f.Evidence.CallChain {
				flow.ThreadFlows[0].Locations = append(flow.ThreadFlows[0].Locations, sarifThreadFlowLocation{Location: sarifLocation{Message: &sarifMessage{call}}})
			}
			r.CodeFlows = []sarifCodeFlow{flow}
		}
		results = append(results, r)
	}
	return results
}

func sameAffectedFile(affected, primary string) bool {
	file, _ := parseFileLine(affected)
	return filepath.Clean(file) == filepath.Clean(primary)
}
func (s *SARIF) relative(path string) string {
	if s.repoPath != "" && filepath.IsAbs(path) {
		if rel, err := filepath.Rel(s.repoPath, path); err == nil {
			path = rel
		}
	}
	path = filepath.Clean(path)
	if strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		path = filepath.Base(path)
	}
	return filepath.ToSlash(strings.TrimPrefix(path, "./"))
}
func parseFileLine(location string) (string, int) {
	index := strings.LastIndex(location, ":")
	if index < 0 {
		return location, 0
	}
	line, err := strconv.Atoi(location[index+1:])
	if err != nil {
		return location, 0
	}
	return location[:index], line
}
func sarifLevel(severity finding.Severity) string {
	switch severity {
	case finding.SeverityCritical, finding.SeverityHigh:
		return "error"
	case finding.SeverityMedium:
		return "warning"
	default:
		return "note"
	}
}

type sarifDocument struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}
type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}
type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}
type sarifDriver struct {
	Name            string      `json:"name"`
	SemanticVersion string      `json:"semanticVersion,omitempty"`
	Rules           []sarifRule `json:"rules,omitempty"`
}
type sarifRule struct {
	ID                   string              `json:"id"`
	Name                 string              `json:"name,omitempty"`
	ShortDescription     sarifMessage        `json:"shortDescription"`
	DefaultConfiguration sarifRuleConfig     `json:"defaultConfiguration"`
	Properties           sarifRuleProperties `json:"properties,omitempty"`
}
type sarifRuleConfig struct {
	Level string `json:"level"`
}
type sarifRuleProperties struct {
	Category string `json:"category,omitempty"`
}
type sarifMessage struct {
	Text string `json:"text"`
}
type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Message   sarifMessage    `json:"message"`
	Locations []sarifLocation `json:"locations,omitempty"`
	CodeFlows []sarifCodeFlow `json:"codeFlows,omitempty"`
}
type sarifLocation struct {
	PhysicalLocation *sarifPhysicalLocation `json:"physicalLocation,omitempty"`
	Message          *sarifMessage          `json:"message,omitempty"`
}
type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           *sarifRegion          `json:"region,omitempty"`
}
type sarifArtifactLocation struct {
	URI string `json:"uri"`
}
type sarifRegion struct {
	StartLine int `json:"startLine"`
}
type sarifCodeFlow struct {
	ThreadFlows []sarifThreadFlow `json:"threadFlows"`
}
type sarifThreadFlow struct {
	Locations []sarifThreadFlowLocation `json:"locations"`
}
type sarifThreadFlowLocation struct {
	Location sarifLocation `json:"location"`
}
