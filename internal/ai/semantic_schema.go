package ai

import "github.com/poizdev/lookup/internal/finding"

// SemanticReviewSchema is the provider-neutral structured-output contract for
// v3 adjudication responses. Providers serialize this same schema in their
// native structured-output request field.
func SemanticReviewSchema() map[string]any {
	baseProperties := func(relation EvidenceRelation) map[string]any {
		return map[string]any{
			"id":                map[string]any{"type": "string"},
			"evidence_relation": map[string]any{"type": "string", "enum": []string{string(relation)}},
			"reason":            map[string]any{"type": "string"},
			"recommendation":    map[string]any{"type": "string"},
			"evidence_ids":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}
	}
	baseRequired := []string{"id", "evidence_relation", "reason", "recommendation", "evidence_ids"}
	variant := func(relation EvidenceRelation) map[string]any {
		return map[string]any{
			"type":                 "object",
			"properties":           baseProperties(relation),
			"required":             append([]string(nil), baseRequired...),
			"additionalProperties": false,
		}
	}
	supports := variant(EvidenceSupports)
	supportsProperties := supports["properties"].(map[string]any)
	supportsProperties["claim_strength"] = map[string]any{"type": "string", "enum": []string{string(ClaimStrengthUnchanged), string(ClaimStrengthLower)}}
	supportsProperties["severity"] = map[string]any{"type": "string", "enum": []string{
		string(finding.SeverityCritical), string(finding.SeverityHigh), string(finding.SeverityMedium), string(finding.SeverityLow), string(finding.SeverityInfo),
	}}
	supportsProperties["confidence"] = map[string]any{"type": "number", "minimum": 0.0, "maximum": 1.0}
	supports["required"] = append(supports["required"].([]string), "claim_strength", "severity", "confidence")

	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"assessments": map[string]any{
				"type": "array",
				"items": map[string]any{"anyOf": []any{
					supports,
					variant(EvidenceContradicts),
					variant(EvidenceInsufficient),
				}},
			},
		},
		"required":             []string{"assessments"},
		"additionalProperties": false,
	}
}
