package security

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/poizdev/lookup/internal/analyzer"
	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language"
)

// HardcodedSecret implements GO-SEC-001: detection of hardcoded secrets in Go source.
//
// It scans variable and constant nodes in the code graph for assignments
// that look like secrets (API keys, passwords, tokens, etc.) by combining:
//   - Name-based keyword matching (variable name contains "password", "api_key", etc.)
//   - Value pattern matching (high-entropy strings, known prefixes like "sk-", "ghp_")
//
// Secrets found in test files are reported with Low severity instead of Critical.
type HardcodedSecret struct {
	RepositoryRoot string
}

var _ analyzer.Rule = (*HardcodedSecret)(nil)

func (r *HardcodedSecret) ID() string                  { return "GO-SEC-001" }
func (r *HardcodedSecret) Category() analyzer.Category { return analyzer.CategorySecurity }
func (r *HardcodedSecret) Severity() analyzer.Severity { return analyzer.SeverityCritical }

// sensitiveNamePatterns matches variable/constant names that typically hold secrets.
var sensitiveNamePatterns = []string{
	"password",
	"passwd",
	"pass_word",
	"api_key",
	"apikey",
	"api_secret",
	"apisecret",
	"secret",
	"secret_key",
	"secretkey",
	"token",
	"access_token",
	"auth_token",
	"private_key",
	"privatekey",
	"encryption_key",
	"signing_key",
	"client_secret",
	"db_password",
	"database_password",
	"connection_string",
	"credentials",
}

// knownPrefixPatterns matches values that start with known API key prefixes.
var knownPrefixPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^sk-[a-zA-Z0-9]{20,}`),                                   // OpenAI
	regexp.MustCompile(`^sk_live_[a-zA-Z0-9]{20,}`),                              // Stripe live
	regexp.MustCompile(`^sk_test_[a-zA-Z0-9]{20,}`),                              // Stripe test
	regexp.MustCompile(`^ghp_[a-zA-Z0-9]{36,}`),                                  // GitHub PAT
	regexp.MustCompile(`^gho_[a-zA-Z0-9]{36,}`),                                  // GitHub OAuth
	regexp.MustCompile(`^github_pat_[a-zA-Z0-9_]{30,}`),                          // GitHub fine-grained PAT
	regexp.MustCompile(`^glpat-[a-zA-Z0-9\-_]{20,}`),                             // GitLab PAT
	regexp.MustCompile(`^xox[bpas]-[a-zA-Z0-9\-]{10,}`),                          // Slack tokens
	regexp.MustCompile(`^AKIA[0-9A-Z]{16}`),                                      // AWS Access Key
	regexp.MustCompile(`^AIza[a-zA-Z0-9_\-]{35}`),                                // Google API Key
	regexp.MustCompile(`^eyJ[a-zA-Z0-9_\-]+\.[a-zA-Z0-9_\-]+\.[a-zA-Z0-9_\-]+$`), // JWT
}

// genericSecretValuePattern matches strings that look like high-entropy secrets.
// Requires at least 16 characters mixing alphanumeric and special chars.
var genericSecretValuePattern = regexp.MustCompile(`^[a-zA-Z0-9+/=_\-]{16,}$`)

// testFilePatterns identifies test file paths.
var testFilePatterns = []string{
	"_test.go",
	"_test.ts",
	"_test.js",
	"_test.py",
	"test_",
	"/test/",
	"/tests/",
	"__tests__/",
	"/testdata/",
	"/fixtures/",
	"_mock",
	"_fake",
}

// falsePositiveValues are common non-secret default/placeholder values.
var falsePositiveValues = map[string]bool{
	"":                  true,
	"\"\"":              true,
	"''":                true,
	"``":                true,
	"nil":               true,
	"null":              true,
	"None":              true,
	"TODO":              true,
	"CHANGEME":          true,
	"changeme":          true,
	"your-api-key":      true,
	"your-api-key-here": true,
	"your-secret-here":  true,
	"xxx":               true,
	"placeholder":       true,
	"example":           true,
	"test":              true,
	"default":           true,
	"os.Getenv":         true,
	"os.LookupEnv":      true,
	"viper.GetString":   true,
	"config.":           true,
	"cfg.":              true,
}

func (r *HardcodedSecret) Check(g *graph.Graph) []language.PotentialFinding {
	var findings []language.PotentialFinding

	g.Mu().RLock()
	defer g.Mu().RUnlock()

	for _, node := range g.Nodes {
		if node.Kind != graph.NodeVariable && node.Kind != graph.NodeConstant {
			continue
		}

		nameLower := strings.ToLower(node.Name)

		// Check if the variable/constant name matches a sensitive keyword.
		if !matchesSensitiveName(nameLower) {
			continue
		}

		// Get the assigned value from metadata (stored by builder).
		value := getMetadataString(node, "Body")
		if value == "" {
			value = getMetadataString(node, "Value")
		}

		// Skip empty or placeholder values.
		if isFalsePositive(value) {
			continue
		}

		// Skip values that are clearly function calls or variable references (not literals).
		if looksLikeFunctionCall(value) || looksLikeVariableRef(value) {
			continue
		}
		if !credentialLikeLiteral(value) {
			continue
		}

		severity := r.Severity()
		title := "Hardcoded secret detected"
		description := "A variable or constant with a sensitive name contains what appears to be a hardcoded secret value. " +
			"Secrets should be stored in environment variables, a secrets manager, or a configuration file excluded from version control."

		// Downgrade severity for test files.
		if isTestFile(r.sourceIdentity(node.File)) {
			severity = analyzer.SeverityLow
			title = "Hardcoded secret in test file"
			description = "A potential hardcoded secret was found in a test file. " +
				"While less critical than production code, consider using environment variables or test fixtures."
		}

		finding := language.PotentialFinding{
			RuleID:           r.ID(),
			Category:         string(r.Category()),
			Severity:         string(severity),
			Title:            title,
			Description:      description,
			Observation:      "A string literal assigned to a sensitive-name variable matches the configured credential-like pattern.",
			Hypothesis:       "The literal may be a deployable secret rather than test data, a placeholder, or another non-secret identifier.",
			File:             node.File,
			StartLine:        node.StartLine,
			EndLine:          node.EndLine,
			AffectedSymbols:  []string{node.Name},
			Confidence:       0.90,
			EvidenceStrength: "strong",
		}

		findings = append(findings, finding)
	}

	// Also scan for known API key prefixes in ANY string value, regardless of name.
	for _, node := range g.Nodes {
		if node.Kind != graph.NodeVariable && node.Kind != graph.NodeConstant {
			continue
		}

		value := getMetadataString(node, "Body")
		if value == "" {
			value = getMetadataString(node, "Value")
		}

		if matchesKnownPrefix(value) {
			severity := analyzer.SeverityCritical
			if isTestFile(r.sourceIdentity(node.File)) {
				severity = analyzer.SeverityLow
			}

			// Avoid duplicate if already reported by name-based detection.
			if matchesSensitiveName(strings.ToLower(node.Name)) {
				continue
			}

			finding := language.PotentialFinding{
				RuleID:   r.ID(),
				Category: string(r.Category()),
				Severity: string(severity),
				Title:    "Known API key pattern detected",
				Description: "A variable contains a value matching a known API key format " +
					"(e.g., OpenAI, Stripe, GitHub, AWS). This is very likely a leaked secret.",
				Observation:      "A string literal matches one of the configured API-key prefix and length patterns.",
				Hypothesis:       "The matched literal may be an active credential rather than test or example data.",
				File:             node.File,
				StartLine:        node.StartLine,
				EndLine:          node.EndLine,
				AffectedSymbols:  []string{node.Name},
				Confidence:       0.98,
				EvidenceStrength: "strong",
			}

			findings = append(findings, finding)
		}
	}

	return findings
}

func (r *HardcodedSecret) sourceIdentity(path string) string {
	root := strings.TrimSpace(r.RepositoryRoot)
	if root == "" {
		return path
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return path
	}
	return filepath.ToSlash(relative)
}

func credentialLikeLiteral(value string) bool {
	cleaned := strings.Trim(strings.TrimSpace(value), "\"'`")
	if strings.Contains(cleaned, "%{") || strings.Contains(cleaned, "{{") || strings.Contains(cleaned, "${") {
		return false
	}
	return matchesKnownPrefix(value) || genericSecretValuePattern.MatchString(cleaned)
}

