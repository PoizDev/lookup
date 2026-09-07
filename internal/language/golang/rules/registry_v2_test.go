package rules_test

import (
	"context"
	"strings"
	"testing"

	"github.com/poizdev/lookup/internal/graph"
	"github.com/poizdev/lookup/internal/language"
	golangadapter "github.com/poizdev/lookup/internal/language/golang"
	"github.com/poizdev/lookup/internal/language/golang/rules"
)

func TestAllV2RulesHaveUniqueNamespacedIDs(t *testing.T) {
	all := rules.All()
	if len(all) < 30 {
		t.Fatalf("rule count = %d, want at least 30 high-value rules", len(all))
	}
	seen := map[string]bool{}
	for _, rule := range all {
		id := rule.ID()
		if seen[id] {
			t.Fatalf("duplicate rule ID %q", id)
		}
		seen[id] = true
		if id != "ARC-001" && !strings.Contains(id, "-") {
			t.Fatalf("unscoped rule ID %q", id)
		}
	}
}

func TestMeasurementRulesAreStaticAuthoritative(t *testing.T) {
	for _, rule := range rules.All() {
		if rule.ID() != "GO-MNT-001" && rule.ID() != "GO-MNT-002" {
			continue
		}
		policy, ok := rule.(interface{ ReviewPolicy() language.ReviewPolicy })
		if !ok || policy.ReviewPolicy() != language.ReviewPolicyStaticAuthoritative {
			t.Fatalf("rule %s is not static authoritative", rule.ID())
		}
	}
}

func TestGORMTaintRuleDistinguishesDynamicAndParameterizedSQL(t *testing.T) {
	unsafe := analyze(t, `package app
import (
  "fmt"
  "github.com/gin-gonic/gin"
  "gorm.io/gorm"
)
func handler(c *gin.Context, db *gorm.DB) {
  id := c.Query("id")
  query := fmt.Sprintf("SELECT * FROM users WHERE id = %s", id)
  db.Raw(query)
}`)
	findings := findingsByID(unsafe, "GIN-SEC-001", "GORM-SEC-001")
	if len(findings) != 1 {
		t.Fatalf("deduplicated Gin/GORM SQL findings = %#v", findings)
	}
	if findings[0].RuleID != "GIN-SEC-001" {
		t.Fatalf("framework-specific SQL finding = %#v", findings[0])
	}
	if findings[0].Confidence != 0.90 || len(findings[0].EvidenceSteps) != 3 {
		t.Fatalf("assessment/evidence = %#v", findings[0])
	}

	safe := analyze(t, `package app
import (
  "github.com/gin-gonic/gin"
  "gorm.io/gorm"
)
func handler(c *gin.Context, db *gorm.DB) {
  id := c.Query("id")
  db.Where("id = ?", id).First(&User{})
  db.Raw("SELECT * FROM users WHERE id = ?", id)
}`)
	if got := findingsByID(safe, "GIN-SEC-001", "GORM-SEC-001"); len(got) != 0 {
		t.Fatalf("safe parameterization findings = %#v", got)
	}
}

func TestSQLClassificationSeparatesStaticParameterizedAndMixedDynamicQueries(t *testing.T) {
	static := analyze(t, `package app
import "gorm.io/gorm"
func maintain(db *gorm.DB) {
  db.Raw("SELECT 1")
  db.Exec("VACUUM")
}`)
	if got := findingsByID(static, "GORM-SEC-001"); len(got) != 0 {
		t.Fatalf("static SQL findings = %#v", got)
	}

	boundValue := analyze(t, `package app
import (
  "github.com/gin-gonic/gin"
  "gorm.io/gorm"
)
func handler(c *gin.Context, db *gorm.DB) {
  query := "SELECT * FROM users WHERE id = ?"
  id := c.Query("id")
  db.Raw(query, id)
  db.Where(map[string]any{"id": id}).First(&User{})
}`)
	if got := findingsByID(boundValue, "GIN-SEC-001", "GORM-SEC-001"); len(got) != 0 {
		t.Fatalf("bound value/builder findings = %#v", got)
	}

	mixed := analyze(t, `package app
import (
  "github.com/gin-gonic/gin"
  "gorm.io/gorm"
)
func handler(c *gin.Context, db *gorm.DB) {
  id := c.Query("id")
  db.Raw("SELECT * FROM users WHERE active = ? AND id = " + id, true)
}`)
	if got := findingsByID(mixed, "GIN-SEC-001"); len(got) != 1 {
		t.Fatalf("mixed dynamic SQL findings = %#v", got)
	}
}

