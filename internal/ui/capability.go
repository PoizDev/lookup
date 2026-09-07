package ui

import (
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/mattn/go-isatty"
)

// Capability describes features supported by an output terminal.
type Capability struct {
	Color   bool
	Unicode bool
	IsTTY   bool
	Width   int
	Height  int
}

// Detect inspects a writer and environment without writing to it.
func Detect(w io.Writer) Capability {
	cap := Capability{Width: 80, Height: 24}
	if f, ok := w.(interface{ Fd() uintptr }); ok {
		fd := f.Fd()
		cap.IsTTY = isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
		if cap.IsTTY {
			if width, height, err := term.GetSize(fd); err == nil && width > 0 {
				cap.Width = width
				if height > 0 {
					cap.Height = height
				}
			}
		}
	}
	_, noColor := os.LookupEnv("NO_COLOR")
	cap.Color = cap.IsTTY && !noColor
	terminal := strings.TrimSpace(os.Getenv("TERM"))
	cap.Unicode = terminal != "" && !strings.EqualFold(terminal, "dumb")
	if terminal == "" && cap.IsTTY {
		cap.Unicode = true
	}
	return cap
}
