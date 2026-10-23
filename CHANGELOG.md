# Changelog

## v1.2.0 (unreleased)

Go versions are released as git tags only; this entry tracks changes since v1.1.1.

### BREAKING
- Error text changed to the canonical cross-SDK messages.
- New `FinalizeAsync` and finalize lifecycle; the finalize event name changed to match other SDKs.
- `GetCustomFieldValueE` added (error-returning custom field access); JSON custom fields parse any JSON value.
- `GetVariableKeys` returns the list of experiments per key.

### Added
- `ContextPublisher_` field and `SetContextPublisher` (preferred over `ContextEventHandler_`).

### Fixed
- JSON expr: `eq` null handling, nested deep equality; `eq(null, null)` returns null.
- Close/flush/refresh: events kept pending on failed publish, concurrent `CloseAsync` callers share one future, no lost publishes, data races removed, logger panics contained.
- Builder `Timeout` applies to requests; astral/multibyte unit hashing; supports Go 1.23+.
