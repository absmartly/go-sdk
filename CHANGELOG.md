# Changelog

## v1.2.0 (unreleased)

Go versions are released as git tags only; this entry tracks changes since v1.1.1.

### BREAKING
- JSON custom fields parse any JSON value.
- Read methods return `ErrContextNotReady` before the context is ready.

### Added
- `FinalizeAsync`, which also waits for logger delivery; `Finalize` event type alias (value `"Close"`).
- `GetCustomFieldValueE` (error-returning custom field access).
- `GetVariableKeysAll` returns every experiment per variable key; `GetVariableKeys` keeps returning the first one.
- `ErrContextClosed`, `ErrContextClosing`, `ErrContextNotReady`, with the v1.1.1 error texts.
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
- A failed close no longer restarts the refresh timer; `Close()` returns after the `Close` event is logged.
- Builder `Timeout` applies to requests; astral/multibyte unit hashing. Go 1.20+ (resty pinned to v2.16.5).