func TestFrameworkHTTPRulesShareFactsWithoutCopyingExtraction(t *testing.T) {
	ginGraph := analyze(t, `package app
import (
  "os/exec"
  "github.com/gin-gonic/gin"
)
func handler(c *gin.Context) {
  arg := c.Query("arg")
  exec.Command("sh", "-c", arg)
  c.Redirect(302, c.Query("next"))
}`)
	for _, id := range []string{"GIN-SEC-002", "GIN-SEC-003"} {
		if got := findingsByID(ginGraph, id); len(got) != 1 {
			t.Errorf("%s findings = %#v", id, got)
		}
	}

	fiberGraph := analyze(t, `package app
import (
  "os/exec"
  "github.com/gofiber/fiber/v3"
)
func handler(c fiber.Ctx) error {
  arg := c.Params("arg")
  exec.Command("sh", "-c", arg)
  return c.Redirect(c.Query("next"))
}`)
	for _, id := range []string{"FIBER-SEC-002", "FIBER-SEC-003"} {
		if got := findingsByID(fiberGraph, id); len(got) != 1 {
			t.Errorf("%s findings = %#v", id, got)
		}
	}
}

func TestCommandRulesDistinguishOrdinaryArgvFromShellAndExecutableControl(t *testing.T) {
	ordinary := analyze(t, `package app
import (
  "os/exec"
  "github.com/gin-gonic/gin"
)
func handler(c *gin.Context) { exec.Command("grep", c.Query("pattern")) }
`)
	if got := findingsByID(ordinary, "GIN-SEC-002"); len(got) != 0 {
		t.Fatalf("ordinary argv findings = %#v", got)
	}

	dangerous := analyze(t, `package app
import (
  "os/exec"
  "github.com/gin-gonic/gin"
)
func handler(c *gin.Context) {
  exec.Command("sh", "-c", c.Query("command"))
  exec.Command(c.Query("binary"), "fixed")
}
`)
	if got := findingsByID(dangerous, "GIN-SEC-002"); len(got) != 2 {
		t.Fatalf("dangerous command findings = %#v", got)
	}
}

func TestFilesystemAndEnvironmentTaintRulesUseNormalizedSources(t *testing.T) {
	g := analyze(t, `package app
import (
  "os"
  "os/exec"
  "github.com/gin-gonic/gin"
)
func handler(c *gin.Context) {
  _, _ = os.ReadFile(c.Query("path"))
  command := os.Getenv("LOOKUP_COMMAND")
  exec.Command("sh", "-c", command)
}
`)
	for _, id := range []string{"GO-FS-SEC-001", "GO-CMD-SEC-002"} {
		if got := findingsByID(g, id); len(got) != 1 {
			t.Errorf("%s findings = %#v", id, got)
		}
	}

	contentOnly := analyze(t, `package app
import (
  "os"
  "github.com/gin-gonic/gin"
)
func handler(c *gin.Context) {
  _ = os.WriteFile("/tmp/fixed", []byte(c.Query("content")), 0600)
  _, _ = os.OpenFile("/tmp/fixed", 2, 0600)
}
`)
	for _, id := range []string{"GO-FS-SEC-001", "GO-SEC-005"} {
		if got := findingsByID(contentOnly, id); len(got) != 0 {
			t.Errorf("fixed path/mode near-miss %s findings = %#v", id, got)
		}
	}
}

