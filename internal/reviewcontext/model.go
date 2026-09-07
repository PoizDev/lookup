// Package reviewcontext builds bounded deterministic semantic evidence packets
// from Lookup's existing graph. It never reparses or scans the repository.
package reviewcontext

import (
	"encoding/json"

	"github.com/poizdev/lookup/internal/finding"
)

type EvidenceKind string

const (
	EvidencePrimary    EvidenceKind = "primary"
	EvidenceEnclosing  EvidenceKind = "enclosing"
	EvidenceGuard      EvidenceKind = "guard"
	EvidenceProvenance EvidenceKind = "provenance"
	EvidenceLifecycle  EvidenceKind = "lifecycle"
	EvidenceSymbol     EvidenceKind = "symbol"
	EvidenceCaller     EvidenceKind = "caller"
	EvidenceCallee     EvidenceKind = "callee"
	EvidenceComment    EvidenceKind = "comment"
	EvidenceEcosystem  EvidenceKind = "ecosystem"
)

type Sufficiency string

const (
	SufficiencySufficient   Sufficiency = "SUFFICIENT"
	SufficiencyIncomplete   Sufficiency = "INCOMPLETE"
	SufficiencyInsufficient Sufficiency = "INSUFFICIENT"
)

const DefaultCanonicalPacketTokenLimit = 32 * 1024

type Limits struct {
	MaxSourceLines     int
	MaxSymbols         int
	MaxCallers         int
	MaxCallees         int
	MaxGraphDepth      int
	MaxProvenanceSteps int
	MaxEvidenceItems   int
	MaxApproxTokens    int
}

func DefaultLimits() Limits {
	return Limits{MaxSourceLines: 120, MaxSymbols: 12, MaxCallers: 4, MaxCallees: 4, MaxGraphDepth: 2, MaxProvenanceSteps: 12, MaxEvidenceItems: 32, MaxApproxTokens: DefaultCanonicalPacketTokenLimit}
}

type EvidenceItem struct {
	ID        string            `json:"id"`
	Kind      EvidenceKind      `json:"kind"`
	Location  *finding.Location `json:"location,omitempty"`
	Symbol    string            `json:"symbol,omitempty"`
	Content   string            `json:"content"`
	FactID    string            `json:"fact_id,omitempty"`
	Retention RetentionPriority `json:"-"`
	Required  bool              `json:"-"`
}

type RetentionPriority uint8

const (
	RetentionGeneric RetentionPriority = iota + 1
	RetentionSupporting
	RetentionCritical
)

func (item EvidenceItem) retentionPriority() RetentionPriority {
	if item.Retention != 0 {
		return item.Retention
	}
	switch item.Kind {
	case EvidencePrimary, EvidenceGuard, EvidenceProvenance, EvidenceLifecycle, EvidenceComment:
		return RetentionCritical
	case EvidenceEnclosing:
		return RetentionSupporting
	default:
		return RetentionGeneric
	}
}

func RemoveLowestPriorityEvidence(packet ReviewPacket) (ReviewPacket, bool) {
	if len(packet.Evidence) <= 1 {
		return packet, false
	}
	remove := -1
	priority := RetentionCritical + 1
	for index, item := range packet.Evidence {
		if item.Kind == EvidencePrimary {
			continue
		}
		candidate := item.retentionPriority()
		if remove >= 0 && candidate == priority && packet.Evidence[remove].Required && !item.Required {
			remove = index
			continue
		}
		if candidate < priority || remove < 0 {
			priority, remove = candidate, index
		}
	}
	if remove < 0 {
		return packet, false
	}
	packet.Evidence = append(append([]EvidenceItem(nil), packet.Evidence[:remove]...), packet.Evidence[remove+1:]...)
	packet.Metadata.Truncated = true
	return packet, true
}

func ReevaluateSufficiency(original, transformed ReviewPacket) ReviewPacket {
	if original.Sufficiency == SufficiencyInsufficient || transformed.Sufficiency == SufficiencyInsufficient {
		transformed.Sufficiency = SufficiencyInsufficient
		return transformed
	}
	remaining := transformed.EvidenceIDs()
	criticalLost := original.Candidate.Observation != transformed.Candidate.Observation || original.Candidate.Hypothesis != transformed.Candidate.Hypothesis
	optionalLost := false
	for _, item := range original.Evidence {
		if _, ok := remaining[item.ID]; !ok {
			if item.Kind == EvidencePrimary || item.Required {
				criticalLost = true
			} else {
				optionalLost = true
			}
			continue
		}
		for _, kept := range transformed.Evidence {
			if kept.ID == item.ID && kept.Content != item.Content {
				if item.Kind == EvidencePrimary || item.Required {
					criticalLost = true
				} else {
					optionalLost = true
				}
			}
		}
	}
	if criticalLost {
		transformed.Sufficiency = SufficiencyInsufficient
		const missing = "adjudication-critical evidence removed during context minimization"
		for _, existing := range transformed.MissingInformation {
			if existing == missing {
				return transformed
			}
		}
		transformed.MissingInformation = append(transformed.MissingInformation, missing)
		return transformed
	}
	if optionalLost {
		transformed.Sufficiency = SufficiencyIncomplete
		const missing = "optional evidence removed during context minimization"
		for _, existing := range transformed.MissingInformation {
			if existing == missing {
				return transformed
			}
		}
		transformed.MissingInformation = append(transformed.MissingInformation, missing)
	}
	return transformed
}