// matchesSensitiveName checks if the variable name contains any sensitive keyword.
func matchesSensitiveName(nameLower string) bool {
	for _, pattern := range sensitiveNamePatterns {
		if strings.Contains(nameLower, pattern) {
			return true
		}
	}
	return false
}

// matchesKnownPrefix checks if a value matches any known API key prefix pattern.
func matchesKnownPrefix(value string) bool {
	// Strip quotes from string literals.
	cleaned := strings.Trim(value, "\"'`")
	for _, re := range knownPrefixPatterns {
		if re.MatchString(cleaned) {
			return true
		}
	}
	return false
}

// isFalsePositive checks if the value is a known placeholder or non-secret.
func isFalsePositive(value string) bool {
	cleaned := strings.Trim(value, "\"'`")
	if falsePositiveValues[cleaned] {
		return true
	}
	// Also check if the value starts with known non-literal patterns.
	for fp := range falsePositiveValues {
		if fp != "" && strings.HasPrefix(cleaned, fp) {
			return true
		}
	}
	return false
}

// looksLikeFunctionCall checks if the value appears to be a function call rather than a literal.
func looksLikeFunctionCall(value string) bool {
	cleaned := strings.TrimSpace(value)
	// Contains parentheses and doesn't start with a quote — likely a function call.
	if strings.Contains(cleaned, "(") && !strings.HasPrefix(cleaned, "\"") &&
		!strings.HasPrefix(cleaned, "'") && !strings.HasPrefix(cleaned, "`") {
		return true
	}
	return false
}

// looksLikeVariableRef checks if the value is a reference to another variable.
func looksLikeVariableRef(value string) bool {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return true
	}
	// If it doesn't contain any quote character, it's likely a variable reference.
	if !strings.ContainsAny(cleaned, "\"'`") && !strings.ContainsAny(cleaned, "0123456789") {
		return true
	}
	return false
}

// isTestFile checks if the file path indicates a test file.
func isTestFile(path string) bool {
	pathLower := strings.ToLower(path)
	for _, pattern := range testFilePatterns {
		if strings.Contains(pathLower, pattern) {
			return true
		}
	}
	return false
}

// getMetadataString safely retrieves a string value from node metadata.
func getMetadataString(node *graph.Node, key string) string {
	if node.Metadata == nil {
		return ""
	}
	val, ok := node.Metadata[key]
	if !ok {
		return ""
	}
	str, ok := val.(string)
	if !ok {
		return ""
	}
	return str
}