func TestResourceRulesDoNotFlagReleasedResources(t *testing.T) {
	closed := analyze(t, `package app
import (
  "context"
  "net/http"
  "os"
  "time"
)
func ok() {
  resp, _ := http.Get("https://example.com")
  defer resp.Body.Close()
  file, _ := os.Open("data.txt")
  defer file.Close()
  ticker := time.NewTicker(time.Second)
  defer ticker.Stop()
  _, cancel := context.WithCancel(context.Background())
  defer cancel()
}`)
	if got := findingsByPrefix(closed, "GO-RES-"); len(got) != 0 {
		t.Fatalf("released resource findings = %#v", got)
	}

	leaked := analyze(t, `package app
import (
  "context"
  "net/http"
  "os"
  "time"
)
func leak() {
  resp, _ := http.Get("https://example.com")
  file, _ := os.Open("data.txt")
  ticker := time.NewTicker(time.Second)
  _, cancel := context.WithCancel(context.Background())
  _, _, _, _ = resp, file, ticker, cancel
}`)
	for _, id := range []string{"GO-RES-001", "GO-RES-003", "GO-RES-004", "GO-RES-005"} {
		if got := findingsByID(leaked, id); len(got) != 1 {
			t.Errorf("%s findings = %#v", id, got)
		}
	}
}

func TestStabilizationLifecycleAndScopeRegressions(t *testing.T) {
	safe := analyze(t, `package app
import ("context"; "os"; "sync")
type owner struct { file *os.File; cancel context.CancelFunc }
type lockedOwner struct { mu sync.RWMutex }
func (o *owner) Close() error { o.cancel(); return o.file.Close() }
func transferred() *owner {
  file, _ := os.Open("data")
  _, cancel := context.WithCancel(context.Background())
  return &owner{file: file, cancel: cancel}
}
func locked(mu *sync.Mutex) { mu.Lock(); mu.Unlock(); mu.Lock(); defer mu.Unlock() }
func lockedField(o *lockedOwner) { o.mu.Lock(); o.mu.Unlock() }
func scoped(files []*os.File) { for _, file := range files { func() { defer file.Close() }() } }
`)
	for _, id := range []string{"GO-RES-003", "GO-RES-005", "GO-CON-002", "GO-CON-004"} {
		if got := findingsByID(safe, id); len(got) != 0 {
			t.Errorf("safe %s findings = %#v", id, got)
		}
	}

	unsafe := analyze(t, `package app
import ("context"; "os"; "sync")
func leaks(mu *sync.Mutex) {
  mu.Lock()
  file, _ := os.Open("data")
  _, cancel := context.WithCancel(context.Background())
  _, _ = file, cancel
  for { defer println("later"); break }
}`)
	for _, id := range []string{"GO-RES-003", "GO-RES-005", "GO-CON-002", "GO-CON-004"} {
		if got := findingsByID(unsafe, id); len(got) != 1 {
			t.Errorf("unsafe %s findings = %#v", id, got)
		}
	}
}

func TestTypeAssertionCommaOKAcceptsNonIdentifierFirstTargets(t *testing.T) {
	g := analyze(t, `package app
type holder struct { Field string }
func checked(x any, arr []string, m map[string]string, obj *holder) {
  value, ok := x.(string); _ = value; _ = ok
  arr[0], ok = x.(string)
  m["k"], ok = x.(string)
  obj.Field, ok = x.(string)
}
func unchecked(x any) { value := x.(string); _ = value }
`)
	if got := findingsByID(g, "GO-COR-002"); len(got) != 1 {
		t.Fatalf("unchecked assertion findings = %#v", got)
	}
}

func TestContextSensitiveHashLoopAndCommandRules(t *testing.T) {
	safe := analyze(t, `package app
import ("crypto/sha1"; "net"; "os"; "os/exec")
func checksum(content []byte) { _ = sha1.Sum(content) }
func serve(listener net.Listener) { for { conn, _ := listener.Accept(); go handle(conn) } }
func isAllowed(command string) bool { return command == "git-upload-pack" }
func allowed() { command := os.Getenv("SSH_ORIGINAL_COMMAND"); if !isAllowed(command) { return }; exec.Command(command, "fixed") }
`)
	for _, id := range []string{"GO-SEC-003", "GO-CON-001", "GO-CMD-SEC-002"} {
		if got := findingsByID(safe, id); len(got) != 0 {
			t.Errorf("safe %s findings = %#v", id, got)
		}
	}
	unsafe := analyze(t, `package app
import ("crypto/sha1"; "os"; "os/exec")
func hashPassword(password []byte) { _ = sha1.Sum(password) }
func batch(items []int) { for range items { go work() } }
func shell() { command := os.Getenv("SSH_ORIGINAL_COMMAND"); exec.Command("sh", "-c", command) }
`)
	for _, id := range []string{"GO-SEC-003", "GO-CON-001", "GO-CMD-SEC-002"} {
		if got := findingsByID(unsafe, id); len(got) != 1 {
			t.Errorf("unsafe %s findings = %#v", id, got)
		}
	}
}

