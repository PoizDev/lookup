package progress

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/poizdev/lookup/internal/ui"
)

type Phase string

const (
	PhaseDiscovery Phase = "Discovering"
	PhaseParsing   Phase = "Parsing"
	PhaseBuilding  Phase = "Building graph"
	PhaseAnalyzing Phase = "Analyzing"
	PhaseAIReview  Phase = "AI Review"
	PhaseDone      Phase = "Done"
)

var phaseNumber = map[Phase]int{PhaseDiscovery: 1, PhaseParsing: 2, PhaseBuilding: 3, PhaseAnalyzing: 4, PhaseAIReview: 5}

type Spinner struct {
	mu             sync.Mutex
	phase          Phase
	message        string
	current, total int
	quiet          bool
	writer         io.Writer
	cap            ui.Capability
	icons          ui.IconSet
	frameIdx       int
	ticker         *time.Ticker
	done           chan struct{}
	started        bool
	phaseStart     time.Time
}

func New(quiet bool, capabilities ...ui.Capability) *Spinner {
	cap := ui.Detect(os.Stderr)
	if len(capabilities) > 0 {
		cap = capabilities[0]
	}
	return &Spinner{phase: PhaseDiscovery, quiet: quiet, writer: os.Stderr, cap: cap, icons: ui.Icons(cap.Unicode), done: make(chan struct{}), phaseStart: time.Now()}
}
func (s *Spinner) Start() {
	if s.quiet || s.started {
		return
	}
	s.started = true
	if !s.cap.IsTTY {
		return
	}
	s.ticker = time.NewTicker(80 * time.Millisecond)
	go func() {
		for {
			select {
			case <-s.done:
				s.ticker.Stop()
				return
			case <-s.ticker.C:
				s.render()
			}
		}
	}()
}
func (s *Spinner) Stop() {
	if s.quiet || !s.started {
		return
	}
	s.mu.Lock()
	s.started = false
	if s.cap.IsTTY {
		close(s.done)
		fmt.Fprintf(s.writer, "\r%*s\r", s.cap.Width, "")
	}
	s.mu.Unlock()
}
func (s *Spinner) render() {
	s.mu.Lock()
	defer s.mu.Unlock()
	frames := s.icons.Spinner
	frame := frames[s.frameIdx%len(frames)]
	s.frameIdx++
	fmt.Fprintf(s.writer, "\r%s %s%s", frame, s.phase, s.detail())
}
func (s *Spinner) detail() string {
	if s.total > 0 {
		return fmt.Sprintf("... %s (%d/%d)", s.message, s.current, s.total)
	}
	if s.message != "" {
		return "... " + s.message
	}
	return "..."
}
func (s *Spinner) SetPhase(phase Phase, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started && s.phase != phase {
		elapsed := time.Since(s.phaseStart).Round(100 * time.Millisecond)
		if s.cap.IsTTY {
			fmt.Fprintf(s.writer, "\r%*s\r", s.cap.Width, "")
		}
		fmt.Fprintf(s.writer, "%s %s%s (%s)\n", s.icons.Success, s.phase, s.detail(), elapsed)
	}
	s.phase = phase
	s.message = detail
	s.current = 0
	s.total = 0
	s.phaseStart = time.Now()
	if s.started && !s.cap.IsTTY {
		fmt.Fprintf(s.writer, "[%d/5] %s%s\n", phaseNumber[phase], phase, s.detail())
	}
}
func (s *Spinner) SetProgress(current, total int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current = current
	s.total = total
}
func (s *Spinner) SetDetail(detail string) { s.mu.Lock(); defer s.mu.Unlock(); s.message = detail }
func (s *Spinner) Increment()              { s.mu.Lock(); defer s.mu.Unlock(); s.current++ }
