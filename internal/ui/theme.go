package ui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/poizdev/lookup/internal/finding"
)

var severityColors = map[finding.Severity]string{
	finding.SeverityCritical: "#EF4444", finding.SeverityHigh: "#F97316",
	finding.SeverityMedium: "#F59E0B", finding.SeverityLow: "#3B82F6",
	finding.SeverityInfo: "#6B7280",
}

type Theme struct{ enabled bool }

const (
	lookupAccent = "#F70E39"
	mutedColor   = "#A1A1AA"
	borderColor  = "#71717A"
	successColor = "#22C55E"
	warningColor = "#F59E0B"
	errorColor   = "#EF4444"
)

func NewTheme(cap Capability) *Theme { return &Theme{enabled: cap.Color} }
func (t *Theme) Enabled() bool       { return t.enabled }

func (t *Theme) SeverityColor(severity finding.Severity) lipgloss.Color {
	if !t.enabled {
		return ""
	}
	return lipgloss.Color(severityColors[severity])
}
func (t *Theme) ScoreColor(score float64) lipgloss.Color {
	if !t.enabled {
		return ""
	}
	switch {
	case score >= 80:
		return lipgloss.Color(successColor)
	case score >= 60:
		return lipgloss.Color(warningColor)
	case score >= 40:
		return "#F97316"
	default:
		return lipgloss.Color(errorColor)
	}
}
func (t *Theme) Primary() lipgloss.Color {
	if !t.enabled {
		return ""
	}
	return lipgloss.Color(lookupAccent)
}
func (t *Theme) Accent() lipgloss.Color { return t.Primary() }
func (t *Theme) Muted() lipgloss.Color {
	if !t.enabled {
		return ""
	}
	return lipgloss.Color(mutedColor)
}
func (t *Theme) Border() lipgloss.Color {
	if !t.enabled {
		return ""
	}
	return lipgloss.Color(borderColor)
}
func (t *Theme) Focused() lipgloss.Color  { return t.Accent() }
func (t *Theme) Selected() lipgloss.Color { return t.Muted() }
func (t *Theme) Success() lipgloss.Color {
	if !t.enabled {
		return ""
	}
	return lipgloss.Color(successColor)
}
func (t *Theme) Warning() lipgloss.Color {
	if !t.enabled {
		return ""
	}
	return lipgloss.Color(warningColor)
}
func (t *Theme) Error() lipgloss.Color {
	if !t.enabled {
		return ""
	}
	return lipgloss.Color(errorColor)
}