func TestSHA1KAnonymityPrefixProtocolIsNotSecurityFinding(t *testing.T) {
	g := analyze(t, `package app
import ("crypto/sha1"; "encoding/hex")
func checkPassword(password string) {
  sum := sha1.Sum([]byte(password))
  encoded := hex.EncodeToString(sum[:])
  prefix, suffix := encoded[:5], encoded[5:]
  _, _ = prefix, suffix
}`)
	if got := findingsByID(g, "GO-SEC-003"); len(got) != 0 {
		t.Fatalf("k-anonymity protocol findings = %#v", got)
	}
}

func TestCoreRulesUseASTFactsAndRespectSafeNearMisses(t *testing.T) {
	unsafe := analyze(t, `package app
import (
  "crypto/md5"
  "crypto/tls"
  "net/http"
  "sync"
)
func risky(value any, wg *sync.WaitGroup) {
  _, _ = http.Get("https://example.com")
  asserted := value.(string)
  _ = asserted
  _ = md5.Sum([]byte("password"))
  _ = &tls.Config{InsecureSkipVerify: true}
  for {
    defer println("later")
    go func() { wg.Add(1) }()
    break
  }
}`)
	for _, id := range []string{"GO-COR-001", "GO-COR-002", "GO-CON-001", "GO-CON-002", "GO-CON-003", "GO-SEC-002", "GO-SEC-004"} {
		if got := findingsByID(unsafe, id); len(got) == 0 {
			t.Errorf("expected %s finding", id)
		} else if got[0].Observation == "" || got[0].Hypothesis == "" {
			t.Errorf("context-required %s relies on normalization fallback: %#v", id, got[0])
		}
	}

	safe := analyze(t, `package app
import (
  "crypto/sha256"
  "crypto/tls"
)
func safe(value any) {
  asserted, ok := value.(string)
  if !ok { return }
  _ = sha256.Sum256([]byte(asserted))
  _ = &tls.Config{InsecureSkipVerify: false, MinVersion: tls.VersionTLS13}
}`)
	for _, id := range []string{"GO-COR-002", "GO-SEC-002", "GO-SEC-003", "GO-SEC-004"} {
		if got := findingsByID(safe, id); len(got) != 0 {
			t.Errorf("safe near-miss %s findings = %#v", id, got)
		}
	}
}

func TestConcurrencyRulesRequireGoReceiverAndUnboundedLoopContext(t *testing.T) {
	g := analyze(t, `package app
type Counter struct{}
func (*Counter) Add(int) {}
func (*Counter) Lock() {}
func (*Counter) Unlock() {}
func safe(counter *Counter) {
  go func() { counter.Add(1) }()
  counter.Lock()
  for i := 0; i < 2; i++ { go work(i) }
}
`)
	for _, id := range []string{"GO-CON-001", "GO-CON-003", "GO-CON-004"} {
		if got := findingsByID(g, id); len(got) != 0 {
			t.Errorf("receiver/bounded loop near-miss %s findings = %#v", id, got)
		}
	}
}

