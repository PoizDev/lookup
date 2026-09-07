package reporter

import (
	"strings"
	"testing"
)

func TestZeroFindingFlavorPoolIntegrity(t *testing.T) {
	if got := len(approvedZeroFindingFlavors); got != 76 {
		t.Fatalf("flavor pool size = %d, want 76", got)
	}
	for index, flavor := range approvedZeroFindingFlavors {
		if strings.TrimSpace(flavor) == "" {
			t.Fatalf("flavor %d is empty", index)
		}
	}
}

func TestZeroFindingFlavorSameRepositoryAndRevisionIsStable(t *testing.T) {
	first := zeroFindingFlavor("repo", "1111111111111111111111111111111111111111")
	second := zeroFindingFlavor("repo", "1111111111111111111111111111111111111111")
	if first != second {
		t.Fatalf("same repository and revision selected %q then %q", first, second)
	}
}

func TestZeroFindingFlavorIdentityIncludesRevision(t *testing.T) {
	const repository = "repo"
	first := zeroFindingFlavorIdentity(repository, "1111111111111111111111111111111111111111")
	second := zeroFindingFlavorIdentity(repository, "2222222222222222222222222222222222222222")
	if first == second {
		t.Fatalf("selector identity does not include revision: %q", first)
	}
}

func TestZeroFindingFlavorIndexAlwaysStaysWithinPool(t *testing.T) {
	for _, identity := range []string{"", "repo", "repo\x001111111111111111111111111111111111111111"} {
		index := zeroFindingFlavorIndex(identity)
		if index < 0 || index >= len(approvedZeroFindingFlavors) {
			t.Fatalf("index for %q = %d, pool size %d", identity, index, len(approvedZeroFindingFlavors))
		}
	}
}

func TestZeroFindingFlavorWithoutRevisionPreservesDeterministicFallback(t *testing.T) {
	const want = "Suspiciously clean. Nice work."
	if first, second := zeroFindingFlavor("repo"), zeroFindingFlavor("repo", ""); first != want || second != want {
		t.Fatalf("fallback flavors = %q and %q, want %q", first, second, want)
	}
}
