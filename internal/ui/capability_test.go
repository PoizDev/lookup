package ui

import (
	"bytes"
	"testing"
)

func TestDetect_NoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if Detect(&bytes.Buffer{}).Color {
		t.Fatal("expected Color=false when NO_COLOR is set")
	}
}

func TestDetect_NonTTY(t *testing.T) {
	cap := Detect(&bytes.Buffer{})
	if cap.IsTTY || cap.Color {
		t.Fatalf("expected non-TTY without color, got %+v", cap)
	}
}

func TestDetect_DumbTerminal(t *testing.T) {
	t.Setenv("TERM", "dumb")
	if Detect(&bytes.Buffer{}).Unicode {
		t.Fatal("expected Unicode=false for TERM=dumb")
	}
}

func TestDetect_FallbackWidth(t *testing.T) {
	cap := Detect(&bytes.Buffer{})
	if cap.Width < 40 || cap.Height < 20 {
		t.Fatalf("expected usable fallback dimensions, got %+v", cap)
	}
}
