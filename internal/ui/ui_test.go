package ui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/poizdev/lookup/internal/finding"
)

func TestTheme_SeverityColorsDiffer(t *testing.T) {
	theme := NewTheme(Capability{Color: true})
	if theme.SeverityColor(finding.SeverityCritical) == theme.SeverityColor(finding.SeverityLow) {
		t.Fatal("critical and low severity colors must differ")
	}
}

func TestTheme_ScoreBandsDiffer(t *testing.T) {
	theme := NewTheme(Capability{Color: true})
	if theme.ScoreColor(85) == theme.ScoreColor(50) {
		t.Fatal("healthy and unhealthy score colors must differ")
	}
}

func TestTheme_NoColor(t *testing.T) {
	if got := NewTheme(Capability{}).SeverityColor(finding.SeverityCritical); got != "" {
		t.Fatalf("expected empty color, got %q", got)
	}
}

func TestThemeUsesLookupBrandAccentAndReadableMutedColor(t *testing.T) {
	theme := NewTheme(Capability{Color: true})
	if got := string(theme.Primary()); got != "#F70E39" {
		t.Fatalf("Primary() = %q, want Lookup accent #F70E39", got)
	}
	if got := string(theme.Muted()); got == "#6B7280" || got == "" {
		t.Fatalf("Muted() = %q, want a higher-contrast semantic token", got)
	}
}

func TestHeadingAndCurrentStylesAreNeutralWhileFocusUsesAccent(t *testing.T) {
	cap := Capability{Color: true, Unicode: true, Width: 80, Height: 24}
	theme := NewTheme(cap)
	styles := NewStyles(theme, cap)
	if styles.Heading().GetForeground() == theme.Primary() {
		t.Fatal("heading uses the Lookup accent as its default foreground")
	}
	if styles.Selected().GetForeground() == theme.Primary() {
		t.Fatal("Current/selected state has the same visual weight as focus")
	}
	if styles.Focused().GetForeground() != theme.Primary() {
		t.Fatal("focused state does not use the Lookup accent")
	}
}

func TestIconsHaveASCIIFallback(t *testing.T) {
	for _, r := range Icons(false).Success {
		if r > 127 {
			t.Fatalf("ASCII icon contains %q", r)
		}
	}
}

func TestRenderVersionUsesCanonicalWordmarkOnWideUnicodeTTY(t *testing.T) {
	out := RenderVersion(Capability{IsTTY: true, Unicode: true, Width: 120}, "1.0.0", "abc123", "2026-08-23")
	if !strings.Contains(out, TerminalWordmark) {
		t.Fatalf("version output missing canonical wordmark: %q", out)
	}
	if strings.Contains(out, "██╗") {
		t.Fatalf("version output contains legacy banner: %q", out)
	}
}

func TestRenderVersionUsesCompactWordmarkWhenFullArtIsUnsafe(t *testing.T) {
	for _, tc := range []struct {
		name string
		cap  Capability
	}{
		{name: "narrow", cap: Capability{IsTTY: true, Unicode: true, Width: WordmarkWidth() - 1}},
		{name: "non tty", cap: Capability{Unicode: true, Width: 120}},
		{name: "no unicode", cap: Capability{IsTTY: true, Width: 120}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := RenderVersion(tc.cap, "dev", "none", "unknown")
			if !strings.HasPrefix(out, "lookup\n\n") {
				t.Fatalf("compact version output = %q", out)
			}
			if strings.Contains(out, TerminalWordmark) {
				t.Fatal("compact version output contains full wordmark")
			}
		})
	}
}

func TestRenderVersionFormatsDevelopmentMetadataWithoutVPrefix(t *testing.T) {
	out := RenderVersion(Capability{}, "dev", "none", "unknown")
	for _, want := range []string{"Version   dev", "Commit    none", "Built     unknown", "Source    github.com/poizdev/lookup"} {
		if !strings.Contains(out, want) {
			t.Fatalf("version output missing %q: %q", want, out)
		}
	}
	if strings.Contains(out, "vdev") {
		t.Fatalf("development version gained v prefix: %q", out)
	}
}

func TestRenderVersionCleansReleaseMetadata(t *testing.T) {
	out := RenderVersion(Capability{}, "0.1.0", "a51c392deadbeef", "2026-08-28T12:34:56Z")
	if !strings.Contains(out, "Version   0.1.0") || !strings.Contains(out, "Commit    a51c392deadb") || !strings.Contains(out, "Built     2026-08-28") {
		t.Fatalf("release metadata not cleaned: %q", out)
	}
	if strings.Contains(out, "v0.1.0") || strings.Contains(out, "a51c392deadbeef") {
		t.Fatalf("release metadata gained prefix or retained long commit: %q", out)
	}
}

func TestRenderVersionWithoutColorContainsNoANSI(t *testing.T) {
	out := RenderVersion(Capability{IsTTY: true, Unicode: true, Width: 120}, "dev", "none", "unknown")
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("NO_COLOR-compatible output contains ANSI: %q", out)
	}
	if !utf8.ValidString(out) {
		t.Fatal("version output is not valid UTF-8")
	}
}
