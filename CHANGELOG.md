# Changelog

## v1.2.0 (2026-11-17)

Go versions are released as git tags only; this entry lists every change since v1.1.1. It is a non-breaking release: v1.1.1 code compiles unchanged, apart from unkeyed (positional) literals of `jsonmodels.Experiment` and `jsonmodels.Exposure` (see Changed). Requires Go 1.17 or later, as v1.1.1 did.

### Added
- Assignment rules (`jsonmodels.Experiment.AssignmentRules`): ordered `assign` rules (a fixed variant) and `split` rules (percentages such as `20/30/50`, hashed with the experiment's own seed), limited by environment and by JSON-expression conditions. The first matching rule wins over the audience/`audienceStrict`, traffic, full-on and custom assignments; only `SetOverride` beats it. A `split` that matches before its unit is set gives the experiment's no-unit result and is re-resolved once the unit is set, unless already exposed. Malformed rules are skipped in favour of the next rule, except that an applicable `assign` rule with an out-of-range variant, or a `split` with a different number of values than the experiment has variants, stops rule evaluation and falls back to normal assignment (as in the JS SDK and the collector). A non-string or unparseable `assignmentRules` only drops that experiment's rules. Matches the JS (#66) and Java (#17) SDKs.
- `semver_eq`, `semver_gt`, `semver_gte`, `semver_lt` and `semver_lte` JSON expression operators. Numeric operands are converted as the JS SDK does (`toFixed(15)`, trailing zeros dropped), so `1e-7` equals `"0.0000001"`.
- `NewBuilder()` (`ABsmartlyBuilder`), with `ApplicationVersion`, `Timeout`, `Retries` and the other options; `ABsmartly`/`ABsmartlyConfig` aliases of `ABSmartly`/`ABSmartlyConfig`.
- `Client.WithApplicationVersion`, sent as `X-Application-Version` when it is 1 to 18 digits and `"0"` otherwise (the collector reads it as a 64-bit integer). `Client.GetApplication`, `GetEnvironment`, `GetApplicationVersion`, `GetAgent`.
- `SDKName`, `SDKVersion`, `SDKAgent`.
- `Context.FinalizeAsync`, which also waits for logger delivery; `Finalize` event type alias (value `"Close"`).
- `Context.GetCustomFieldValueE` (error-returning custom field access) and `CustomFieldValueParser`, an optional interface a custom `VariableParser` can implement to parse `json` custom fields.
- `Context.GetVariableKeysAll` returns every experiment per variable key; `GetVariableKeys` keeps returning one.
- `ErrContextClosed`, `ErrContextClosing`, `ErrContextNotReady`, `ErrUnitAlreadySet`, `ErrUnitBlank` and `UnitError`, for `errors.Is`/`errors.As`. The error texts are unchanged from v1.1.1; `UnitError.Canonical()` gives the cross-SDK text (`Unit '<type>' UID already set.`).
- `Context.SetUnitValue` (string or number UIDs; numbers hashed as the rounded integer, like JS), `GetUnit`, `GetUnits`, `GetAttribute`, `GetAttributes`.
- `HashUnitValue`, `UTF8Bytes`, `Base64UrlNoPadding` helpers; `TypeError`; `future.PanicError`.
- Release workflow: pushing a `vX.Y.Z` tag that matches `SDKVersion` runs the tests and creates the GitHub release.

### Changed
These follow the cross-SDK contract agreed for all ABsmartly SDKs in 2026-11; each says who is affected.
- Audiences and assignment-rule conditions see the client's `application` and `environment` names as attributes (system values first, user-set attributes of the same name win). `app_version` is never injected and nothing new is published. Who is affected: experiments with an enforced audience on `application`/`environment` that no user attribute satisfied; those units got variant 0 and are now randomised normally.
- Audiences are re-evaluated after `SetAttribute`/`SetAttributes`: a cached assignment whose audience result changes is recomputed. Who is affected: code that sets attributes after reading a treatment; a strict audience can move the unit from variant 0 to its assigned variant, with a second exposure.
- Shared variable keys: when several experiments define the same key, `GetVariableValue`/`PeekVariableValue` read the first experiment, by id, that the unit is assigned to or overridden in; `GetVariableValue` exposes each experiment visited. `GetVariableKeys`, `GetVariableExperiment`, `GetVariableAssignment` and `IndexVariables_` use the lowest-id experiment. v1.1.1 used the last experiment in the payload. Who is affected: payloads where two experiments share a variable key.
- Published exposures always include `"ruleOverride"` (false unless a rule chose the variant), and an exposure for an unknown experiment publishes `"unit": null` instead of `""`, as the JS SDK does. Who is affected: anyone comparing publish payloads or `Exposure` event JSON byte for byte.
- `X-Agent` is `absmartly-go-sdk/1.2.0` instead of `absmartly-go-sdk`. The collector stores it verbatim, so it shows in exports. Who is affected: dashboards or filters matching `agent = 'absmartly-go-sdk'` exactly.
- The context fetch (`GET /context`) sends the SDK headers (`X-API-Key`, `X-Agent`, `X-Application`, `X-Environment`, `X-Application-Version`), as the JS SDK does on every request; v1.1.1 sent none. Who is affected: proxies or mocks that reject unexpected headers on the GET.
- `jsonmodels.Experiment` gained `AssignmentRules` and `jsonmodels.Exposure` gained `RuleOverride`, the wire fields of assignment rules. Who is affected: code building these two payload models with unkeyed literals (`jsonmodels.Exposure{1, "exp", ...}`); keyed literals keep compiling. No other v1.1.1 struct changed layout.
- `json` custom fields with the default parser return any JSON value (array, string, number, boolean) instead of only objects (cross-SDK scenarios 53-58); JSON `null` and invalid JSON still return a nil `map[string]interface{}`. Who is affected: code type-asserting every `json` custom field to `map[string]interface{}`.

### Fixed
- Non-ASCII unit UIDs are hashed from their UTF-8 bytes, as in the other SDKs; v1.1.1 dropped every non-ASCII character, so different UIDs collided. Who is affected: units with non-ASCII UIDs are reassigned in running experiments (ASCII UIDs keep their assignments).
- `Publish()` returns the publish error instead of nil, and a failed publish keeps the events queued for the next publish instead of dropping them.
- A failed `Close()` leaves the context open with its events queued and can be retried; the `Close` event is logged only on success, and the refresh timer is not restarted. On success, `Close()` and an empty-queue `CloseAsync()` settle after the `Close` event is logged. Concurrent `CloseAsync` callers share one future, and in-flight publishes are waited for.
- `SetOverride`/`SetCustomAssignment` are allowed after close, like the other SDKs; exposures are no longer queued on a closed context.
- `Refresh()` on a closed context returns instead of panicking; a refresh that finishes after close does not restart the timer.
- The ready, error and refresh events are logged before `WaitUntilReady`/`Refresh` return.
- `SetUnit` with the same UID plus surrounding spaces no longer returns `unit already set`.
- Panics no longer crash the host: transport errors in `GetContextData`, a non-resty response from a custom `HTTPClient`, a data provider or publisher returning no future or panicking, a negative override, a nil `AudienceMatcher` deserializer, event logger panics, and JSON expressions on empty maps, maps without `path`, uncomparable values or sized number types.
- JSON expressions: `eq` null handling, deep equality for arrays and maps (independent of key order), `eq` on `int64`/`uint`/`float32` attributes, and sized numbers in boolean context.
- Data races on `ReadyFuture_`, `RefreshFuture_`, `ClosingFuture_`, `RefreshTimer_`, `Timeout_`, `Attributes_` and `Index_`, and a race in `future.Future.Listen` that could drop a callback; `Future.Join` no longer runs callbacks twice.
- Builder `Timeout` applies to requests; the retry condition no longer panics on a nil response (502 and 503 are retried, as before). TLS 1.2 is the minimum version.