type PacketMetadata struct {
	EstimatedInputTokens int  `json:"estimated_input_tokens"`
	PacketBytes          int  `json:"packet_bytes"`
	EvidenceCount        int  `json:"evidence_count"`
	Truncated            bool `json:"truncated"`
}

type ReviewPacket struct {
	Candidate       finding.PotentialFinding `json:"candidate"`
	PrimaryLocation *finding.Location        `json:"primary_location,omitempty"`
	EnclosingSymbol string                   `json:"enclosing_symbol,omitempty"`
	Evidence        []EvidenceItem           `json:"evidence"`
	// These typed indexes are local conveniences over Evidence. They are omitted
	// from the canonical provider representation to avoid duplicate context and
	// conflicting evidence-ID views.
	Provenance         []EvidenceItem `json:"-"`
	Callers            []EvidenceItem `json:"-"`
	Callees            []EvidenceItem `json:"-"`
	MissingInformation []string       `json:"missing_information,omitempty"`
	Sufficiency        Sufficiency    `json:"sufficiency"`
	Metadata           PacketMetadata `json:"metadata"`
}

func (packet ReviewPacket) Reviewable() bool {
	return packet.Sufficiency == SufficiencySufficient || packet.Sufficiency == SufficiencyIncomplete
}

// ProviderView retains the original candidate in the in-memory ReviewPacket while
// removing redundant legacy payloads from the bounded provider representation.
func ProviderView(packet ReviewPacket) ReviewPacket {
	view := packet
	original := view
	view.Candidate.ID = truncateProviderText(view.Candidate.ID, 256)
	view.Candidate.RuleID = truncateProviderText(view.Candidate.RuleID, 256)
	view.Candidate.Title = truncateProviderText(view.Candidate.Title, 512)
	view.Evidence = append([]EvidenceItem(nil), packet.Evidence...)
	for index := range view.Evidence {
		if view.Evidence[index].Location != nil {
			location := *view.Evidence[index].Location
			view.Evidence[index].Location = &location
		}
	}
	view.MissingInformation = append([]string(nil), packet.MissingInformation...)
	if packet.PrimaryLocation != nil {
		location := *packet.PrimaryLocation
		location.File = truncateProviderText(location.File, 1024)
		view.PrimaryLocation = &location
	}
	if packet.Candidate.Location != nil {
		location := *packet.Candidate.Location
		location.File = truncateProviderText(location.File, 1024)
		view.Candidate.Location = &location
	}
	if original.Candidate.ID != view.Candidate.ID || original.Candidate.RuleID != view.Candidate.RuleID || original.Candidate.Title != view.Candidate.Title || (original.PrimaryLocation != nil && original.PrimaryLocation.File != view.PrimaryLocation.File) || (original.Candidate.Location != nil && original.Candidate.Location.File != view.Candidate.Location.File) {
		view.Metadata.Truncated = true
	}
	view.Candidate.Finding.Evidence = finding.Evidence{}
	view.Candidate.Finding.AIReview = nil
	view.Candidate.Finding.Adjudication = nil
	view.Candidate.Finding.Recommendation = ""
	view.Candidate.Finding.Reason = ""
	view.Provenance = nil
	view.Callers = nil
	view.Callees = nil
	return view
}

func truncateProviderText(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	return value[:maximum] + "…"
}

// RecalculateMetadata describes the canonical provider representation.
func RecalculateMetadata(packet ReviewPacket) ReviewPacket {
	view := ProviderView(packet)
	truncated := packet.Metadata.Truncated || view.Metadata.Truncated
	view.Metadata = PacketMetadata{Truncated: truncated}
	data, _ := json.Marshal(view)
	packet.Metadata.PacketBytes = len(data)
	packet.Metadata.EstimatedInputTokens = (len(data) + 3) / 4
	packet.Metadata.EvidenceCount = len(packet.Evidence)
	packet.Metadata.Truncated = truncated
	return packet
}

func (packet ReviewPacket) EvidenceIDs() map[string]struct{} {
	ids := make(map[string]struct{}, len(packet.Evidence))
	for _, item := range packet.Evidence {
		ids[item.ID] = struct{}{}
	}
	return ids
}