func TestConcurrencyRuleDistinguishesCollectionRangeFromOpenEndedLoop(t *testing.T) {
	g := analyze(t, `package app
func finite(hooks []func()) {
  for _, hook := range hooks { go hook() }
}
func openEnded() {
  for { go work() }
}
func channelBacked(ch <-chan func()) {
  for work := range ch { go work() }
}`)
	got := findingsByID(g, "GO-CON-001")
	if len(got) != 3 {
		t.Fatalf("GO-CON-001 findings = %#v", got)
	}
	for _, item := range got {
		text := strings.ToLower(item.Title + " " + item.Observation + " " + item.Hypothesis)
		if strings.Contains(item.CodeSnippet, "hook") && strings.Contains(text, "unbounded") {
			t.Fatalf("finite collection range represented as unbounded: %#v", item)
		}
		if strings.Contains(item.CodeSnippet, "work") && !strings.Contains(text, "unbounded") {
			t.Fatalf("open-ended loop lost unbounded hypothesis: %#v", item)
		}
	}
}

func TestConcurrencyNumericBoundRequiresCanonicalClauseAndAllEnclosingLoops(t *testing.T) {
	g := analyze(t, `package app
func canonical() {
  for i := 0; i < 10; i++ { go work() }
}
func bodyComparison(attempts int) {
  for { if attempts < 10 { go work() } }
}
func missingUpdate() {
  for i := 0; i < 10; { go work(); i = i }
}
func wrongUpdate() {
  for i, j := 0, 0; i < 10; j++ { go work(); _ = i }
}
func nestedOpenOuter() {
  for { for i := 0; i < 10; i++ { go work() } }
}`)
	got := findingsByID(g, "GO-CON-001")
	if len(got) != 4 {
		t.Fatalf("GO-CON-001 findings = %#v, want four loops without a proven total bound", got)
	}
}

func TestConcurrencyBoundClassificationUsesExecutableLoopChain(t *testing.T) {
	g := analyze(t, `package app
func nestedFiniteInsideOpen(hooks []func()) {
  for { for _, hook := range hooks { go hook() } }
}
func storedClosure() {
  for {
    f := func() { for i := 0; i < 10; i++ { go work() } }
    _ = f
  }
}
func invokedClosure() {
  for { ((func() { for i := 0; i < 10; i++ { go work() } }))() }
}`)
	got := findingsByID(g, "GO-CON-001")
	if len(got) != 2 {
		t.Fatalf("GO-CON-001 findings = %#v, want nested open range and immediately invoked closure", got)
	}
	for _, item := range got {
		text := strings.ToLower(item.Observation + " " + item.Hypothesis)
		if !strings.Contains(text, "unbounded") {
			t.Fatalf("open executable ancestor concealed by finite inner loop: %#v", item)
		}
	}
}

func TestIgnoredErrorRuleDoesNotTreatGinContextLookupAsError(t *testing.T) {
	g := analyze(t, `package app
import "github.com/gin-gonic/gin"
func handler(c *gin.Context) {
  value, _ := c.Get("identity")
	_ = value
}`)
	if got := findingsByID(g, "GO-COR-001"); len(got) != 0 {
		t.Fatalf("Gin context lookup findings = %#v", got)
	}
}

func TestIgnoredErrorRuleIncludesKnownExpressionStatementCalls(t *testing.T) {
	g := analyze(t, `package app
import (
  "net/http"
  "os"
)
func ignored() {
  http.Get("https://example.invalid")
  os.WriteFile("data", []byte("x"), 0600)
}
`)
	if got := findingsByID(g, "GO-COR-001"); len(got) != 2 {
		t.Fatalf("expression statement error findings = %#v", got)
	}
}

