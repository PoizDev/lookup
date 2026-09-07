package language

import (
	"fmt"
	"sort"
	"sync"
)

type Stage int

const (
	StageParse Stage = iota + 1
	StageStructural
	StageSemantic
)

type StageOutcome struct {
	Supported bool
	Attempted bool
	Succeeded bool
}

type StageCoverage struct {
	Supported int
	Attempted int
	Succeeded int
}

type LangCoverage struct {
	Language   string
	Files      int
	Parse      StageCoverage
	Structural StageCoverage
	Semantic   StageCoverage
}

type FileID uint64

type fileAnalysis struct {
	language   string
	parse      StageOutcome
	structural StageOutcome
	semantic   StageOutcome
}

type CoverageCollector struct {
	mu     sync.RWMutex
	nextID FileID
	files  map[FileID]fileAnalysis
}

func NewCoverageCollector() *CoverageCollector {
	return &CoverageCollector{files: make(map[FileID]fileAnalysis)}
}

func (c *CoverageCollector) Discover(language string, capabilities Capabilities) FileID {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	c.files[c.nextID] = fileAnalysis{
		language:   language,
		parse:      StageOutcome{Supported: capabilities.Parse},
		structural: StageOutcome{Supported: capabilities.Structural},
		semantic:   StageOutcome{Supported: capabilities.Semantic},
	}
	return c.nextID
}

func (c *CoverageCollector) Attempt(id FileID, stage Stage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	file, ok := c.files[id]
	if !ok {
		return fmt.Errorf("coverage file %d is not registered", id)
	}
	outcome, err := stageOutcome(&file, stage)
	if err != nil {
		return err
	}
	if !outcome.Supported {
		return fmt.Errorf("%s stage is unsupported for %s", stage, file.language)
	}
	if outcome.Attempted {
		return fmt.Errorf("%s stage was already attempted for %s", stage, file.language)
	}
	if stage == StageStructural && !file.parse.Succeeded {
		return fmt.Errorf("structural stage requires successful parse for %s", file.language)
	}
	if stage == StageSemantic && !file.structural.Succeeded {
		return fmt.Errorf("semantic stage requires successful structural analysis for %s", file.language)
	}
	outcome.Attempted = true
	c.files[id] = file
	return nil
}

func (c *CoverageCollector) Succeed(id FileID, stage Stage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	file, ok := c.files[id]
	if !ok {
		return fmt.Errorf("coverage file %d is not registered", id)
	}
	outcome, err := stageOutcome(&file, stage)
	if err != nil {
		return err
	}
	if !outcome.Attempted {
		return fmt.Errorf("%s stage was not attempted for %s", stage, file.language)
	}
	if outcome.Succeeded {
		return fmt.Errorf("%s stage already succeeded for %s", stage, file.language)
	}
	outcome.Succeeded = true
	c.files[id] = file
	return nil
}

func stageOutcome(file *fileAnalysis, stage Stage) (*StageOutcome, error) {
	switch stage {
	case StageParse:
		return &file.parse, nil
	case StageStructural:
		return &file.structural, nil
	case StageSemantic:
		return &file.semantic, nil
	default:
		return nil, fmt.Errorf("unknown analysis stage %d", stage)
	}
}

func (s Stage) String() string {
	switch s {
	case StageParse:
		return "parse"
	case StageStructural:
		return "structural"
	case StageSemantic:
		return "semantic"
	default:
		return fmt.Sprintf("stage(%d)", s)
	}
}

func (c *CoverageCollector) Coverage() []LangCoverage {
	c.mu.RLock()
	defer c.mu.RUnlock()
	byLanguage := make(map[string]LangCoverage)
	for _, file := range c.files {
		coverage := byLanguage[file.language]
		coverage.Language = file.language
		coverage.Files++
		addStage(&coverage.Parse, file.parse)
		addStage(&coverage.Structural, file.structural)
		addStage(&coverage.Semantic, file.semantic)
		byLanguage[file.language] = coverage
	}
	result := make([]LangCoverage, 0, len(byLanguage))
	for _, coverage := range byLanguage {
		result = append(result, coverage)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Language < result[j].Language })
	return result
}

func addStage(total *StageCoverage, outcome StageOutcome) {
	if outcome.Supported {
		total.Supported++
	}
	if outcome.Attempted {
		total.Attempted++
	}
	if outcome.Succeeded {
		total.Succeeded++
	}
}
