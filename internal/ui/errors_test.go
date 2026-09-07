package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/poizdev/lookup/internal/apperror"
)

func TestRenderErrorUnknownIncludesMessageAndDebugHint(t *testing.T) {
	out := RenderError(Capability{Unicode: true}, errors.New("xyzzy failure"))
	if !strings.Contains(out, "✘") || !strings.Contains(out, "xyzzy failure") || !strings.Contains(out, "--debug") {
		t.Fatalf("unexpected unknown error rendering: %q", out)
	}
}

func TestRenderErrorUsesASCIIFallback(t *testing.T) {
	if out := RenderError(Capability{}, errors.New("fail")); !strings.Contains(out, "[ERR]") {
		t.Fatalf("expected ASCII error icon: %q", out)
	}
}

func TestRenderErrorKnownHints(t *testing.T) {
	tests := []struct {
		kind apperror.Kind
		want string
	}{
		{apperror.KindAuthentication, "lookup init"},
		{apperror.KindNetworkTimeout, "--no-ai"},
		{apperror.KindRateLimit, "Wait"},
		{apperror.KindPathNotFound, "lookup ."},
	}
	for _, tt := range tests {
		err := apperror.New(tt.kind, "opaque failure")
		if out := RenderError(Capability{Unicode: true}, err); !strings.Contains(out, tt.want) {
			t.Errorf("RenderError(%q) = %q, want %q", tt.kind, out, tt.want)
		}
	}
}

func TestRenderErrorUsesTypedClassificationNotMessageText(t *testing.T) {
	err := apperror.New(apperror.KindRateLimit, "opaque provider failure")
	out := RenderError(Capability{Unicode: true}, err)
	if !strings.Contains(out, "Rate limit") || !strings.Contains(out, "Wait") {
		t.Fatalf("typed rate-limit hint missing: %q", out)
	}
	plain := RenderError(Capability{Unicode: true}, errors.New("the number 429 appears but this is not a provider error"))
	if strings.Contains(plain, "Rate limit exceeded") {
		t.Fatalf("plain string was misclassified: %q", plain)
	}
}
