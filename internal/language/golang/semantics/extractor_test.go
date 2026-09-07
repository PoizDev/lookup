package semantics_test

import (
	"context"
	"strings"
	"testing"

	golangadapter "github.com/poizdev/lookup/internal/language/golang"
	"github.com/poizdev/lookup/internal/language/golang/semantics"
	"github.com/poizdev/lookup/internal/semantic"
)

func TestExtractNormalizesGinGORMAndCommandSemantics(t *testing.T) {
	source := []byte(`package app
import (
  "context"
  "fmt"
  "os/exec"
  "github.com/gin-gonic/gin"
  "gorm.io/gorm"
)
func handler(c *gin.Context, db *gorm.DB) {
  id := c.Query("id")
  query := fmt.Sprintf("SELECT * FROM users WHERE id = %s", id)
  db.Raw(query)
  db.Where("id = ?", id).First(&user)
  exec.Command("sh", "-c", id)
  ctx, cancel := context.WithCancel(context.Background())
  defer cancel()
  go work(ctx)
}
func routes(r *gin.Engine) {
  admin := r.Group("/admin")
  admin.Use(AuthMiddleware())
  admin.GET("/users", handler)
}`)

	document := parseAndExtract(t, source)
	assertFact(t, document, semantic.FactHTTPInput, "http.query", "id")
	assertFact(t, document, semantic.FactPropagation, "fmt.Sprintf", "query")
	unsafeSQL := assertFact(t, document, semantic.FactSink, "sql.raw", "")
	if len(unsafeSQL.Inputs) != 1 || unsafeSQL.Inputs[0] != "query" {
		t.Fatalf("unsafe SQL inputs = %v", unsafeSQL.Inputs)
	}
	assertFact(t, document, semantic.FactDatabaseOperation, "gorm.where.parameterized", "")
	assertFact(t, document, semantic.FactSink, "command.exec", "")
	assertFact(t, document, semantic.FactResourceAcquire, "context.cancel", "cancel")
	assertFact(t, document, semantic.FactResourceRelease, "context.cancel", "")
	assertFact(t, document, semantic.FactConcurrencyOperation, "goroutine.start", "")
	assertFact(t, document, semantic.FactRoute, "http.route.group", "admin")
	route := assertFact(t, document, semantic.FactRoute, "http.route", "")
	if route.Metadata["method"] != "GET" || route.Metadata["path"] != "/users" || route.Metadata["group"] != "admin" || route.Metadata["handler"] != "handler" || route.Metadata["middleware_attached"] != "true" || route.Metadata["protection_semantics"] != "unknown" {
		t.Fatalf("route metadata = %#v", route.Metadata)
	}
	middleware := assertFact(t, document, semantic.FactMiddleware, "http.middleware", "")
	if middleware.Metadata["group"] != "admin" || middleware.Metadata["protection_semantics"] != "unknown" {
		t.Fatalf("middleware metadata = %#v", middleware.Metadata)
	}

	index := semantic.NewIndex([]*semantic.Document{document})
	if !index.HasEcosystem(semantic.EcosystemGin) || !index.HasEcosystem(semantic.EcosystemGORM) || !index.HasEcosystem(semantic.EcosystemOSExec) {
		t.Fatalf("ecosystem detection missing for imports %v", document.Imports)
	}
}

func TestExtractNormalizesFiberAndNetHTTPInputs(t *testing.T) {
	source := []byte(`package app
import (
  "net/http"
  "github.com/gofiber/fiber/v3"
)
func fiberHandler(c fiber.Ctx) {
  q := c.Query("q")
  p := c.Params("id")
  h := c.Get("X-Key")
  _, _, _ = q, p, h
}
func stdHandler(w http.ResponseWriter, r *http.Request) {
  id := r.URL.Query().Get("id")
  token := r.Header.Get("Authorization")
  _, _ = id, token
}`)

	document := parseAndExtract(t, source)
	operations := map[string]bool{}
	for _, fact := range document.Facts {
		if fact.Kind == semantic.FactHTTPInput {
			operations[fact.Operation] = true
		}
	}
	for _, operation := range []string{"http.query", "http.path", "http.header"} {
		if !operations[operation] {
			t.Errorf("missing %s in %v", operation, operations)
		}
	}
}

func TestExtractUsesReceiverProvenanceForFrameworkMethods(t *testing.T) {
	source := []byte(`package app
import (
  "github.com/gofiber/fiber/v3"
  "gorm.io/gorm"
)
type Client struct{}
func (c *Client) Get(key string) string { return key }
type Store struct{}
func (s *Store) Raw(value string) {}
func register(app *fiber.App, client *Client, store *Store) {
  app.Get("/users", handler)
  value := client.Get("not-an-http-header")
  store.Raw(value)
}
func handler(c fiber.Ctx) error { return nil }
var _ *gorm.DB
`)

	document := parseAndExtract(t, source)
	routes := facts(document, semantic.FactRoute, "http.route")
	if len(routes) != 1 || routes[0].Metadata["method"] != "Get" || routes[0].Metadata["path"] != "/users" {
		t.Fatalf("Fiber routes = %#v", routes)
	}
	if got := facts(document, semantic.FactHTTPInput, "http.header"); len(got) != 0 {
		t.Fatalf("unrelated Client.Get normalized as Fiber input: %#v", got)
	}
	if got := facts(document, semantic.FactSink, "sql.raw"); len(got) != 0 {
		t.Fatalf("unrelated Store.Raw normalized as GORM sink: %#v", got)
	}
}

