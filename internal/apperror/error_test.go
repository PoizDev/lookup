package apperror

import (
	"errors"
	"testing"
)

func TestKindSurvivesWrapping(t *testing.T) {
	cause := errors.New("socket closed")
	err := Wrap(KindNetworkTimeout, "provider request timed out", cause)
	if !IsKind(err, KindNetworkTimeout) {
		t.Fatalf("kind was lost: %v", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("cause was lost: %v", err)
	}
}