func TestRowsScannerAndTransactionRulesRequireMissingLifecycleChecks(t *testing.T) {
	unsafe := analyze(t, `package app
import (
  "bufio"
  "database/sql"
  "strings"
)
func query(db *sql.DB) {
  rows, _ := db.Query("SELECT id FROM users")
  defer rows.Close()
  for rows.Next() {}
  scanner := bufio.NewScanner(strings.NewReader("x"))
  for scanner.Scan() {}
  tx, _ := db.Begin()
  _ = tx
}`)
	for _, id := range []string{"GO-COR-003", "GO-COR-004", "GO-COR-005"} {
		if got := findingsByID(unsafe, id); len(got) != 1 {
			t.Errorf("%s findings = %#v", id, got)
		}
	}

	safe := analyze(t, `package app
import (
  "bufio"
  "database/sql"
  "strings"
)
func query(db *sql.DB) {
  rows, _ := db.Query("SELECT id FROM users")
  defer rows.Close()
  for rows.Next() {}
  _ = rows.Err()
  scanner := bufio.NewScanner(strings.NewReader("x"))
  for scanner.Scan() {}
  _ = scanner.Err()
  tx, _ := db.Begin()
  defer tx.Rollback()
}`)
	for _, id := range []string{"GO-COR-003", "GO-COR-004", "GO-COR-005", "GO-RES-002"} {
		if got := findingsByID(safe, id); len(got) != 0 {
			t.Errorf("safe lifecycle %s findings = %#v", id, got)
		}
	}

	nearMiss := analyze(t, `package app
import (
  "bufio"
  "database/sql"
  "strings"
)
func transfer(db *sql.DB) (*sql.Rows, error) {
  rows, err := db.Query("SELECT id FROM users")
  if err != nil { return nil, err }
  return rows, nil
}
func configuredOnly() { _ = bufio.NewScanner(strings.NewReader("x")) }
`)
	for _, id := range []string{"GO-COR-003", "GO-COR-004", "GO-RES-002"} {
		if got := findingsByID(nearMiss, id); len(got) != 0 {
			t.Errorf("ownership/iteration near-miss %s findings = %#v", id, got)
		}
	}
}

func TestScannerRuleCarriesIterationAndCompletePostLoopEvidence(t *testing.T) {
	g := analyze(t, `package app
import (
  "bufio"
  "bytes"
)
func fill(origin []byte) []byte {
  scanner := bufio.NewScanner(bytes.NewReader(origin))
  output := bytes.NewBuffer(nil)
  for scanner.Scan() {
    output.WriteByte('x')
  }
  output.WriteByte('\n')
  return output.Bytes()
}`)
	findings := findingsByID(g, "GO-COR-004")
	if len(findings) != 1 {
		t.Fatalf("GO-COR-004 findings = %#v", findings)
	}
	iteration, postLoop := evidenceStepByKind(findings[0].EvidenceSteps, "iteration"), evidenceStepByKind(findings[0].EvidenceSteps, "post_use")
	if iteration == nil || !strings.Contains(iteration.Expression, "scanner.Scan()") {
		t.Fatalf("iteration evidence = %#v", iteration)
	}
	if postLoop == nil || !strings.Contains(postLoop.Expression, "output.WriteByte") || !strings.Contains(postLoop.Expression, "return output.Bytes()") {
		t.Fatalf("post-loop evidence does not cover the function remainder: %#v", postLoop)
	}
	if strings.Contains(postLoop.Expression, "output.WriteByte('x')") {
		t.Fatalf("post-loop evidence includes loop body instead of starting after iteration: %q", postLoop.Expression)
	}
}

func TestScannerRuleRequiresErrCheckAfterIteration(t *testing.T) {
	for name, source := range map[string]string{
		"before loop": `package app
import ("bufio"; "strings")
func scan() {
  scanner := bufio.NewScanner(strings.NewReader("x"))
  _ = scanner.Err()
  for scanner.Scan() {}
}`,
		"inside loop": `package app
import ("bufio"; "strings")
func scan() {
  scanner := bufio.NewScanner(strings.NewReader("x"))
  for scanner.Scan() { _ = scanner.Err() }
}`,
	} {
		t.Run(name, func(t *testing.T) {
			if got := findingsByID(analyze(t, source), "GO-COR-004"); len(got) != 1 {
				t.Fatalf("GO-COR-004 findings = %#v", got)
			}
		})
	}
}

func TestScannerRuleRecognizesSameLinePostLoopErrCheck(t *testing.T) {
	g := analyze(t, `package app
import ("bufio"; "strings")
func scan() { scanner := bufio.NewScanner(strings.NewReader("x")); for scanner.Scan() {}; _ = scanner.Err() }
`)
	if got := findingsByID(g, "GO-COR-004"); len(got) != 0 {
		t.Fatalf("same-line post-loop Err check findings = %#v", got)
	}
}

