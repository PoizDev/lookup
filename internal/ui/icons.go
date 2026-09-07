package ui

type IconSet struct {
	Success, Error, Warning, Info, Arrow, Bullet, Focus, Selected string
	Spinner                                                       []string
}

func Icons(unicode bool) IconSet {
	if unicode {
		return IconSet{"✓", "✘", "!", "•", "→", "◆", "┃", "✓", []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}}
	}
	return IconSet{"[OK]", "[ERR]", "[WARN]", "[INFO]", "->", "*", ">", "[x]", []string{"-", "\\", "|", "/"}}
}
