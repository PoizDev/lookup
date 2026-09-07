package ai

import (
	"regexp"
	"strconv"
)

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]{12,}`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`),
	regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`),
	regexp.MustCompile(`\bsk-(?:proj-)?[A-Za-z0-9_-]{16,}\b`),
	regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----[\s\S]*?-----END (?:RSA |EC |OPENSSH )?PRIVATE KEY-----`),
}

var credentialValuePattern = regexp.MustCompile(`(?i)((?:password|passwd|api[_-]?key|secret|token)\s*(?::=|=|:)\s*["'])([^"']{8,})(["'])`)

func redactContexts(items []canonicalContext) []canonicalContext {
	replacements := map[string]string{}
	next := 1
	placeholder := func(secret string) string {
		if existing, ok := replacements[secret]; ok {
			return existing
		}
		value := "<REDACTED_SECRET_" + strconv.Itoa(next) + ">"
		next++
		replacements[secret] = value
		return value
	}
	redact := func(value string) string {
		value = credentialValuePattern.ReplaceAllStringFunc(value, func(match string) string {
			parts := credentialValuePattern.FindStringSubmatch(match)
			return parts[1] + placeholder(parts[2]) + parts[3]
		})
		for _, pattern := range secretPatterns {
			value = pattern.ReplaceAllStringFunc(value, func(secret string) string {
				return placeholder(secret)
			})
		}
		return value
	}
	for i := range items {
		items[i].ID = redact(items[i].ID)
		items[i].RuleID = redact(items[i].RuleID)
		items[i].Title = redact(items[i].Title)
		items[i].Observation = redact(items[i].Observation)
		items[i].Hypothesis = redact(items[i].Hypothesis)
		items[i].Snippet = redact(items[i].Snippet)
		items[i].Message = redact(items[i].Message)
		if items[i].Location != nil {
			items[i].Location.File = redact(items[i].Location.File)
		}
		for j := range items[i].Evidence {
			items[i].Evidence[j].Expression = redact(items[i].Evidence[j].Expression)
			items[i].Evidence[j].Message = redact(items[i].Evidence[j].Message)
		}
		for j := range items[i].CallChain {
			items[i].CallChain[j] = redact(items[i].CallChain[j])
		}
		if items[i].Packet != nil {
			packet := items[i].Packet
			packet.Candidate.ID = redact(packet.Candidate.ID)
			packet.Candidate.RuleID = redact(packet.Candidate.RuleID)
			packet.Candidate.Title = redact(packet.Candidate.Title)
			packet.Candidate.Observation = redact(packet.Candidate.Observation)
			packet.Candidate.Hypothesis = redact(packet.Candidate.Hypothesis)
			packet.Candidate.Reason = redact(packet.Candidate.Reason)
			packet.EnclosingSymbol = redact(packet.EnclosingSymbol)
			if packet.PrimaryLocation != nil {
				packet.PrimaryLocation.File = redact(packet.PrimaryLocation.File)
			}
			for j := range packet.MissingInformation {
				packet.MissingInformation[j] = redact(packet.MissingInformation[j])
			}
			for j := range packet.Evidence {
				packet.Evidence[j].Content = redact(packet.Evidence[j].Content)
				packet.Evidence[j].Symbol = redact(packet.Evidence[j].Symbol)
				packet.Evidence[j].FactID = redact(packet.Evidence[j].FactID)
				if packet.Evidence[j].Location != nil {
					packet.Evidence[j].Location.File = redact(packet.Evidence[j].Location.File)
				}
			}
			for j := range packet.Provenance {
				packet.Provenance[j].Content = redact(packet.Provenance[j].Content)
			}
			for j := range packet.Callers {
				packet.Callers[j].Content = redact(packet.Callers[j].Content)
			}
			for j := range packet.Callees {
				packet.Callees[j].Content = redact(packet.Callees[j].Content)
			}
		}
	}
	return items
}
