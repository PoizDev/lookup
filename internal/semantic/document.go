package semantic

import (
	"sort"
	"strings"
)

type Ecosystem string

const (
	EcosystemNetHTTP     Ecosystem = "net/http"
	EcosystemDatabaseSQL Ecosystem = "database/sql"
	EcosystemOSExec      Ecosystem = "os/exec"
	EcosystemCrypto      Ecosystem = "crypto"
	EcosystemTemplate    Ecosystem = "template"
	EcosystemFilesystem  Ecosystem = "filesystem"
	EcosystemGin         Ecosystem = "gin"
	EcosystemFiber       Ecosystem = "fiber"
	EcosystemGORM        Ecosystem = "gorm"
	EcosystemFlask       Ecosystem = "flask"
	EcosystemFastAPI     Ecosystem = "fastapi"
	EcosystemDjango      Ecosystem = "django"
	EcosystemSQLAlchemy  Ecosystem = "sqlalchemy"
	EcosystemPyTorch     Ecosystem = "pytorch"
	EcosystemOpenCV      Ecosystem = "opencv"
	EcosystemUltralytics Ecosystem = "ultralytics"
	EcosystemASPNetCore  Ecosystem = "aspnetcore"
	EcosystemEFCore      Ecosystem = "efcore"
	EcosystemTokio       Ecosystem = "tokio"
	EcosystemAxum        Ecosystem = "axum"
	EcosystemActix       Ecosystem = "actix-web"
	EcosystemSQLx        Ecosystem = "sqlx"
	EcosystemDiesel      Ecosystem = "diesel"
	EcosystemReqwest     Ecosystem = "reqwest"
	EcosystemTonic       Ecosystem = "tonic"
)

type Function struct {
	Name       string   `json:"name"`
	Parameters []string `json:"parameters,omitempty"`
	Location   Location `json:"location"`
}

type Document struct {
	Path      string     `json:"path"`
	Language  string     `json:"language"`
	Source    []byte     `json:"-"`
	Imports   []string   `json:"imports,omitempty"`
	Functions []Function `json:"functions,omitempty"`
	Facts     []Fact     `json:"facts,omitempty"`
}

type Index struct {
	documents  []*Document
	byKind     map[FactKind][]Fact
	ecosystems map[Ecosystem]struct{}
}

func NewIndex(documents []*Document) *Index {
	index := &Index{byKind: make(map[FactKind][]Fact), ecosystems: make(map[Ecosystem]struct{})}
	index.documents = append(index.documents, documents...)
	sort.Slice(index.documents, func(i, j int) bool { return index.documents[i].Path < index.documents[j].Path })
	for _, document := range index.documents {
		if document == nil {
			continue
		}
		for _, importPath := range document.Imports {
			if ecosystem, ok := ecosystemForImport(importPath); ok {
				index.ecosystems[ecosystem] = struct{}{}
			}
		}
		for _, fact := range document.Facts {
			index.byKind[fact.Kind] = append(index.byKind[fact.Kind], fact)
		}
	}
	for kind := range index.byKind {
		sortFacts(index.byKind[kind])
	}
	return index
}

func (i *Index) Documents() []*Document {
	documents := make([]*Document, len(i.documents))
	copy(documents, i.documents)
	return documents
}

func (i *Index) Facts(kind FactKind) []Fact {
	facts := make([]Fact, len(i.byKind[kind]))
	copy(facts, i.byKind[kind])
	return facts
}

func (i *Index) AllFacts() []Fact {
	var facts []Fact
	for _, values := range i.byKind {
		facts = append(facts, values...)
	}
	sortFacts(facts)
	return facts
}

func (i *Index) HasEcosystem(ecosystem Ecosystem) bool {
	_, ok := i.ecosystems[ecosystem]
	return ok
}

