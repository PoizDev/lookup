// Package scoring turns normalized findings into deterministic health scores.
package scoring

import (
	"math"
	"sort"

	"github.com/poizdev/lookup/internal/finding"
)

type Scores struct {
	Overall    float64                      `json:"overall"`
	ByCategory map[finding.Category]float64 `json:"by_category"`
}

var categoryWeights = map[finding.Category]float64{
	finding.CategorySecurity:        .30,
	finding.CategoryCorrectness:     .25,
	finding.CategoryArchitecture:    .20,
	finding.CategoryPerformance:     .15,
	finding.CategoryMaintainability: .10,
}

var severityWeights = map[finding.Severity]float64{
	finding.SeverityCritical: 15,
	finding.SeverityHigh:     8,
	finding.SeverityMedium:   3,
	finding.SeverityLow:      1,
	finding.SeverityInfo:     0,
}

var categoryOrder = []finding.Category{
	finding.CategorySecurity,
	finding.CategoryCorrectness,
	finding.CategoryArchitecture,
	finding.CategoryPerformance,
	finding.CategoryMaintainability,
}

// Calculate deterministically scores only findings accepted by the certainty
// gate. Provider output has no score field and cannot enter this boundary.
func Calculate(findings []finding.FinalFinding) Scores {
	scores := Scores{ByCategory: make(map[finding.Category]float64, len(categoryWeights))}
	for _, category := range categoryOrder {
		scores.ByCategory[category] = 100
	}

	type rulePenalty struct {
		contributions []float64
		maxWeight     float64
	}
	grouped := make(map[finding.Category]map[string]*rulePenalty)
	for _, item := range findings {
		if _, known := categoryWeights[item.Category]; !known {
			continue
		}
		confidence := min(max(item.Confidence, 0), 1)
		weight := severityWeights[item.Severity]
		if grouped[item.Category] == nil {
			grouped[item.Category] = make(map[string]*rulePenalty)
		}
		penalty := grouped[item.Category][item.RuleID]
		if penalty == nil {
			penalty = &rulePenalty{}
			grouped[item.Category][item.RuleID] = penalty
		}
		penalty.contributions = append(penalty.contributions, weight*confidence)
		penalty.maxWeight = max(penalty.maxWeight, weight)
	}

	for _, category := range categoryOrder {
		rules := grouped[category]
		ruleIDs := make([]string, 0, len(rules))
		for ruleID := range rules {
			ruleIDs = append(ruleIDs, ruleID)
		}
		sort.Strings(ruleIDs)
		for _, ruleID := range ruleIDs {
			penalty := rules[ruleID]
			sort.Slice(penalty.contributions, func(i, j int) bool { return penalty.contributions[i] > penalty.contributions[j] })
			var total float64
			for index, contribution := range penalty.contributions {
				total += contribution / math.Sqrt(float64(index+1))
			}
			// Preserve the first finding's existing weight, then give higher-severity
			// rules proportionally more headroom without allowing one rule to dominate.
			cap := 10 + 2*penalty.maxWeight
			scores.ByCategory[category] -= min(total, cap)
		}
	}

	for _, category := range categoryOrder {
		scores.ByCategory[category] = max(scores.ByCategory[category], 0)
		scores.Overall += scores.ByCategory[category] * categoryWeights[category]
	}
	return scores
}
