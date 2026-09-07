package language

import "fmt"

type Language string

const (
	Go         Language = "Go"
	TypeScript Language = "TypeScript"
	JavaScript Language = "JavaScript"
	Python     Language = "Python"
	Rust       Language = "Rust"
	C          Language = "C"
	CPP        Language = "C++"
	CSharp     Language = "C#"
	Java       Language = "Java"
	Kotlin     Language = "Kotlin"
	Lua        Language = "Lua"
	PHP        Language = "PHP"
	Ruby       Language = "Ruby"
	Swift      Language = "Swift"
)

type SupportLevel int

const (
	LevelUnsupported SupportLevel = 0
	LevelParse       SupportLevel = 1 // Tree-sitter grammar exists, can read file
	LevelStructural  SupportLevel = 2 // Full Unified Code Graph transfer (all generic rules work)
	LevelSemantic    SupportLevel = 3 // Language-specific behavior analysis
)

// Capabilities describes which analysis stages an adapter can perform.
type Capabilities struct {
	Parse      bool
	Structural bool
	Semantic   bool
}

func (c Capabilities) Validate() error {
	if c.Structural && !c.Parse {
		return fmt.Errorf("structural capability requires parse capability")
	}
	if c.Semantic && !c.Structural {
		return fmt.Errorf("semantic capability requires structural capability")
	}
	return nil
}

func (c Capabilities) SupportLevel() SupportLevel {
	switch {
	case c.Semantic:
		return LevelSemantic
	case c.Structural:
		return LevelStructural
	case c.Parse:
		return LevelParse
	default:
		return LevelUnsupported
	}
}

func (s SupportLevel) String() string {
	switch s {
	case LevelUnsupported:
		return "Unsupported"
	case LevelParse:
		return "L1 (Parse)"
	case LevelStructural:
		return "L2 (Structural)"
	case LevelSemantic:
		return "L3 (Semantic)"
	default:
		return "Unknown"
	}
}
