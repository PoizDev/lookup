# Lookup Rule Catalog

Analyzer v2 rule IDs are namespaced by language or ecosystem. Severity below is the base/static severity; deterministic evidence can refine confidence and, where documented, severity. Examples are intentionally abbreviated.

| ID | Title | Category | Base severity | Scope | Description | Example |
|:--|:--|:--|:--|:--|:--|:--|
| `ARC-001` | Layer violation | Architecture | Medium | Generic graph | Presentation code directly imports a persistence layer. | `handlers → repository` |
| `GO-SEC-001` | Hardcoded secret | Security | Critical | Go literal | Sensitive declaration contains a credential-like literal; test fixtures are downgraded. | `token := "sk-…"` |
| `GO-COR-001` | Ignored returned error | Correctness | Medium | Go core | A known error-bearing call discards its error result. | `resp, _ := http.Get(url)` |
| `GO-COR-002` | Unchecked type assertion | Correctness | High | Go core | Single-value type assertion can panic. | `name := v.(string)` |
| `GO-COR-003` | rows.Err ignored | Correctness | Medium | database/sql | Rows iteration lacks a final error check. | `for rows.Next() {}` |
| `GO-COR-004` | scanner.Err ignored | Correctness | Medium | bufio scanner | Scanner iteration lacks a final error check. | `for scanner.Scan() {}` |
| `GO-COR-005` | Transaction incomplete | Correctness | High | database/sql | A transaction has no visible Commit/Rollback in the function. | `tx, _ := db.Begin()` |
| `GO-RES-001` | HTTP body not closed | Correctness | Medium | net/http | Acquired response body has no matching close. | `resp, _ := http.Get(url)` |
| `GO-RES-002` | Rows not closed | Correctness | Medium | database/sql | Query rows have no matching close. | `rows, _ := db.Query(q)` |
| `GO-RES-003` | File not closed | Correctness | Medium | os | Opened file has no matching close. | `f, _ := os.Open(path)` |
| `GO-RES-004` | Ticker not stopped | Correctness | Medium | Go stdlib | New ticker has no matching Stop. | `t := time.NewTicker(d)` |
| `GO-RES-005` | Context cancel not called | Correctness | Medium | Go context | Derived context cancellation function is not called. | `_, cancel := context.WithCancel(ctx)` |
| `GO-CON-001` | Goroutine in loop | Correctness | Medium | Go concurrency | Loop starts goroutines without a statically visible bound. | `for ... { go work() }` |
| `GO-CON-002` | Defer in loop | Correctness | Low | Go defer | Deferred work accumulates until the surrounding function returns. | `for ... { defer f.Close() }` |
| `GO-CON-003` | WaitGroup Add in goroutine | Correctness | High | Go concurrency | `Add` can race with `Wait` when called after goroutine start. | `go func(){ wg.Add(1) }()` |
| `GO-CON-004` | Lock not released | Correctness | Medium | Go sync | Mutex lock has no matching Unlock in the function. | `mu.Lock()` |
| `GO-SEC-002` | Weak MD5 | Security | Medium | crypto/md5 | Security-sensitive code uses MD5 primitives. | `md5.New()` |
| `GO-SEC-003` | Weak SHA-1 | Security | Medium | crypto/sha1 | Security-sensitive code uses SHA-1 primitives. | `sha1.Sum(data)` |
| `GO-SEC-004` | TLS verification disabled | Security | High | crypto/tls | TLS config explicitly disables certificate verification. | `InsecureSkipVerify: true` |
| `GO-SEC-005` | World-writable mode | Security | Medium | os filesystem | Literal mode grants write access to everyone. | `os.WriteFile(p, b, 0666)` |
| `GO-MNT-001` | Long function | Maintainability | Medium | Go core | Function exceeds 100 lines; over 200 raises severity. | `func process(...) // 140 lines` |
| `GO-MNT-002` | Cyclomatic complexity | Maintainability | Medium | Go AST | Go control/logical AST nodes produce complexity over 10. | `if`, `for`, `case`, `&&`, `||` |
| `GO-PER-001` | Expensive loop operation | Performance | Low | Go AST | Known allocation/encoding/timer call occurs inside a loop. | `for ... { regexp.Compile(p) }` |
| `GO-SQL-SEC-001` | Tainted database/sql | Security | High | net/http + database/sql | HTTP input reaches non-parameterized SQL execution. | `db.Query("…" + id)` |
| `GO-CMD-SEC-001` | Tainted command | Security | High | net/http + os/exec | HTTP input reaches command construction/execution. | `exec.Command("sh", "-c", q)` |
| `GO-CMD-SEC-002` | Environment-controlled shell command | Security | Medium | os + os/exec | Environment input reaches a shell command string or executable selection. | `exec.Command("sh", "-c", os.Getenv("CMD"))` |
| `GO-HTTP-SEC-001` | Unsafe redirect | Security | Medium | net/http | User-controlled input reaches an HTTP redirect target. | `http.Redirect(w, r, next, 302)` |
| `GO-FS-SEC-001` | Tainted filesystem path | Security | High | HTTP + os filesystem | HTTP query/path/header/body input reaches an open/read/write path. | `os.ReadFile(c.Query("path"))` |
| `GIN-SEC-001` | Gin tainted SQL | Security | High | Gin + SQL sink | Gin request input reaches non-parameterized SQL. | `db.Raw(c.Query("q"))` |
| `GIN-SEC-002` | Gin tainted command | Security | High | Gin + os/exec | Gin request input reaches command execution. | `exec.Command("sh", "-c", c.Query("q"))` |
| `GIN-SEC-003` | Gin unsafe redirect | Security | Medium | Gin | Gin request input controls a redirect. | `c.Redirect(302, c.Query("next"))` |
| `FIBER-SEC-001` | Fiber tainted SQL | Security | High | Fiber v2/v3 + SQL sink | Fiber request input reaches non-parameterized SQL. | `db.Raw(c.Query("q"))` |
| `FIBER-SEC-002` | Fiber tainted command | Security | High | Fiber v2/v3 + os/exec | Fiber request input reaches command execution. | `exec.Command("sh", "-c", c.Params("q"))` |
| `FIBER-SEC-003` | Fiber unsafe redirect | Security | Medium | Fiber v2/v3 | Fiber request input controls a redirect. | `c.Redirect(c.Query("next"))` |
| `GORM-SEC-001` | GORM dynamic/tainted SQL | Security | High | GORM | Distinguishes parameterized SQL from dynamic Raw/Exec; taint strengthens confidence. | `db.Raw(fmt.Sprintf(..., id))` |
| `GORM-SEC-002` | Global updates enabled | Security | High | GORM | Session enables updates/deletes without a WHERE condition. | `AllowGlobalUpdate: true` |
| `GORM-SEC-003` | Unscoped destructive operation | Security | High | GORM | Unscoped delete bypasses soft-delete protection. | `db.Unscoped().Delete(&u)` |
| `GORM-COR-001` | Result error unchecked | Correctness | Medium | GORM | Assigned GORM result is used without reading `.Error`. | `result := db.Create(&u)` |
| `GORM-COR-002` | Transaction incomplete | Correctness | High | GORM | GORM transaction has no visible Commit/Rollback. | `tx := db.Begin()` |
| `PY-SEC-001` | Dynamic eval | Security | High | Python AST | Dynamic input reaches unshadowed builtin `eval`; literal-only and shadowed calls are excluded. | `eval(user_input)` |
| `PY-SEC-002` | Dynamic exec | Security | High | Python AST | Dynamic input reaches unshadowed builtin `exec`; literal-only and shadowed calls are excluded. | `exec(source)` |
| `PY-COR-001` | Mutable default argument | Correctness | Medium | Python AST | Function parameter uses a list, dict, or set default shared across calls. | `def f(items=[]):` |
| `RUST-COR-001` | Unsafe block review | Correctness | Low | Rust AST | An explicit unsafe block requires manual invariant review; presence alone is not treated as a vulnerability. | `unsafe { *ptr }` |
| `CSHARP-COR-001` | Async void method | Correctness | Medium | C# AST | Async method returns void rather than an awaitable task. | `async void Run()` |
| `CSHARP-SEC-001` | Dynamic EF raw SQL | Security | High | C# + EF provenance | Dynamic SQL reaches an EF raw API with import, `DbContext`, receiver, and argument evidence. | `db.Database.ExecuteSqlRaw(sql)` |
| `PY-CMD-SEC-001` | Request-to-shell command | Security | High | Python web + shell | External request input reaches `os.system` or a `shell=True` subprocess. | `os.system(command)` |
| `PY-SQL-SEC-001` | Dynamic DB-API SQL | Security | High | Python web + DB-API | Request input reaches a provenance-qualified cursor execute call. | `cursor.execute(query)` |
| `PY-FS-SEC-001` | Request-controlled path | Security | High | Python filesystem | Request input controls a builtin filesystem path. | `open(path)` |
| `PY-DESER-SEC-001` | Unsafe pickle input | Security | High | Python pickle | Request bytes reach pickle deserialization. | `pickle.loads(body)` |
| `PY-TLS-SEC-001` | TLS verification disabled | Security | Medium | Python requests | Requests explicitly uses `verify=False`. | `requests.get(url, verify=False)` |
| `PY-ML-SEC-001` | Unsafe PyTorch artifact load | Security | High | PyTorch | External artifact reaches `torch.load` with `weights_only=False`. | `torch.load(path, weights_only=False)` |
| `PY-CV-COR-001` | Unchecked OpenCV image | Correctness | Medium | OpenCV | `imread` output reaches an image consumer without a visible `None` guard. | `cv2.resize(image, size)` |
| `PY-CV-RES-001` | Capture not released | Correctness | Medium | OpenCV | Video capture neither releases nor transfers ownership. | `cv2.VideoCapture(0)` |
| `CSHARP-ASYNC-001` | Blocking Task in async method | Correctness | Medium | .NET Task | A known Task is synchronously waited inside async code. | `Task.Delay(1).Wait()` |
| `CSHARP-CMD-SEC-001` | ASP.NET input to shell | Security | High | ASP.NET + Process | Request input reaches cmd/PowerShell shell interpretation. | `Process.Start("cmd.exe", "/c " + cmd)` |
| `CSHARP-FS-SEC-001` | ASP.NET input to filesystem | Security | High | ASP.NET + System.IO | Request input controls a System.IO path. | `File.ReadAllText(path)` |
| `CSHARP-SSRF-SEC-001` | ASP.NET input to outbound URL | Security | High | ASP.NET + HttpClient | Request input controls an HttpClient URL. | `client.GetAsync(url)` |
| `CSHARP-TLS-SEC-001` | Certificate validation bypass | Security | High | .NET HTTP | Validation callback unconditionally returns true. | `callback = (...) => true` |
| `RUST-ASYNC-001` | Blocking sleep in async context | Correctness | Medium | Tokio | `std::thread::sleep` blocks async code outside `spawn_blocking`. | `std::thread::sleep(d)` |
| `RUST-SQL-SEC-001` | Dynamic Rust SQL | Security | High | SQLx/Diesel | Service input reaches dynamic SQL syntax. | `sqlx::query(&sql)` |
| `RUST-CMD-SEC-001` | Service input to shell | Security | High | std::process | External input reaches `sh`/`bash -c`. | `Command::new("sh").arg("-c")` |
| `RUST-FS-SEC-001` | Service input to filesystem | Security | High | std::fs | External input controls a filesystem path. | `fs::read_to_string(path)` |
| `RUST-SSRF-SEC-001` | Service input to reqwest | Security | High | reqwest | External input controls an outbound URL. | `reqwest::get(url)` |
| `RUST-TLS-SEC-001` | Invalid certificates accepted | Security | High | reqwest | Client explicitly accepts invalid certificates. | `danger_accept_invalid_certs(true)` |

