package discovery

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Classifier struct{}

func NewClassifier() *Classifier {
	return &Classifier{}
}

func (c *Classifier) Classify(path string) string {
	base := strings.ToLower(filepath.Base(path))
	if base == "dockerfile" {
		return "Dockerfile"
	}
	if base == "makefile" || base == "gnumakefile" {
		return "Makefile"
	}

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "Go"
	case ".ts", ".tsx":
		return "TypeScript"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "JavaScript"
	case ".py", ".pyw":
		return "Python"
	case ".rs":
		return "Rust"
	case ".c", ".h":
		return "C"
	case ".cpp", ".cc", ".cxx", ".hpp", ".hxx", ".hh":
		return "C++"
	case ".cs":
		return "C#"
	case ".java":
		return "Java"
	case ".kt", ".kts":
		return "Kotlin"
	case ".lua":
		return "Lua"
	case ".php":
		return "PHP"
	case ".rb":
		return "Ruby"
	case ".swift":
		return "Swift"
	case ".sh", ".bash":
		return "Shell"
	case ".yaml", ".yml":
		return "YAML"
	case ".json":
		return "JSON"
	case ".toml":
		return "TOML"
	case ".md", ".markdown":
		return "Markdown"
	case ".sql":
		return "SQL"
	case ".proto":
		return "Protobuf"
	case ".dockerfile":
		return "Dockerfile"
	case ".makefile":
		return "Makefile"
	}

	// Shebang check
	if file, err := os.Open(path); err == nil {
		defer file.Close()
		buf := make([]byte, 256)
		n, _ := io.ReadFull(file, buf)
		if n > 2 && buf[0] == '#' && buf[1] == '!' {
			nlIdx := bytes.IndexByte(buf, '\n')
			if nlIdx == -1 {
				nlIdx = n
			}
			line := string(buf[:nlIdx])
			if strings.Contains(line, "python") {
				return "Python"
			}
			if strings.Contains(line, "node") {
				return "JavaScript"
			}
			if strings.Contains(line, "ruby") {
				return "Ruby"
			}
			if strings.Contains(line, "bash") || strings.Contains(line, "sh") {
				return "Shell"
			}
			if strings.Contains(line, "perl") {
				return "Perl"
			}
		}
	}

	return ""
}
