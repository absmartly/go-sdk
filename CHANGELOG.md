# Changelog

## v1.2.0 (unreleased)

Go versions are released as git tags only; this entry tracks changes since v1.1.1.

### BREAKING
- Users whose unit UID contains non-ASCII characters will be reassigned in running experiments: UIDs are now hashed from their UTF-8 bytes, as in the other SDKs. ASCII UIDs keep their assignments.
- Audiences are re-evaluated after `SetAttribute`/`SetAttributes`: a cached assignment whose audience result changes with the new attributes is recomputed, so a user can move in or out of an experiment's audience within one context.
- When several experiments define the same variable key, the value is read from the first experiment, by id, that the unit is assigned to or overridden in; `GetVariableValue` exposes each experiment visited, while `PeekVariableValue` exposes none (v1.1.1 used the last experiment in the payload). This matches the JS SDK's `_resolveVariableValue` rule and affects `GetVariableValue`, `PeekVariableValue`, `GetVariableKeys`, `GetVariableKeysAll` and `IndexVariables_`, all of which now follow experiment-id order.
- Go 1.20 or later is required (v1.1.1 built on Go 1.17). `resty` is pinned to v2.16.5.
- JSON custom fields parse any JSON value.

### Added
- `FinalizeAsync`, which also waits for logger delivery; `Finalize` event type alias (value `"Close"`).
- `GetCustomFieldValueE` (error-returning custom field access).
- `GetVariableKeysAll` returns every experiment per variable key; `GetVariableKeys` keeps returning the first one.
- `ErrContextClosed`, `ErrContextClosing`, `ErrContextNotReady`, with the v1.1.1 error texts. Read methods return `ErrContextNotReady` before the context is ready, with the same text as v1.1.1.
- `ErrUnitAlreadySet`, `ErrUnitBlank` and `UnitError`: unit errors keep the v1.1.1 texts (`unit already set`, `unit  UID must not be blank.`), match with `errors.Is`, and `UnitError.Canonical()` gives the cross-SDK text (`Unit '<type>' UID already set.`).
- `SetUnitValue` (string or number UIDs, numbers hashed as the rounded integer like JS), `GetUnit`, `GetUnits`, `GetAttribute`, `GetAttributes`.
- `HashUnitValue`, `UTF8Bytes`, `Base64UrlNoPadding` encoding helpers.
- `ContextPublisher_` field and `SetContextPublisher` (preferred over `ContextEventHandler_`).

### Fixed
- `SetOverride`/`SetCustomAssignment` are allowed after finalize, like the other SDKs.
- Exposure for an unknown experiment serializes `"unit": null` instead of `""`.
- The ready, error and refresh events are logged before `WaitUntilReady`/`Refresh` return.
- JSON expr: `eq` null handling, nested deep equality; `eq(null, null)` returns null.
- Close/flush/refresh: events kept pending on failed publish, concurrent `CloseAsync` callers share one future, no lost publishes, logger panics contained.
- Data races on `ReadyFuture_`, `RefreshFuture_`, `ClosingFuture_`, `RefreshTimer_`, `Timeout_` and `Attributes_`, and the `AttrsSeq` write during audience matching. Known remaining race, inherited from v1.1.1: `Index_` is written under `DataLock` by a refresh and read under `ContextLock_` by `GetTreatment`/`PeekTreatment`.
- A failed close no longer restarts the refresh timer. On success, `Close()` and an empty-queue `CloseAsync()` settle after the `Close` event is logged, as in v1.1.1.
- Builder `Timeout` applies to requests; the retry condition no longer panics on a nil response (502 and 503 are retried, as before).
