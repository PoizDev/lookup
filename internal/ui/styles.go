package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/poizdev/lookup/internal/finding"
)

type Styles struct {
	theme *Theme
	cap   Capability
}

func NewStyles(theme *Theme, cap Capability) *Styles { return &Styles{theme: theme, cap: cap} }
func (s *Styles) Heading() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true)
}
func (s *Styles) Strong() lipgloss.Style { return lipgloss.NewStyle().Bold(true) }
func (s *Styles) Muted() lipgloss.Style  { return lipgloss.NewStyle().Foreground(s.theme.Muted()) }
func (s *Styles) Focused() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(s.theme.Focused())
}
func (s *Styles) Selected() lipgloss.Style { return lipgloss.NewStyle().Foreground(s.theme.Selected()) }
func (s *Styles) Success() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(s.theme.Success())
}
func (s *Styles) Warning() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(s.theme.Warning())
}
func (s *Styles) Error() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(s.theme.Error())
}
func (s *Styles) Disabled() lipgloss.Style           { return s.Muted() }
func (s *Styles) RenderHeading(value string) string  { return s.Render(value, s.Heading()) }
func (s *Styles) RenderStrong(value string) string   { return s.Render(value, s.Strong()) }
func (s *Styles) RenderFocused(value string) string  { return s.Render(value, s.Focused()) }
func (s *Styles) RenderSelected(value string) string { return s.Render(value, s.Selected()) }
func (s *Styles) RenderMuted(value string) string    { return s.Render(value, s.Muted()) }
func (s *Styles) RenderSuccess(value string) string  { return s.Render(value, s.Success()) }
func (s *Styles) RenderWarning(value string) string  { return s.Render(value, s.Warning()) }
func (s *Styles) RenderError(value string) string    { return s.Render(value, s.Error()) }
func (s *Styles) SeverityBadge(v finding.Severity) lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(s.theme.SeverityColor(v))
}
func (s *Styles) ScoreStyle(v float64) lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(s.theme.ScoreColor(v))
}
func (s *Styles) Divider(width int) string {
	if width <= 0 {
		width = s.cap.Width
	}
	char := "-"
	if s.cap.Unicode {
		char = "━"
	}
	return strings.Repeat(char, width)
}
func (s *Styles) Box(value string, width int) string {
	if max := s.cap.Width - 2; max > 0 && width > max {
		width = max
	}
	if width > 72 {
		width = 72
	}
	if width < 20 {
		width = 20
	}
	style := lipgloss.NewStyle().Padding(0, 1).Width(width)
	if s.cap.Unicode {
		style = style.Border(lipgloss.RoundedBorder()).BorderForeground(s.theme.Border())
	}
	return s.Render(value, style)
}
func (s *Styles) Card(value string, width int) string {
	if max := s.cap.Width - 4; max > 0 && width > max {
		width = max
	}
	if width > 60 {
		width = 60
	}
	if width < 20 {
		width = 20
	}
	border := lipgloss.RoundedBorder()
	if !s.cap.Unicode {
		border = lipgloss.Border{Top: "-", Bottom: "-", Left: "|", Right: "|", TopLeft: "+", TopRight: "+", BottomLeft: "+", BottomRight: "+"}
	}
	style := lipgloss.NewStyle().Padding(0, 1).Width(width).Border(border)
	if s.cap.Color {
		style = style.BorderForeground(s.theme.Border())
	}
	return style.Render(value)
}
func (s *Styles) Render(value string, style lipgloss.Style) string {
	if !s.cap.Color {
		return value
	}
	return style.Render(value)
}