func TestScannerRuleCarriesSameLinePostLoopSuffix(t *testing.T) {
	g := analyze(t, `package app
import ("bufio"; "strings")
func scan() int { scanner := bufio.NewScanner(strings.NewReader("x")); for scanner.Scan() {}; return 1 }
`)
	findings := findingsByID(g, "GO-COR-004")
	if len(findings) != 1 {
		t.Fatalf("GO-COR-004 findings = %#v", findings)
	}
	postUse := evidenceStepByKind(findings[0].EvidenceSteps, "post_use")
	if postUse == nil || !strings.Contains(postUse.Expression, "return 1") {
		t.Fatalf("same-line post-loop suffix = %#v", postUse)
	}
}

func evidenceStepByKind(steps []language.EvidenceStep, kind string) *language.EvidenceStep {
	for index := range steps {
		if steps[index].Kind == kind {
			return &steps[index]
		}
	}
	return nil
}

func TestFilesystemAndGORMDestructiveRulesUseLiteralAndOperationContext(t *testing.T) {
	unsafe := analyze(t, `package app
import (
  "os"
  "gorm.io/gorm"
)
func risky(db *gorm.DB) {
  _ = os.WriteFile("shared", []byte("x"), 0O0_666)
  _ = os.Chmod("existing", 0O0_666)
  db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&User{})
  db.Unscoped().Delete(&User{}, "id = ?", 7)
}`)
	for _, id := range []string{"GO-SEC-005", "GORM-SEC-002", "GORM-SEC-003"} {
		want := 1
		if id == "GO-SEC-005" {
			want = 2
		}
		if got := findingsByID(unsafe, id); len(got) != want {
			t.Errorf("%s findings = %#v", id, got)
		}
	}
	permissions := findingsByID(unsafe, "GO-SEC-005")
	permission := permissions[0]
	if permission.Observation == "" || permission.Hypothesis == "" {
		t.Fatalf("permission candidate lacks explicit semantic contract: %#v", permission)
	}
	if strings.Contains(strings.ToLower(permission.Observation), "world-writable") || !strings.Contains(permission.Observation, "other-write") {
		t.Fatalf("permission observation overstates effective permissions: %q", permission.Observation)
	}
	for _, want := range []string{"umask", "existing"} {
		if !strings.Contains(strings.ToLower(permission.Hypothesis), want) {
			t.Fatalf("permission hypothesis lacks %q context: %q", want, permission.Hypothesis)
		}
	}
	if strings.Contains(strings.ToLower(permissions[1].Hypothesis), "umask") {
		t.Fatalf("chmod hypothesis incorrectly depends on creation umask: %q", permissions[1].Hypothesis)
	}

	safe := analyze(t, `package app
import "os"
func safe() { _ = os.WriteFile("private", []byte("x"), 0600) }`)
	if got := findingsByID(safe, "GO-SEC-005"); len(got) != 0 {
		t.Fatalf("safe file mode findings = %#v", got)
	}
}

func TestKeyedSecurityRulesRequireCanonicalCompositeTypes(t *testing.T) {
	g := analyze(t, `package app
import (
  "crypto/tls"
  "gorm.io/gorm"
)
type LocalTLS struct{ InsecureSkipVerify bool }
type LocalSession struct{ AllowGlobalUpdate bool }
func safe() {
  _ = LocalTLS{InsecureSkipVerify: true}
  _ = LocalSession{AllowGlobalUpdate: true}
  _ = tls.Config{InsecureSkipVerify: false}
  _ = gorm.Session{AllowGlobalUpdate: false}
}
`)
	for _, id := range []string{"GO-SEC-004", "GORM-SEC-002"} {
		if got := findingsByID(g, id); len(got) != 0 {
			t.Errorf("unrelated keyed field %s findings = %#v", id, got)
		}
	}
}

func TestConditionalOverwriteDoesNotSuppressTaintedSQLBranch(t *testing.T) {
	g := analyze(t, `package app
import (
  "github.com/gin-gonic/gin"
  "gorm.io/gorm"
)
func handler(c *gin.Context, db *gorm.DB, maintenance bool) {
  query := c.Query("query")
  if maintenance { query = "SELECT 1" }
  db.Raw(query)
}
`)
	if got := findingsByID(g, "GIN-SEC-001"); len(got) != 1 {
		t.Fatalf("conditional overwrite findings = %#v", got)
	}
}

