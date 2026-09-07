package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// TerminalWordmark is the static runtime copy of docs/ascii.txt. Keep changes
// byte-for-byte in sync; brand_parity_test.go enforces parity.
const TerminalWordmark = `⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⣶⣶⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⢰⣶⡆⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⣀⣀⡀⠀⢀⣠⣤⣀⣀⠀⠀⠀
⢠⡀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⢀⣶⣶⠄⠀⠀⠀⠀⣿⣿⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⢸⣿⡇⠀⠀⠀⠀⠀⠀⣀⣀⠀⠀⠀⠀⠀⠀⣀⣀⠿⢿⣧⣼⠿⠛⠛⠻⢟⣷⣄⠀
⠘⣧⠀⠀⠀⠀⠀⣀⣤⡀⠀⠀⣼⣟⡟⠀⠀⠀⠀⠀⣿⣿⠀⠀⣠⣴⣶⣿⣷⣶⣤⡀⠀⠀⣀⣴⣶⣾⣿⣶⣤⣀⠀⢸⣿⡇⠀⠀⠀⣠⣶⡶⣿⣻⠀⠀⠀⠀⠀⠀⣿⣻⠀⢸⣯⡇⠀⠀⠀⠀⠀⢻⢿⡄
⠀⢹⡆⠀⠀⠀⢰⣿⠿⣿⣦⢰⣿⡻⠀⠀⠀⠀⠀⠀⣿⣿⢀⣾⣿⠏⠁⠀⠀⠉⢿⣿⡆⣼⣿⠟⠃⠀⠀⠉⠻⣿⣦⢸⣿⡇⠀⣤⣾⣿⠏⠀⣿⣽⠀⠀⠀⠀⠀⠀⣿⣽⠀⢸⣿⠆⠀⠀⠀⠀⠀⢸⣿⡇
⠀⠀⣿⡄⠀⢀⣿⡟⠄⠹⣿⣿⡿⠁⠀⠀⠀⠀⠀⠀⣿⣿⢸⣿⡇⠀⠀⠀⠀⠀⠈⣿⣿⣿⡏⠀⠀⠀⠀⠀⠀⣿⣿⣼⣿⣧⣾⣿⣅⠀⠀⠀⣿⢾⠀⠀⠀⠀⠀⠀⣿⣾⠀⢸⣿⣧⡀⠀⠀⠀⣠⣿⢾⠁
⠀⠀⢸⣧⠀⣼⡿⠁⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⣿⣿⢸⣿⣇⠀⠀⠀⠀⠀⢀⣿⡿⣿⣧⠀⠀⠀⠀⠀⠀⣿⣿⢻⣿⡟⠁⠻⣿⣦⡀⠀⢻⣟⣇⠀⠀⠀⠀⣰⣿⣽⡀⢺⡿⡜⠿⣷⣶⣿⡻⠝⠁⠀
⠀⠀⠀⢿⣧⣿⠃⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⣿⣿⠀⠻⣿⣦⣄⣀⣠⣤⣾⡿⠃⠹⣿⣷⣤⣀⣠⣤⣾⣿⠇⢸⣿⡇⠀⠀⠈⠻⣿⣦⠀⠙⢿⣻⣷⣿⡿⠛⠈⣿⣻⢸⣟⡇⠀⠀⠀⠀⠀⠀⠀⠀
⠀⠀⠀⠀⠉⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠛⠛⠂⠀⠈⠙⠛⠛⠛⠛⠉⠀⠀⠀⠀⠉⠛⠛⠛⠛⠉⠀⠀⠘⠛⠓⠀⠀⠀⠀⠙⠛⠛⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⣹⣯⡇⠀⠀⠀⠀⠀⠀⠀⠀
`

func WordmarkWidth() int {
	width := 0
	for _, row := range strings.Split(strings.TrimSuffix(TerminalWordmark, "\n"), "\n") {
		if rowWidth := lipgloss.Width(row); rowWidth > width {
			width = rowWidth
		}
	}
	return width
}

func renderWordmark(cap Capability, styles *Styles) string {
	if cap.IsTTY && cap.Unicode && cap.Width >= WordmarkWidth() {
		return strings.TrimSuffix(TerminalWordmark, "\n")
	}
	return styles.Render("lookup", lipgloss.NewStyle().Bold(true).Foreground(styles.theme.Accent()))
}
