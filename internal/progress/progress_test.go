package progress

import (
	"strings"
	"testing"
)

func TestSpinnerAIReviewDetailShowsSelectedCachedAndPendingProgress(t *testing.T) {
	spinner := New(true)
	spinner.SetPhase(PhaseAIReview, "")
	spinner.SetDetail("284 selected · 231 cached")
	spinner.SetProgress(31, 53)
	detail := spinner.detail()
	for _, want := range []string{"284 selected", "231 cached", "31/53"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail %q missing %q", detail, want)
		}
	}
}