func TestExtractComplexityUsesGoSyntaxNodesOnly(t *testing.T) {
	source := []byte(`package app
func complex(a, b bool) {
  fake := "while catch ? if for switch"
  // if while catch ?
  if a && b { fake = "used" }
  for a { break }
  switch fake { case "used": println(fake) }
}`)

	document := parseAndExtract(t, source)
	count := 0
	for _, fact := range document.Facts {
		if fact.Kind == semantic.FactControl {
			count++
		}
	}
	if count != 5 {
		t.Fatalf("control fact count = %d, want 5 (if, &&, for, switch, case)", count)
	}
}

func TestExtractEmitsCoreSecurityResourceAndConcurrencyFacts(t *testing.T) {
	source := []byte(`package app
import (
  "context"
  "crypto/md5"
  "crypto/sha1"
  "crypto/tls"
  "net/http"
  "os"
  "sync"
  "time"
)
func analyze(w http.ResponseWriter, r *http.Request, value any, wg *sync.WaitGroup) {
  resp, _ := http.Get("https://example.com")
  defer resp.Body.Close()
  file, err := os.Open("data.txt")
  _ = err
  defer file.Close()
  ticker := time.NewTicker(time.Second)
  defer ticker.Stop()
  _, cancel := context.WithCancel(context.Background())
  defer cancel()
  asserted := value.(string)
  _ = asserted
  _ = md5.New()
  _ = sha1.New()
  _ = &tls.Config{InsecureSkipVerify: true}
  for {
    defer file.Close()
    go work()
    go func() { wg.Add(1) }()
    break
  }
  http.Redirect(w, r, r.URL.Query().Get("next"), http.StatusFound)
}`)

	document := parseAndExtract(t, source)
	if got := facts(document, semantic.FactGuard, "type_assertion.unchecked"); len(got) != 1 {
		t.Fatalf("unchecked assertion fact count = %d, facts=%#v", len(got), got)
	}
	for _, expected := range []struct {
		kind      semantic.FactKind
		operation string
	}{
		{semantic.FactGuard, "error.ignored"},
		{semantic.FactGuard, "type_assertion.unchecked"},
		{semantic.FactResourceAcquire, "http.response.body"},
		{semantic.FactResourceAcquire, "file"},
		{semantic.FactResourceAcquire, "ticker"},
		{semantic.FactResourceAcquire, "context.cancel"},
		{semantic.FactResourceRelease, "close"},
		{semantic.FactResourceRelease, "stop"},
		{semantic.FactCryptoOperation, "crypto.weak.md5"},
		{semantic.FactCryptoOperation, "crypto.weak.sha1"},
		{semantic.FactCryptoOperation, "tls.insecure_skip_verify"},
		{semantic.FactConcurrencyOperation, "defer.in_loop"},
		{semantic.FactConcurrencyOperation, "goroutine.loop"},
		{semantic.FactConcurrencyOperation, "waitgroup.add_in_goroutine"},
		{semantic.FactSink, "http.redirect"},
	} {
		assertFact(t, document, expected.kind, expected.operation, "")
	}
}

func TestExtractDistinguishesGORMGlobalAndUnscopedDestructiveOperations(t *testing.T) {
	source := []byte(`package app
import "gorm.io/gorm"
func remove(db *gorm.DB) {
  db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&User{})
  db.Unscoped().Delete(&User{}, "id = ?", 7)
}`)
	document := parseAndExtract(t, source)
	assertFact(t, document, semantic.FactDatabaseOperation, "gorm.allow_global_update", "")
	assertFact(t, document, semantic.FactDatabaseOperation, "gorm.unscoped.delete", "")
}

func TestExtractIgnoredErrorUsesKnownErrorResultPosition(t *testing.T) {
	document := parseAndExtract(t, []byte(`package app
import "os"
func write(file *os.File, data []byte) error {
  var err error
  if _, err = file.Write(data); err != nil { return err }
  _, _ = file.Write(data)
  _ = file.Close()
  return nil
}`))
	facts := facts(document, semantic.FactGuard, "error.ignored")
	if len(facts) != 2 {
		t.Fatalf("ignored error facts = %#v, want Write error and Close error only", facts)
	}
	for _, fact := range facts {
		if strings.Contains(fact.Expression, "_, err =") {
			t.Fatalf("captured Write error was reported ignored: %#v", fact)
		}
	}
}

func TestExtractResolvesAliasedEcosystemImports(t *testing.T) {
	source := []byte(`package app
import (
  osexec "os/exec"
  g "github.com/gin-gonic/gin"
)
func handler(c *g.Context) {
  value := c.Query("cmd")
  osexec.Command("sh", "-c", value)
}`)
	document := parseAndExtract(t, source)
	assertFact(t, document, semantic.FactHTTPInput, "http.query", "value")
	assertFact(t, document, semantic.FactSink, "command.exec", "")
}

func parseAndExtract(t *testing.T, source []byte) *semantic.Document {
	t.Helper()
	adapter := &golangadapter.Adapter{}
	ast, err := adapter.Parse(context.Background(), source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	t.Cleanup(func() { _ = ast.Close() })
	ast.FilePath = "app.go"
	document, err := semantics.Extract(ast)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return document
}

func assertFact(t *testing.T, document *semantic.Document, kind semantic.FactKind, operation, output string) semantic.Fact {
	t.Helper()
	for _, fact := range document.Facts {
		if fact.Kind != kind || fact.Operation != operation {
			continue
		}
		if output == "" || contains(fact.Outputs, output) {
			return fact
		}
	}
	t.Fatalf("missing fact kind=%s operation=%s output=%s; facts=%#v", kind, operation, output, document.Facts)
	return semantic.Fact{}
}

func facts(document *semantic.Document, kind semantic.FactKind, operation string) []semantic.Fact {
	var result []semantic.Fact
	for _, fact := range document.Facts {
		if fact.Kind == kind && fact.Operation == operation {
			result = append(result, fact)
		}
	}
	return result
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