func TestCyclomaticComplexityCountsOnlyGoDecisionNodes(t *testing.T) {
	g := analyze(t, `package app
func complex(a, b bool, n int) {
  fake := "while catch ? if for switch case"
  if a && b { fake = "1" }
  if a || b { fake = "2" }
  if n > 0 { fake = "3" }
  if n > 1 { fake = "4" }
  for n > 0 { n-- }
  switch fake { case "1": println(fake); case "2": println(fake) }
}`)
	findings := findingsByID(g, "GO-MNT-002")
	if len(findings) != 1 || !strings.Contains(findings[0].Title, "11") {
		t.Fatalf("complexity findings = %#v", findings)
	}
}

func TestGORMResultErrorRuleRequiresAnUncheckedAssignedResult(t *testing.T) {
	unchecked := analyze(t, `package app
import "gorm.io/gorm"
func create(db *gorm.DB) {
  result := db.Create(&User{})
  _ = result.RowsAffected
}`)
	if got := findingsByID(unchecked, "GORM-COR-001"); len(got) != 1 {
		t.Fatalf("unchecked GORM result findings = %#v", got)
	}

	checked := analyze(t, `package app
import "gorm.io/gorm"
func create(db *gorm.DB) error {
  result := db.Create(&User{})
  return result.Error
}`)
	if got := findingsByID(checked, "GORM-COR-001"); len(got) != 0 {
		t.Fatalf("checked GORM result findings = %#v", got)
	}

	directError := analyze(t, `package app
import "gorm.io/gorm"
func create(db *gorm.DB) error {
  err := db.Create(&User{}).Error
  if err != nil { return err }
  return nil
}`)
	if got := findingsByID(directError, "GORM-COR-001"); len(got) != 0 {
		t.Fatalf("direct GORM Error findings = %#v", got)
	}

	terminal := analyze(t, `package app
import "gorm.io/gorm"
func find(db *gorm.DB, users any) {
  query := db.Where("active = ?", true)
  query.Find(users)
}
`)
	if got := findingsByID(terminal, "GORM-COR-001"); len(got) != 1 || !strings.Contains(got[0].CodeSnippet, "Find") {
		t.Fatalf("builder/terminal GORM findings = %#v", got)
	}
}

func TestGORMReceiverProvenanceIncludesGlobalsAndStructFields(t *testing.T) {
	g := analyze(t, `package app
import "gorm.io/gorm"
var globalDB *gorm.DB
type Repo struct{ db *gorm.DB }
func (r *Repo) lookup(query string) {
  r.db.Raw(query)
  globalDB.Exec(query)
}
`)
	if got := findingsByID(g, "GORM-SEC-001"); len(got) != 2 {
		t.Fatalf("field/global GORM findings = %#v", got)
	}
}

func analyze(t *testing.T, source string) *graph.Graph {
	t.Helper()
	adapter := &golangadapter.Adapter{}
	ast, err := adapter.Parse(context.Background(), []byte(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	t.Cleanup(func() { _ = ast.Close() })
	ast.FilePath = "fixture.go"
	document, err := adapter.ExtractSemantic(ast)
	if err != nil {
		t.Fatalf("extract semantic: %v", err)
	}
	builder := graph.NewBuilder()
	builder.AddFile(ast.FilePath)
	builder.AddFunctions(adapter.ExtractFunctions(ast))
	builder.AddDocument(document)
	return builder.Build()
}

func findingsByID(g *graph.Graph, ids ...string) []language.PotentialFinding {
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	var findings []language.PotentialFinding
	for _, rule := range rules.All() {
		if !wanted[rule.ID()] {
			continue
		}
		findings = append(findings, rule.Check(g)...)
	}
	return findings
}

func findingsByPrefix(g *graph.Graph, prefix string) []language.PotentialFinding {
	var findings []language.PotentialFinding
	for _, rule := range rules.All() {
		if strings.HasPrefix(rule.ID(), prefix) {
			findings = append(findings, rule.Check(g)...)
		}
	}
	return findings
}
