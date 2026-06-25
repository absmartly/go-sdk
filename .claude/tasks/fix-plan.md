# Go SDK Fix Plan

Based on full review of PR #11. This PR was rated CLEAN.

## Minor (informational only)

### 1. Map comparison logic dependency
- **File**: `sdk/jsonexpr/eval/Evaluator.go:81-89`
- **Status**: Correct. Length check at line 68-72 ensures map equality logic works. No fix needed.

### 2. Slice DeepEqual vs recursive Compare
- **File**: `sdk/jsonexpr/eval/Evaluator.go:75-79`
- **Status**: Matches cross-SDK behavior (strict equality for collection elements). No fix needed.

### 3. BooleanConvert redundant checks
- **File**: `sdk/jsonexpr/eval/Evaluator.go:110-137`
- **Status**: Redundant but harmless. Could clean up for clarity but not required.

No action items from initial review.

## Additional Findings (Full Review v2)

### Correctness

#### 4. Race condition: `ReadyFuture_` accessed without synchronization [HIGH]
- **File**: `sdk/Context.go:182, 191, 232-237`
- **Issue**: `ReadyFuture_` is set to `nil` from a goroutine callback (line 182, 191) while `WaitUntilReady()` reads it (line 232) without any lock. The race detector confirms this: `go test -race` fails on multiple tests (TestWaitUntilReady, TestEventLoggerCalledOnPublish, TestEventLoggerCalledOnRefresh, TestEventLoggerCalledOnClose, etc.).
- **Root cause**: The `ReadyFuture_` field is a bare pointer shared across goroutines with no synchronization. The `SetData` callback runs in a future listener goroutine and sets `cntx.ReadyFuture_ = nil`, while `WaitUntilReady` reads `c.ReadyFuture_` from the caller goroutine.
- **Fix**: Protect `ReadyFuture_` access with a mutex (could reuse `ContextLock_` or `DataLock`), or use `atomic.Pointer[future.Future]`. The `RefreshFuture_` field at line 609/628 has the same pattern but is partially protected by the `Refreshing_` atomic CAS.
- **Severity**: High — this can cause crashes in production under concurrent access.

#### 5. Race condition: `RefreshFuture_` read outside CAS guard [MEDIUM]
- **File**: `sdk/Context.go:628`
- **Issue**: After the `Refreshing_.CompareAndSwap` block (lines 607-626), `RefreshFuture_` is read at line 628 without any lock. If another goroutine concurrently enters `RefreshAsync()` and the CAS succeeds, the write at line 609 races with the read at line 628.
- **Fix**: Move the `RefreshFuture_` read inside a lock or restructure to return the future from within the CAS block.

#### 6. `CreateDefaultContextConfig()` uses deprecated fields exclusively [LOW]
- **File**: `sdk/ContextConfig.go:101-110`
- **Issue**: The factory function sets `PublishDelay_`, `RefreshInterval_`, `Units_`, `Attributes_`, `Cassigmnents_`, `Overrides_` (all deprecated fields). This means new code calling `CreateDefaultContextConfig()` then setting `PublishDelay` (new field) gets unexpected behavior — the accessor `publishDelay()` returns the new field (0) if non-zero, otherwise falls back to deprecated field (1000). This works accidentally but is confusing.
- **Fix**: Set the new field names (`PublishDelay`, `RefreshInterval`, etc.) in `CreateDefaultContextConfig()`.

#### 7. `NewWithOptions` always returns nil error [LOW]
- **File**: `sdk/ABSmartly.go:62-76`
- **Issue**: `NewWithOptions` returns `(*ABSmartly, error)` but never returns an error. No validation of `opts.Endpoint`, `opts.APIKey`, etc. Empty strings silently produce a broken client.
- **Fix**: Validate required fields and return errors for empty endpoint/apiKey/application/environment.

### Security

