# Changelog

## v1.2.0 (unreleased)

Go versions are released as git tags only; this entry tracks changes since v1.1.1.

### BREAKING
- Error text changed to the canonical cross-SDK messages.
- New `FinalizeAsync` and finalize lifecycle; the finalize event name changed to match other SDKs.
- `GetCustomFieldValueE` added (error-returning custom field access); JSON custom fields parse any JSON value.
- `GetVariableKeys` returns the list of experiments per key.

### Added
- `SetUnitValue` (string or number UIDs, numbers hashed as the rounded integer like JS), `GetUnit`, `GetUnits`, `GetAttribute`, `GetAttributes`.
- `HashUnitValue`, `UTF8Bytes`, `Base64UrlNoPadding` encoding helpers.
- `ContextPublisher_` field and `SetContextPublisher` (preferred over `ContextEventHandler_`).

### Fixed
- `SetOverride`/`SetCustomAssignment` are allowed after finalize, like the other SDKs.
- Exposure for an unknown experiment serializes `"unit": null` instead of `""`.
- The ready, error and refresh events are logged before `WaitUntilReady`/`Refresh` return.
- Blank unit error text is `Unit '<type>' UID must not be blank.`
- JSON expr: `eq` null handling, nested deep equality; `eq(null, null)` returns null.
- Close/flush/refresh: events kept pending on failed publish, concurrent `CloseAsync` callers share one future, no lost publishes, data races removed, logger panics contained.
- Builder `Timeout` applies to requests; astral/multibyte unit hashing; supports Go 1.23+.