## Safe patterns

The SQL rules distinguish static literals, parameterized calls, and dynamic query expressions. They do not emit for placeholder APIs such as `db.Query("… WHERE id = ?", id)`, `db.Raw("… WHERE id = ?", id)`, `db.Where("id = ?", id)`, or GORM map/struct builders. Bound parameter values are excluded from SQL-expression taint. Ordinary `os/exec` argv are not treated as shell injection; only tainted executable selection or shell command strings are sinks. Lifecycle rules suppress ownership-transfer returns/calls and use moderate confidence where branch-sensitive ownership is unknown. Two-value type assertions, secure TLS configuration, non-world-writable modes, and checked GORM `.Error` results are explicit negative fixtures.

## Rule migration

| Legacy ID | Analyzer v2 status |
|:--|:--|
| `SEC-001` | Retained as `GO-SEC-001`; terminal redaction accepts the legacy alias. |
| `SEC-002` | Superseded by `GO-SQL-SEC-001`, `GIN-SEC-001`, `FIBER-SEC-001`, and `GORM-SEC-001`. |
| `COR-001` | Rewritten as AST fact rule `GO-COR-001`. |
| `COR-002` | Removed from runtime: regex-based nil prediction could not prove a dereference failure with acceptable signal. `GO-COR-002` is a separate AST-proven type-assertion rule, not a compatibility alias. |
| `MNT-001` | Retained as `GO-MNT-001`. |
| `MNT-002` | Rewritten as Go AST rule `GO-MNT-002`; non-Go tokens were removed. |
| `PER-001` | Rewritten as AST-context rule `GO-PER-001`; generic N+1 claims were removed. |
| `PER-002` | Split into `GO-CON-*` and `GO-RES-*` lifecycle rules. |
| `ARC-001` | Retained unchanged because its scope is language-independent graph structure. |