#### 8. Regex injection via MatchOperator [LOW]
- **File**: `sdk/jsonexpr/operators/MatchOperator.go:21`
- **Issue**: `regexp.Compile(pattern)` uses user-supplied audience rule patterns. Malicious patterns could cause catastrophic backtracking (ReDoS). Go's `regexp` package uses RE2 which is linear-time, so this is **not exploitable** in Go specifically. Informational only — other SDK languages may be vulnerable.
- **Status**: No fix needed (Go's RE2 engine is safe).

#### 9. TLS config is empty [INFO]
- **File**: `sdk/DefaultHTTPClient.go:50`
- **Issue**: `SetTLSClientConfig(&tls.Config{})` overrides defaults with an empty config. This is functionally identical to the default (TLS 1.2+, system CA pool), but it's unnecessary and could mask future Go default improvements.
- **Status**: No fix needed, informational only.

### Performance

#### 10. `reflect.ValueOf(result).Int()` in comparison operators [LOW]
- **Files**: `sdk/jsonexpr/operators/GreaterThanOperator.go:18`, `LessThanOperator.go:18`, `LessThanOrEqual.go:18`, `GreaterThatOrEqualOperator.go:18`, `InOperator.go:32,40`
- **Issue**: `Compare()` returns `interface{}` containing an `int`. Each operator wraps it in `reflect.ValueOf(result).Int()` which creates a reflection object just to extract an int. A simple type assertion `result.(int)` would be more efficient and idiomatic.
- **Fix**: Replace `reflect.ValueOf(result).Int()` with `result.(int)` in all 6 locations.

#### 11. `Evaluator.Compare` panic on nil interface values [MEDIUM]
- **File**: `sdk/jsonexpr/eval/Evaluator.go:31-35`
- **Issue**: Line 31 calls `lhs.IsZero()` and line 33 calls `lhs.Interface()` — both can panic if `lhs` is a `reflect.Value` wrapping a nil interface of certain kinds. The `IsValid()` check on line 30 only confirms the Value was created, not that the underlying value is non-nil. For example, `reflect.ValueOf((*int)(nil))` is valid but `IsZero()` is true, and the code correctly handles this. However, calling `.Kind()` on line 38 after the zero/nil block could process a nil-pointer Value as `reflect.Ptr` which falls through all branches to the bottom returning `nil` — this is correct but could be more explicit.
- **Status**: Functionally correct but fragile. No fix required.

### Code Quality

#### 12. Typo in filename: `GreaterThatOrEqualOperator.go` [LOW]
- **File**: `sdk/jsonexpr/operators/GreaterThatOrEqualOperator.go`
- **Issue**: "That" should be "Than" — `GreaterThanOrEqualOperator.go`.
- **Fix**: Rename file.

#### 13. `Cassigmnents_` typo in deprecated field [INFO]
- **File**: `sdk/ContextConfig.go:19`
- **Issue**: The deprecated field `Cassigmnents_` has a typo ("sigm" instead of "sign"). Since it's deprecated and the new field `CustomAssignments` is correctly named, this is just informational.
- **Status**: No fix needed (deprecated).

### Summary of Actionable Items

| # | Severity | Category | Description |
|---|----------|----------|-------------|
| 4 | HIGH | Correctness | Race condition on `ReadyFuture_` — multiple tests fail with `-race` |
| 5 | MEDIUM | Correctness | Race condition on `RefreshFuture_` read outside CAS guard |
| 10 | LOW | Performance | Use type assertion instead of `reflect.ValueOf().Int()` |
| 6 | LOW | Correctness | `CreateDefaultContextConfig` uses deprecated fields |
| 7 | LOW | Correctness | `NewWithOptions` never validates inputs |
| 12 | LOW | Quality | Filename typo `GreaterThatOrEqualOperator.go` |

## Additional Findings (Full Review v3)

### Correctness

#### 14. `WaitUntilReadyAsync` calls `SetResult` on already-resolved future [MEDIUM]
- **File**: `sdk/Context.go:223-224`
- **Issue**: `WaitUntilReadyAsync()` adds a listener that calls `c.ReadyFuture_.SetResult(val, err)` — i.e., it calls `SetResult` on the same future that is already being resolved. This is a no-op at best and potentially double-resolves the future. The intent seems to be to return a future that resolves when the context is ready, but the implementation is confused: it listens on `ReadyFuture_` and then sets the result back on `ReadyFuture_` itself.
- **Fix**: This listener is pointless — `ReadyFuture_` already has its result set by `CreateContext`. Just return `c.ReadyFuture_` directly without the Listen call.

#### 15. `WaitUntilReadyAsync` accesses `ReadyFuture_` without synchronization [HIGH]
- **File**: `sdk/Context.go:223, 226`
- **Issue**: Same race as finding #4. `WaitUntilReadyAsync()` reads `c.ReadyFuture_` without any lock, while `CreateContext`'s goroutine sets `cntx.ReadyFuture_ = nil` (line 182, 191). If `WaitUntilReadyAsync` is called concurrently with the data callback, it can read a nil `ReadyFuture_` and panic on line 223 (`nil pointer dereference`).
- **Fix**: Same mutex protection as finding #4.

#### 16. `BinaryOperator.Evaluate` returns `nil` when one of lhs/rhs is nil but not both [MEDIUM]
- **File**: `sdk/jsonexpr/operators/BinaryOperator.go:27-32`
- **Issue**: The logic is: if both non-nil call Binary; if both nil call Binary; otherwise return nil. This means `Binary(evaluator, nil, 42)` is never called — the BinaryOperator short-circuits to nil. This is intentional for comparison operators (which all check `lhs == nil || rhs == nil`), but the `EqualsOperator.Binary` explicitly handles `nil == nil` (returns true) and `nil vs non-nil` (returns nil). The `nil == nil` case in EqualsOperator (line 13-14) is therefore **dead code** — it's already handled by `BinaryOperator.Evaluate` line 30-31 which calls `Binary(evaluator, nil, nil)`. This is not a bug, but the logic is confusing and potentially fragile if a new operator is added that needs to handle one-nil-one-non-nil.
- **Status**: Correct behavior (matches cross-SDK). Informational only.

### Security

#### 17. Client does not validate response before nil dereference [LOW]
- **File**: `sdk/Client.go:64-65`
- **Issue**: `GetContextData` checks `if err != nil || value.(*resty.Response).StatusCode()/100 != 2` — if `err != nil`, the code still accesses `value.(*resty.Response).Status()` on line 65. If `value` is `nil` when `err != nil`, this will panic with nil pointer dereference. Resty's `Get()` can return `(nil, error)` on DNS failure, timeout, etc.
- **Fix**: Check `err != nil` separately: `if err != nil { return nil, err }` before accessing `value`.

#### 18. No TLS certificate pinning or custom CA configuration exposed [INFO]
- **File**: `sdk/DefaultHTTPClient.go`
- **Issue**: The SDK does not expose any way to configure TLS certificate validation (custom CAs, pinning). This is fine for most use cases but worth noting for enterprise deployments. Go's default TLS config is secure.
- **Status**: Informational only. No fix needed.

### Performance

#### 19. `Evaluator.Compare` performs unnecessary `NumberConvert` on lhs when rhs conversion fails [LOW]
- **File**: `sdk/jsonexpr/eval/Evaluator.go:39-40`
- **Issue**: Both `NumberConvert(rhs)` and `NumberConvert(lhs)` are called regardless of whether the first conversion succeeds. If `NumberConvert(rhs)` fails, the lhs conversion is wasted work. Should check rhs error first.
- **Fix**: `rvalue, rerr := e.NumberConvert(rhs); if rerr != nil { ... } lvalue, lerr := e.NumberConvert(lhs)`

#### 20. `Flush()` copies `Context` struct by value [MEDIUM]
- **File**: `sdk/Context.go:979`
- **Issue**: `FlushMapper{Context: *c}` copies the entire Context struct (which includes mutexes, atomic values, slices, maps) by value. Copying a mutex is explicitly prohibited in Go (vet would catch `sync.Mutex` copy, but `sync.RWMutex` pointer fields dodge this). The copy creates a snapshot of pointer fields, so the copied context shares the same underlying data — this is currently safe but fragile. If `FlushMapper` ever writes to the context, it would cause races.
- **Status**: Not a current bug but a correctness hazard. Consider passing `*Context` to `FlushMapper`.

### Code Quality

#### 21. `var` declarations used instead of `:=` throughout [LOW]
- **Files**: All changed Go files
- **Issue**: The codebase consistently uses `var x = expr` instead of the idiomatic `x := expr`. While functionally equivalent, the idiomatic Go style is short variable declaration (`:=`) inside function bodies. This is a pre-existing style issue, not introduced by this PR.
- **Status**: No fix needed (consistent existing style).

#### 22. `error` used as variable name shadows builtin [LOW]
- **File**: `sdk/jsonexpr/operators/InOperator.go:46,49,52,55`
- **Issue**: `var rhsString, error = evaluator.StringConvert(rhsVal)` uses `error` as a variable name, which shadows the built-in `error` type. Should use `err` per Go convention.
- **Fix**: Rename `error` to `err` in all four locations.

#### 23. Unused `reflect` import could be removed from `EqualsOperator.go` if `result == 0` is used [INFO]
- **File**: `sdk/jsonexpr/operators/EqualsOperator.go:4`
- **Issue**: The `reflect` import is used only indirectly via `reflect.ValueOf` for the `Compare` call. Since `Compare` already returns `interface{}` containing an `int`, the `result == 0` comparison (line 21) already works without reflect — but if finding #10 is applied (switching to type assertion), the `reflect` import in `EqualsOperator.go` could be removed.
- **Status**: Depends on finding #10.

### Summary of New Actionable Items (v3)

| # | Severity | Category | Description |
|---|----------|----------|-------------|
| 15 | HIGH | Correctness | `WaitUntilReadyAsync` accesses `ReadyFuture_` without sync (same root cause as #4) |
| 17 | LOW | Security | `Client.GetContextData` nil dereference when `err != nil` and `value == nil` |
| 14 | MEDIUM | Correctness | `WaitUntilReadyAsync` pointlessly calls `SetResult` on same future |
| 20 | MEDIUM | Performance | `Flush()` copies entire Context struct by value |
| 19 | LOW | Performance | Unnecessary `NumberConvert` on lhs when rhs fails |
| 22 | LOW | Quality | `error` shadows builtin type in `InOperator.go` |
