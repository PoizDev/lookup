package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const sourceRepository = "github.com/poizdev/lookup"

func RenderVersion(cap Capability, version, commit, date string) string {
	styles := NewStyles(NewTheme(cap), cap)
	label := func(value string) string {
		return styles.Render(value, lipgloss.NewStyle().Foreground(styles.theme.Muted()))
	}
	versionValue := styles.Render(version, lipgloss.NewStyle().Foreground(styles.theme.Accent()))
	return fmt.Sprintf("%s\n\n%s   %s\n%s    %s\n%s     %s\n%s    %s\n",
		renderWordmark(cap, styles),
		label("Version"), versionValue,
		label("Commit"), displayCommit(commit),
		label("Built"), displayDate(date),
		label("Source"), sourceRepository,
	)
}

func displayCommit(value string) string {
	runes := []rune(value)
	if len(runes) > 12 {
		return string(runes[:12])
	}
	return value
}

func displayDate(value string) string {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, strings.TrimSpace(value)); err == nil {
			return parsed.Format("2006-01-02")
		}
	}
	return value
}
