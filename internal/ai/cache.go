package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/poizdev/lookup/internal/finding"
)

type CacheMetadata struct{ Provider, Model, PromptSchema, RuleVersion string }
type AssessmentCache struct{ dir string }

func NewAssessmentCache(dir string) *AssessmentCache {
	_ = os.MkdirAll(dir, 0o700)
	return &AssessmentCache{dir: dir}
}
func DefaultAssessmentCache() *AssessmentCache {
	base, err := os.UserCacheDir()
	if err != nil {
		return &AssessmentCache{}
	}
	return NewAssessmentCache(filepath.Join(base, "lookup", "ai-assessments"))
}
func (cache *AssessmentCache) key(request ReviewRequest, meta CacheMetadata) string {
	payload := struct {
		Context  canonicalContext
		Metadata CacheMetadata
	}{canonicalizeForTransmission(request), meta}
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func (cache *AssessmentCache) Get(request ReviewRequest, meta CacheMetadata) (finding.AIAssessment, bool) {
	if cache == nil || cache.dir == "" {
		return finding.AIAssessment{}, false
	}
	data, err := os.ReadFile(filepath.Join(cache.dir, cache.key(request, meta)+".json"))
	if err != nil {
		return finding.AIAssessment{}, false
	}
	var assessment finding.AIAssessment
	if json.Unmarshal(data, &assessment) != nil || !validAssessment(assessment) {
		return finding.AIAssessment{}, false
	}
	return assessment, true
}

func validAssessment(assessment finding.AIAssessment) bool {
	if assessment.Malformed {
		return false
	}
	if assessment.Confidence < 0 || assessment.Confidence > 1 {
		return false
	}
	switch assessment.Severity {
	case finding.SeverityCritical, finding.SeverityHigh, finding.SeverityMedium, finding.SeverityLow, finding.SeverityInfo:
	default:
		return false
	}
	if strings.TrimSpace(assessment.Reason) == "" {
		return false
	}
	derived, invalid := deriveAdjudication(EvidenceRelation(assessment.EvidenceRelation), ClaimStrength(assessment.ClaimStrength))
	return invalid == "" && assessment.Verdict == derived
}
func (cache *AssessmentCache) Put(request ReviewRequest, meta CacheMetadata, assessment finding.AIAssessment) {
	if cache == nil || cache.dir == "" || assessment.Malformed || os.MkdirAll(cache.dir, 0o700) != nil {
		return
	}
	data, err := json.Marshal(assessment)
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(cache.dir, ".assessment-*")
	if err != nil {
		return
	}
	name := tmp.Name()
	defer os.Remove(name)
	_ = tmp.Chmod(0o600)
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Close()
	} else {
		_ = tmp.Close()
	}
	if err == nil {
		_ = os.Rename(name, filepath.Join(cache.dir, cache.key(request, meta)+".json"))
	}
}