func (i *Index) Ecosystems() []Ecosystem {
	result := make([]Ecosystem, 0, len(i.ecosystems))
	for ecosystem := range i.ecosystems {
		result = append(result, ecosystem)
	}
	sort.Slice(result, func(a, b int) bool { return result[a] < result[b] })
	return result
}

// EcosystemsForImports returns deterministic ecosystem facts established for
// one semantic document rather than repository-wide assumptions.
func EcosystemsForImports(imports []string) []Ecosystem {
	seen := map[Ecosystem]struct{}{}
	for _, importPath := range imports {
		if ecosystem, ok := ecosystemForImport(importPath); ok {
			seen[ecosystem] = struct{}{}
		}
	}
	result := make([]Ecosystem, 0, len(seen))
	for ecosystem := range seen {
		result = append(result, ecosystem)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func ecosystemForImport(path string) (Ecosystem, bool) {
	switch {
	case path == "net/http":
		return EcosystemNetHTTP, true
	case path == "database/sql":
		return EcosystemDatabaseSQL, true
	case path == "os/exec":
		return EcosystemOSExec, true
	case strings.HasPrefix(path, "crypto/"):
		return EcosystemCrypto, true
	case path == "html/template" || path == "text/template":
		return EcosystemTemplate, true
	case path == "os" || path == "path/filepath":
		return EcosystemFilesystem, true
	case path == "github.com/gin-gonic/gin":
		return EcosystemGin, true
	case strings.HasPrefix(path, "github.com/gofiber/fiber/v"):
		return EcosystemFiber, true
	case path == "gorm.io/gorm":
		return EcosystemGORM, true
	case path == "flask" || strings.HasPrefix(path, "flask."):
		return EcosystemFlask, true
	case path == "fastapi" || strings.HasPrefix(path, "fastapi."):
		return EcosystemFastAPI, true
	case path == "django" || strings.HasPrefix(path, "django."):
		return EcosystemDjango, true
	case path == "sqlalchemy" || strings.HasPrefix(path, "sqlalchemy."):
		return EcosystemSQLAlchemy, true
	case path == "torch" || strings.HasPrefix(path, "torch."):
		return EcosystemPyTorch, true
	case path == "cv2" || strings.HasPrefix(path, "cv2."):
		return EcosystemOpenCV, true
	case path == "ultralytics" || strings.HasPrefix(path, "ultralytics."):
		return EcosystemUltralytics, true
	case strings.HasPrefix(path, "Microsoft.AspNetCore"):
		return EcosystemASPNetCore, true
	case path == "Microsoft.EntityFrameworkCore" || strings.HasPrefix(path, "Microsoft.EntityFrameworkCore."):
		return EcosystemEFCore, true
	case path == "tokio" || strings.HasPrefix(path, "tokio::"):
		return EcosystemTokio, true
	case path == "axum" || strings.HasPrefix(path, "axum::"):
		return EcosystemAxum, true
	case path == "actix_web" || strings.HasPrefix(path, "actix_web::"):
		return EcosystemActix, true
	case path == "sqlx" || strings.HasPrefix(path, "sqlx::"):
		return EcosystemSQLx, true
	case path == "diesel" || strings.HasPrefix(path, "diesel::"):
		return EcosystemDiesel, true
	case path == "reqwest" || strings.HasPrefix(path, "reqwest::"):
		return EcosystemReqwest, true
	case path == "tonic" || strings.HasPrefix(path, "tonic::"):
		return EcosystemTonic, true
	default:
		return "", false
	}
}

func sortFacts(facts []Fact) {
	sort.SliceStable(facts, func(i, j int) bool {
		left, right := facts[i], facts[j]
		if left.Location.File != right.Location.File {
			return left.Location.File < right.Location.File
		}
		if left.Location.StartLine != right.Location.StartLine {
			return left.Location.StartLine < right.Location.StartLine
		}
		if left.Location.StartColumn != right.Location.StartColumn {
			return left.Location.StartColumn < right.Location.StartColumn
		}
		return left.ID < right.ID
	})
}
