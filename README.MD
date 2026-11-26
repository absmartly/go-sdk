# ABsmartly Go SDK

Go SDK for [ABsmartly](https://www.absmartly.com/) A/B testing platform.

## Compatibility

The A/B Smartly Go SDK is compatible with Go versions 1.17 and later. (v1.1.1's go.mod said 1.15, but it used `atomic.Value.CompareAndSwap` and so needed Go 1.17 to build.) CI builds and tests on Go 1.17, 1.20, 1.23 and the latest stable Go. The module requires `resty` v2.16.5; it builds with Go 1.17, which CI checks. Upgrading `resty` to v2.17+ in your own module raises your Go floor to 1.23.
It provides both a blocking and an asynchronous interfaces.

`Context.GetVariableKeys()` keeps its v1.1 signature (`map[string]string`), returning, for each key, the first experiment that defines it by experiment id. Use the new `Context.GetVariableKeysAll()` to get every experiment per key (`map[string][]string`, in experiment-id order), as the other ABsmartly SDKs return. When several experiments share a variable key, see [Shared variable keys](#shared-variable-keys).

**Behavior changes:** Read methods (`GetTreatment`, `PeekTreatment`, `GetVariableValue`, `PeekVariableValue`, `GetVariableKeys`, `GetExperiments`, `GetCustomFieldValueKeys`, `GetData`) return `sdk.ErrContextNotReady` (`ABSmartly Context is not yet ready`) before the context is ready; callers must not treat the zero value as a result. After close they return `sdk.ErrContextClosing`/`sdk.ErrContextClosed`; the texts are unchanged from v1.1 (`ABSmartly Context is closing`/`ABSmartly Context is closed`), and the errors can also be matched with `errors.Is`. `GetCustomFieldValue` keeps its signature and returns nil on any error; use the new `GetCustomFieldValueE` to receive the error. Refreshing a context whose initial load failed fetches and installs the data, as in v1.1.1. Panics in event loggers are contained and do not change the publisher's success or failure result.

## Finalization and event delivery

Use `f, err := ctx.FinalizeAsync()` and then `f.Get(context.Background())` when an adapter must return all publish/error and finalize logger events before responding. Failed final publishes return the publisher error, retain queued events, and leave the context retryable. On success, `Close()` returns after the `Close` event has been logged, as in v1.1, and so does `CloseAsync()` when no events are queued. Called from inside one of the context's event loggers, it only waits for the close itself. `CloseAsync()` completes on transport and sealing and is not a logger-delivery barrier. A failed close does not restart the refresh timer.

Logger callbacks may call `Close`, `Publish`, or `Refresh`, including through a captured original context. Do not block on `FinalizeAsync()` inside a logger: it waits for logger callbacks to return.

The finalization event is still `sdk.Close` with the value `"Close"`; `sdk.Finalize` is an alias with the same value.

## Getting Started

### Install the SDK

```bash
go get github.com/absmartly/go-sdk/sdk@latest
```

### Dependencies
```
github.com/go-resty/resty/v2@v2.16.5
```

## Import and Initialize the SDK

Once the SDK is installed, it can be initialized in your project.

### Recommended: Using Builder Pattern

The builder pattern provides the most ergonomic way to initialize the SDK:

```go
package main

import (
    "log"
    "os"
    "github.com/absmartly/go-sdk/sdk"
)

func main() {
    absmartly, err := sdk.NewBuilder().
        Endpoint("https://your-company.absmartly.io/v1").
        ApiKey(os.Getenv("ABSMARTLY_APIKEY")).
        Application(os.Getenv("ABSMARTLY_APPLICATION")).
        Environment(os.Getenv("ABSMARTLY_ENVIRONMENT")).
        Build()

    if err != nil {
        log.Fatalf("Failed to initialize SDK: %v", err)
    }

    // ...
}
```

With optional parameters:

```go
import (
    "log"
    "os"
    "time"
    "github.com/absmartly/go-sdk/sdk"
)

absmartly, err := sdk.NewBuilder().
    Endpoint("https://your-company.absmartly.io/v1").
    ApiKey(os.Getenv("ABSMARTLY_APIKEY")).
    Application(os.Getenv("ABSMARTLY_APPLICATION")).
    Environment(os.Getenv("ABSMARTLY_ENVIRONMENT")).
    Timeout(5 * time.Second).
    Retries(3).
    Build()

if err != nil {
    log.Fatalf("Failed to initialize SDK: %v", err)
}
```

### Advanced: Using Config Structs

For advanced use cases, you can still use the traditional config-based approach:

```go
package main

import (
    "os"
    "github.com/absmartly/go-sdk/sdk"
)

func main() {
    clientConfig := sdk.ClientConfig{
        Endpoint_:    "https://your-company.absmartly.io/v1",
        ApiKey_:      os.Getenv("ABSMARTLY_APIKEY"),
        Application_: os.Getenv("ABSMARTLY_APPLICATION"), // created in the ABSmartly web console
        Environment_: os.Getenv("ABSMARTLY_ENVIRONMENT"), // created in the ABSmartly web console
    }

    sdkConfig := sdk.ABsmartlyConfig{
        Client_: sdk.CreateDefaultClient(clientConfig),
    }

    absmartly := sdk.Create(sdkConfig)

    // ...
}
```

### SDK Options

The builder pattern supports all the same options as the config-based approach.

#### Required Options

| Option       | Type     | Description                                                                                                                                                                   |
| :----------- | :------- | :---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Endpoint     | `string` | The URL to your API endpoint. Most commonly `"https://your-company.absmartly.io/v1"`                                                                                         |
| ApiKey       | `string` | Your API key which can be found on the Web Console.                                                                                                                           |
| Environment  | `string` | The environment of the platform where the SDK is installed. Environments are created on the Web Console and should match the available environments in your infrastructure.   |
| Application  | `string` | The name of the application where the SDK is installed. Applications are created on the Web Console and should match the applications where your experiments will be running. |

#### Optional Options

| Option       | Type                  | Default | Description                                                     |
| :----------- | :-------------------- | :------ | :-------------------------------------------------------------- |
| Timeout      | `time.Duration`       | `3s`    | Request and connection timeout.                                   |
| Retries      | `int`                 | `5`     | The number of retries before the SDK stops trying to connect.   |
| ApplicationVersion | `string`        | `""`    | The version of your application. Sent as the `X-Application-Version` header when it is 1 to 18 digits such as `"1042"`; any other value, or none, is sent as `"0"`. |
| ContextEventLogger | `ContextEventLogger` | `nil` | A callback interface which runs after SDK events.              |

#### Advanced Configuration (Config-Based Approach)

For advanced use cases requiring custom implementations, use the traditional config-based approach:

#### ClientConfig

| Config           | Type     | Required? | Default   | Description                                                                                                                                                                   |
| :--------------- | :------- | :-------: | :-------: | :---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Endpoint_        | `string` |  &#9989;  | `""`      | The URL to your API endpoint. Most commonly `"https://your-company.absmartly.io/v1"`                                                                                         |
| ApiKey_          | `string` |  &#9989;  | `""`      | Your API key which can be found on the Web Console.                                                                                                                           |
| Environment_     | `string` |  &#9989;  | `""`      | The environment of the platform where the SDK is installed. Environments are created on the Web Console and should match the available environments in your infrastructure.   |
| Application_     | `string` |  &#9989;  | `""`      | The name of the application where the SDK is installed. Applications are created on the Web Console and should match the applications where your experiments will be running. |
| MaxRetries_      | `int`    | &#10060;  | `5`       | The number of retries before the SDK stops trying to connect.                                                                                                                 |
| ConnectTimeout_  | `int`    | &#10060;  | `3000`    | An amount of time, in milliseconds, before the SDK will stop trying to connect.                                                                                               |

To send your application version with the config-based approach, call `WithApplicationVersion` on the client and use the client it returns; it does not change the client it is called on:

```go
client := sdk.CreateDefaultClient(clientConfig).WithApplicationVersion("1042")
sdkConfig := sdk.ABsmartlyConfig{Client_: client}
```

The version is sent as the `X-Application-Version` header when it is 1 to 18 digits; otherwise `"0"` is sent.

#### ABsmartlyConfig

| Config                  | Type                  | Required? | Default                             | Description                                                  |
| :---------------------- | :-------------------- | :-------: | :---------------------------------- | :----------------------------------------------------------- |
| Client_                 | `Client`              |  &#9989;  | `nil`                               | The HTTP client instance (use CreateDefaultClient)           |
| ContextEventLogger_     | `ContextEventLogger`  | &#10060;  | See "Using a Custom Event Logger"  | A callback interface which runs after SDK events.            |
| ContextDataProvider_    | `ContextDataProvider` | &#10060;  | `DefaultContextDataProvider`        | Custom provider for fetching context data (advanced usage)   |
| ContextEventHandler_    | `ContextEventHandler` | &#10060;  | `DefaultContextEventHandler`        | Custom handler for publishing events (advanced usage)        |

### Using a Custom Event Logger

The A/B Smartly SDK can be instantiated with an event logger used for all contexts.
In addition, an event logger can be specified when creating a particular context, in the `ContextConfig`.

```go
type ContextEventLogger interface {
    HandleEvent(context Context, eventType EventType, data interface{})
}

type EventType string

const (
    Error    EventType = "Error"
    Ready    EventType = "Ready"
    Refresh  EventType = "Refresh"
    Publish  EventType = "Publish"
    Exposure EventType = "Exposure"
    Goal     EventType = "Goal"
    Close    EventType = "Close"
)
```

**Example Implementation:**

```go
import (
    "log"
    "os"
    "github.com/absmartly/go-sdk/sdk"
)

type CustomEventLogger struct{}

func (l *CustomEventLogger) HandleEvent(context sdk.Context, eventType sdk.EventType, data interface{}) {
    switch eventType {
    case sdk.Exposure:
        exposure := data.(sdk.Exposure)
        log.Printf("Exposed to experiment: %s", exposure.Name)
    case sdk.Goal:
        goal := data.(sdk.GoalAchievement)
        log.Printf("Goal tracked: %s", goal.Name)
    case sdk.Error:
        err := data.(error)
        log.Printf("Error: %v", err)
    case sdk.Ready:
        log.Println("Context is ready")
    case sdk.Refresh:
        log.Println("Context refreshed")
    case sdk.Publish:
        log.Println("Events published")
    case sdk.Close:
        log.Println("Context closed")
    }
}

// Usage with builder pattern (recommended)
absmartly, err := sdk.NewBuilder().
    Endpoint("https://your-company.absmartly.io/v1").
    ApiKey(os.Getenv("ABSMARTLY_APIKEY")).
    Application(os.Getenv("ABSMARTLY_APPLICATION")).
    Environment(os.Getenv("ABSMARTLY_ENVIRONMENT")).
    ContextEventLogger(&CustomEventLogger{}).
    Build()

if err != nil {
    log.Fatalf("Failed to initialize SDK: %v", err)
}

// Or with config structs (advanced)
sdkConfig := sdk.ABsmartlyConfig{
    Client_:             sdk.CreateDefaultClient(clientConfig),
    ContextEventLogger_: &CustomEventLogger{},
}
```

**Event Types**

The data parameter depends on the type of event. Currently, the SDK logs the following events:

| Event      | When                                                       | Data                                              |
| :--------- | :--------------------------------------------------------- | :------------------------------------------------ |
| `Error`    | Context receives an error                                  | `error` object                                    |
| `Ready`    | Context turns ready                                        | `ContextData` used to initialize the context      |
| `Refresh`  | `Context.Refresh()` method succeeds                        | `ContextData` used to refresh the context         |
| `Publish`  | `Context.Publish()` method succeeds                        | `PublishEvent` sent to the A/B Smartly collector  |
| `Exposure` | `Context.GetTreatment()` method succeeds on first exposure | `Exposure` enqueued for publishing                |
| `Goal`     | `Context.Track()` method succeeds                          | `GoalAchievement` enqueued for publishing         |
| `Close`    | `Context.Close()` method succeeds the first time           | `nil`                                             |

## Create a New Context Request

### Synchronously

```go
contextConfig := sdk.ContextConfig{
    Units_: map[string]string{
        "session_id": "5ebf06d8cb5d8137290c4abb64155584fbdb64d8",
    },
}

ctx := absmartly.CreateContext(contextConfig)
_ = ctx.WaitUntilReady()
if ctx.IsFailed() {
    log.Fatal("Failed to initialize context: could not load experiment data")
}

log.Println("Context ready")
```

### Asynchronously

```go
contextConfig := sdk.ContextConfig{
    Units_: map[string]string{
        "session_id": "5ebf06d8cb5d8137290c4abb64155584fbdb64d8",
    },
}

ctx := absmartly.CreateContext(contextConfig)

// Use goroutine for async initialization
go func() {
    _ = ctx.WaitUntilReady()
    if ctx.IsFailed() {
        log.Print("Failed to initialize context: could not load experiment data")
        return
    }
    log.Println("Context ready")
}()

// Continue with other work...
```

### With Prefetched Data

When doing full-stack experimentation, you can create a context on the server-side and reuse the data on the client-side to avoid an additional round-trip.

```go
// Server-side: create initial context
serverConfig := sdk.ContextConfig{
    Units_: map[string]string{
        "session_id": "5ebf06d8cb5d8137290c4abb64155584fbdb64d8",
        "user_id":    "123456",
    },
}

serverCtx := absmartly.CreateContext(serverConfig)
_ = serverCtx.WaitUntilReady()
if serverCtx.IsFailed() {
    log.Fatal("Failed to initialize context: could not load experiment data")
}

// Get the context data to send to client
contextData, err := serverCtx.GetData()
if err != nil {
    log.Fatalf("Failed to read context data: %v", err)
}

// Client-side: create context with prefetched data
clientConfig := sdk.ContextConfig{
    Units_: map[string]string{
        "session_id": "5ebf06d8cb5d8137290c4abb64155584fbdb64d8",
        "user_id":    "123456",
    },
}

clientCtx := absmartly.CreateContextWith(clientConfig, contextData)
// Context is immediately ready, no need to wait
```

### Refreshing the Context with Fresh Experiment Data

For long-running contexts, the context is usually created once when the application is first started.
However, any experiments being tracked in your production code, but started after the context was created, will not be triggered.
To mitigate this, we can use the `RefreshInterval_` option on the context config.

```go
contextConfig := sdk.ContextConfig{
    Units_: map[string]string{
        "session_id": "5ebf06d8cb5d8137290c4abb64155584fbdb64d8",
        "user_id":    "123456",
    },
    RefreshInterval_: (4 * time.Hour).Milliseconds(), // Refresh every 4 hours (the field is in milliseconds)
}

ctx := absmartly.CreateContext(contextConfig)
_ = ctx.WaitUntilReady()
if ctx.IsFailed() {
    log.Fatal("Failed to initialize context: could not load experiment data")
}
```

Alternatively, the `Refresh()` method can be called manually.
The `Refresh()` method pulls updated experiment data from the A/B Smartly collector and will trigger recently started experiments when `GetTreatment()` is called again.

```go
ctx.Refresh() // blocks until the refresh finishes

// or without blocking:
if err := func() error { _, err := ctx.RefreshAsync().Get(context.Background()); return err }(); err != nil {
    log.Printf("Failed to refresh context: %v", err)
}
```

### Setting Extra Units

You can add additional units to a context by calling the `SetUnit()` or the `SetUnits()` method.
This method may be used for example, when a user logs in to your application, and you want to use the new unit type to the context.
Please note that **you cannot override an already set unit type** as that would be a change of identity, and will throw an error. In this case, you must create a new context instead.
The `SetUnit()` and `SetUnits()` methods can be called before the context is ready.

```go
if err := ctx.SetUnit("db_user_id", "1000013"); err != nil {
    log.Printf("Failed to set unit: %v", err)
}

if err := ctx.SetUnits(map[string]string{
    "db_user_id": "1000013",
}); err != nil {
    log.Printf("Failed to set units: %v", err)
}
```

## Basic Usage

### Selecting a Treatment

```go
treatment, err := ctx.GetTreatment("exp_test_experiment")
if err != nil {
    log.Printf("Failed to get treatment: %v", err)
}

if treatment == 0 {
    // user is in control group (variant 0)
} else {
    // user is in treatment group (variant 1)
}
```

### Treatment Variables

```go
defaultButtonColor := "red"
buttonColor, err := ctx.GetVariableValue("button.color", defaultButtonColor)
if err != nil {
    log.Printf("Failed to get variable value: %v", err)
    buttonColor = defaultButtonColor
}
```

### Shared variable keys

When several experiments define the same variable key, `GetVariableValue` and `PeekVariableValue` pick one winner:

- The candidates are the experiments any of whose variants define the key, in experiment-id order.
- The winner is the first candidate the unit is assigned to, overridden in or rule-assigned to. A unit outside a strict audience, or without a unit of the experiment's type, is not assigned.
- The value comes from the winner's selected variant. If that variant does not define the key, you get your default; later experiments are not consulted. A key defined as `null`, `false`, `0` or `""` returns that value.
- Only the winner is exposed. With no winner you get your default and nothing is exposed. `PeekVariableValue` never exposes.

This matches the Java SDK. (v1.1.1 used the last experiment in the payload.)

### Peek at Treatment Variants

Although generally not recommended, it is sometimes necessary to peek at a treatment or variable without triggering an exposure.
The A/B Smartly SDK provides a `PeekTreatment()` method for that.

```go
treatment, err := ctx.PeekTreatment("exp_test_experiment")
if err != nil {
    log.Printf("Failed to peek treatment: %v", err)
}

if treatment == 0 {
    // user is in control group (variant 0)
} else {
    // user is in treatment group
}
```

### Peeking at Variables

```go
variable, err := ctx.PeekVariableValue("my_variable", "default")
if err != nil {
    log.Printf("Failed to peek variable: %v", err)
}
```

### Overriding Treatment Variants

During development, for example, it is useful to force a treatment for an experiment. This can be achieved with the `SetOverride()` and/or `SetOverrides()` methods.
The `SetOverride()` and `SetOverrides()` methods can be called before the context is ready.

```go
ctx.SetOverride("exp_test_experiment", 1) // force variant 1 of treatment

ctx.SetOverrides(map[string]int{
    "exp_test_experiment":     1,
    "exp_another_experiment":  0,
})
```

## Advanced

### Context Attributes

Attributes are used to pass meta-data about the user and/or the request.
They can be used later in the Web Console to create segments or audiences.
The `SetAttribute()` and `SetAttributes()` methods can be called before the context is ready.

```go
ctx.SetAttribute("user_agent", req.Header.Get("User-Agent"))

ctx.SetAttributes(map[string]interface{}{
    "customer_age": "new_customer",
    "user_score":   150,
})
```

The `application` and `environment` names from the client configuration are
always injected into the attribute map that audiences are evaluated against, so
an audience filter can target `application` or `environment` without setting
them explicitly. A value left unset or empty is omitted, so a filter can rely on
it only when it is configured or set explicitly. A user-set attribute of the
same name always takes precedence, including one set after the context is
created. These values are not added to the publish payload; the collector
attaches them to every exposure server-side.

Audience filters and assignment-rule conditions can compare semantic versions
with `semver_eq`, `semver_gt`, `semver_gte`, `semver_lt` and `semver_lte`. A
leading `v` and build metadata are ignored, missing parts count as `0`, and a
pre-release ranks below its release, so `1.2` equals `1.2.0` and `2.0.0-beta`
is less than `2.0.0`:

```json
{"filter": [{"semver_gte": [{"var": "app_version"}, {"value": "2.4.0"}]}]}
```

The SDK does not set `app_version` itself; set it with `SetAttribute`.

### Assignment Rules

An experiment can define assignment rules in the Web Console. They are
evaluated in order and the first one that applies sets the variant:

- an `assign` rule gives a fixed variant;
- a `split` rule divides matching units across the variants by percentages,
  such as `20/30/50`. Units are hashed with the experiment's own seed, so a
  rule split equal to the experiment split keeps every unit on the variant it
  already had.

A rule can be limited to some environments (matched, case-sensitively, against
the client's `Environment`) and to units whose attributes match its
conditions, which use the same JSON expressions as audiences, including the
`application` and `environment` names. A matching rule takes precedence over
the audience and `audienceStrict`, traffic eligibility, full-on and custom
assignments; only `SetOverride` takes precedence over it. Exposures from a rule
carry `ruleOverride: true`, so they are left out of the experiment's analysis.

Rules are re-evaluated when attributes change or a refresh changes them. A
`split` rule that matches before its unit is set gives the experiment's no-unit
result (the full-on variant, or `0`) and is resolved again once the unit is
set, unless it has already been exposed: a user never switches variant or gets
a second exposure. An invalid rule is skipped in favour of the next one; an
`assign` variant out of range, or a split with a different number of values
than the experiment has variants, stops rule evaluation and falls back to
normal assignment.

Older SDK versions ignore assignment rules, so their units fall through to
normal assignment and are analysed. Use this version or later where rules must
apply.

### Holdouts

A holdout keeps a share of units out of a group of experiments, so their
combined effect can be measured. The context payload lists the holdouts
(`ContextPayload.Holdouts`) and each experiment names the holdouts covering it
(`ExperimentData.HoldoutIds`). Nothing needs to be configured: the SDK applies
them as the JavaScript SDK does.

- Each holdout puts the unit in an arm, drawn with the covered experiment's
  unit. Arm 0 holds the unit out. In a three-arm holdout (three values in its
  split) arm 1 holds the unit out of experiments that are not full-on, and a
  full-on experiment is served as usual.
- A unit held out by any of the holdouts covering an experiment gets
  variant `0` and the control variant's variables, and the experiment
  publishes no exposure of its own. An override (`SetOverride`) or a matching
  assignment rule still wins; custom assignments, audiences, traffic and
  full-on do not.
- Every `GetTreatment` or `GetVariableValue` on a covered experiment also
  publishes each covering holdout's exposure, once per holdout and unit type,
  whatever the arm. `PeekTreatment` and `PeekVariableValue` publish nothing.
- When experiments share a variable key (see
  [Shared variable keys](#shared-variable-keys)), a held-out experiment is not
  assigned, so it never wins the key; if no experiment wins, the first
  held-out one supplies its control value rather than the default, and
  `GetVariableValue` publishes the holdout exposures of every experiment it
  checked before the winner, as in the Java SDK.

A unit keeps its arm until the holdout's iteration changes. Holdouts add
nothing to the publish payload of experiments without holdouts (the
`ruleOverride` flag every exposure now carries comes with assignment rules),
and warmed cached reads and exposures allocate nothing more. Creating a
context and rebuilding an assignment each allocate one small extra object.

Holdouts, and the switchback experiments below, reach a context only through
the full payload, `jsonmodels.ContextPayload`, whose experiments are
`jsonmodels.ExperimentData` values: the v1.2.0 `Experiment` plus `HoldoutIds`
and the switchback fields. `jsonmodels.ContextData` and `jsonmodels.Experiment`
keep their v1.2.0 layouts, so existing code, positional literals included,
keeps compiling. `CreateContext` and refreshes with the default client get the
payload with no code change. With prefetched data, use the payload variants:

```go
var payload, _ = sdk.DefaultContextDataDeserializer{}.DeserializePayload(body)
var ctx = sd.CreateContextWithPayload(contextConfig, payload)
ctx.SetPayload(payload) // the SetData of a ContextPayload
```

`Deserialize`, `GetContextData`, `GetData`, `SetData` and `CreateContextWith`
keep working with `ContextData`, which has no holdouts or switchbacks. A custom
`ContextDataDeserializer` or `ContextDataProvider` gives contexts the payload
by also implementing `DeserializePayload` (`ContextPayloadDeserializer`) or
`GetContextPayload` (`ContextPayloadProvider`).

### Switchback Experiments

Switchback serving needs v1.3.0 or later. Earlier versions ignore the
switchback fields and serve such an experiment as a unit experiment on its
`unitType`, which does not follow the schedule.

A switchback experiment randomizes over *clusters* and time rather than over
units: every unit in the same cluster (for example a store's city) gets the
same variant within a period, and the variant changes on a fixed time grid.
The context payload marks it with `ExperimentData.RandomizationType ==
"switchback"`; nothing needs to be configured beyond giving the context the
full payload (see [Holdouts](#holdouts)). The SDK serves it as the
collector and the other ABsmartly SDKs do (see the Java SDK's
[`SWITCHBACK.md`](https://github.com/absmartly/java-sdk/blob/main/SWITCHBACK.md)
for the full rules):

- `ExperimentData.SwitchbackClusters` lists, in order, the unit types linked to
  the experiment and the attribute path holding each one's cluster value
  (`"city"`, `"geo/city"`, `"items/0/city"`). For each linked type in that
  order, the context's unit of that type reads the attribute at its path. The
  first unit that yields a value serves the experiment and the value is the
  cluster. Set the cluster attributes with `SetAttribute` before reading the
  treatment.
- A cluster value is a non-empty string, used as-is, or a whole-number
  integer, used as its decimal text. Every floating-point number (`3.0`
  included), boolean, map, slice, empty string and `nil` gives no value. Go
  keeps `3` and `3.0` apart: an `int` attribute is the integer `3`, a
  `float64` is a float even when whole. Values from a JSON document should be
  decoded with `json.Decoder.UseNumber`, so `json.Number("3")` keys a cluster
  and `json.Number("3.0")` does not (audiences compare a `json.Number` as the
  number it denotes); plain `encoding/json` decodes every
  number as a `float64`, which gives no value. Prefer integer or string
  attributes: a whole `float64` is published as `3`, which the collector
  reads back as an integer when it re-resolves the cluster. A value with its
  own JSON encoding (a struct, a `json.Marshaler` or `encoding.TextMarshaler`,
  `[]byte`, a map with non-string keys) is read from that encoding, as the
  collector reads the published attribute, so a whole `float64` field of a
  struct (written as `3`) keys a cluster.
- With no unit of a linked type set, the experiment does not apply. When no
  unit yields a value, linked units disagree, or the value is not in
  `ExperimentData.SwitchbackClusterValues`, the unit gets variant `0` and no
  exposure is published; only an override (`SetOverride`) still applies.
- Otherwise overrides, assignment rules, strict audiences, full-on, traffic
  (on the serving unit) and custom assignments apply as for a unit
  experiment, and the variant is drawn from the cluster value and the period
  `floor((now - SwitchbackStartAt) / SwitchbackPeriodMs)`. Before the start,
  and when the schedule cannot be served (no start, a start that is not hour
  aligned or outside `[0, 10000-01-01)`, a period of 0 or less, fewer than
  two arms), the draw gives variant `0` with no exposure. Holdouts never
  apply to a switchback experiment.
- The exposure is published under the serving unit, once per period while
  that period is retained (the last two periods per experiment; re-entering
  an older period, after a clock rewind for example, exposes it again), and
  is stamped with the time that chose its period. Its fields are the same as a
  unit experiment's; the cluster attributes are published with their
  timestamps as before.

The cluster of a period is read once, at the period's first read (a peek
included). Changing a cluster attribute mid-period is not supported, even
before the exposure is published or while a rejected exposure is queued again:
the arm cached at the first read stays for the rest of the period, and the
change takes effect from the next period. Set the cluster attributes before
the first read of each period. Payloads without switchback
experiments are served and published exactly as before, and cached reads and
exposures allocate as before (the decoded experiment structs are a little
wider). An attribute value with its own `MarshalJSON` or `MarshalText` is read
through that encoding under the context lock, so the marshaler must not call
back into the context.

### Custom Assignments

Sometimes it may be necessary to override the automatic selection of a
variant. For example, if you wish to have your variant chosen based on
data from an API call. This can be accomplished using the
`SetCustomAssignment()` method.

```go
if err := ctx.SetCustomAssignment("exp_test_experiment", 1); err != nil {
    log.Printf("Failed to set custom assignment: %v", err)
}
```

If you are running multiple experiments and need to choose different
custom assignments for each one, you can do so using the
`SetCustomAssignments()` method.

```go
if err := ctx.SetCustomAssignments(map[string]int{
    "exp_test_experiment":    1,
    "exp_another_experiment": 0,
}); err != nil {
    log.Printf("Failed to set custom assignments: %v", err)
}
```

### Tracking Goals

Goals are created in the A/B Smartly web console.

```go
if err := ctx.Track("payment", map[string]interface{}{
    "item_count":   1,
    "total_amount": 1999.99,
}); err != nil {
    log.Printf("Failed to track goal: %v", err)
}
```

### Publish

Sometimes it is necessary to ensure all events have been published to the A/B Smartly collector, before proceeding.
You can explicitly call the `Publish()` method.

**Synchronously:**

```go
if err := ctx.Publish(); err != nil {
    log.Printf("Failed to publish events: %v", err)
}
```

**Asynchronously:**

```go
go func() {
    if err := ctx.Publish(); err != nil {
        log.Printf("Failed to publish events: %v", err)
    }
}()
```

### Finalize

The `Close()` method will ensure all events have been published to the A/B Smartly collector, like `Publish()`, and will also "seal" the context: methods that could generate an event return an error once the context is closed. `Close()` blocks until the events are published; use `CloseAsync()` to get the result as a future.

**Synchronously:**

```go
ctx.Close() // blocks until all pending events are published
```

**Asynchronously:**

```go
closing, err := ctx.CloseAsync()
if err != nil {
    log.Printf("Failed to close context: %v", err)
} else if _, err := closing.Get(context.Background()); err != nil {
    log.Printf("Failed to publish pending events: %v", err)
}
```

## Platform-Specific Examples

### Using with Gin Web Framework

```go
package main

import (
    "github.com/gin-gonic/gin"
    "github.com/absmartly/go-sdk/sdk"
    "log"
)

var absmartlySDK sdk.ABsmartly

func main() {
    // Initialize SDK once at app startup
    clientConfig := sdk.ClientConfig{
        Endpoint_:    "https://your-company.absmartly.io/v1",
        ApiKey_:      "YOUR-API-KEY",
        Application_: "website",
        Environment_: "production",
    }

    sdkConfig := sdk.ABsmartlyConfig{
        Client_: sdk.CreateDefaultClient(clientConfig),
    }

    absmartlySDK = sdk.Create(sdkConfig)

    r := gin.Default()

    r.GET("/", func(c *gin.Context) {
        sessionID, _ := c.Cookie("session_id")

        contextConfig := sdk.ContextConfig{
            Units_: map[string]string{
                "session_id": sessionID,
            },
        }

        ctx := absmartlySDK.CreateContext(contextConfig)
        _ = ctx.WaitUntilReady()
        if ctx.IsFailed() {
            c.JSON(500, gin.H{"error": "failed to load experiment data"})
            return
        }
        defer ctx.Close()

        treatment, err := ctx.GetTreatment("exp_test_experiment")
        if err != nil {
            c.JSON(500, gin.H{"error": err.Error()})
            return
        }

        if treatment == 0 {
            c.HTML(200, "control.html", nil)
        } else {
            c.HTML(200, "treatment.html", nil)
        }
    })

    r.Run()
}
```

### Using with Echo Framework

```go
package main

import (
    "github.com/labstack/echo/v4"
    "github.com/absmartly/go-sdk/sdk"
    "log"
)

var absmartlySDK sdk.ABsmartly

func main() {
    // Initialize SDK once at app startup
    clientConfig := sdk.ClientConfig{
        Endpoint_:    "https://your-company.absmartly.io/v1",
        ApiKey_:      "YOUR-API-KEY",
        Application_: "website",
        Environment_: "production",
    }

    sdkConfig := sdk.ABsmartlyConfig{
        Client_: sdk.CreateDefaultClient(clientConfig),
    }

    absmartlySDK = sdk.Create(sdkConfig)

    e := echo.New()

    e.GET("/", func(c echo.Context) error {
        cookie, err := c.Cookie("session_id")
        if err != nil {
            return c.JSON(400, map[string]string{"error": "session_id cookie not found"})
        }

        contextConfig := sdk.ContextConfig{
            Units_: map[string]string{
                "session_id": cookie.Value,
            },
        }

        ctx := absmartlySDK.CreateContext(contextConfig)
        _ = ctx.WaitUntilReady()
        if ctx.IsFailed() {
            return c.JSON(500, map[string]string{"error": "failed to load experiment data"})
        }
        defer ctx.Close()

        treatment, err := ctx.GetTreatment("exp_test_experiment")
        if err != nil {
            return c.JSON(500, map[string]string{"error": err.Error()})
        }

        return c.JSON(200, map[string]int{"treatment": treatment})
    })

    e.Start(":8080")
}
```

### Using with Chi Router

```go
package main

import (
    "net/http"
    "fmt"
    "github.com/go-chi/chi/v5"
    "github.com/absmartly/go-sdk/sdk"
    "log"
)

var absmartlySDK sdk.ABsmartly

func main() {
    // Initialize SDK once at app startup
    clientConfig := sdk.ClientConfig{
        Endpoint_:    "https://your-company.absmartly.io/v1",
        ApiKey_:      "YOUR-API-KEY",
        Application_: "website",
        Environment_: "production",
    }

    sdkConfig := sdk.ABsmartlyConfig{
        Client_: sdk.CreateDefaultClient(clientConfig),
    }

    absmartlySDK = sdk.Create(sdkConfig)

    r := chi.NewRouter()

    r.Get("/", func(w http.ResponseWriter, r *http.Request) {
        cookie, err := r.Cookie("session_id")
        if err != nil {
            http.Error(w, "session_id cookie not found", http.StatusBadRequest)
            return
        }

        contextConfig := sdk.ContextConfig{
            Units_: map[string]string{
                "session_id": cookie.Value,
            },
        }

        ctx := absmartlySDK.CreateContext(contextConfig)
        _ = ctx.WaitUntilReady()
        if ctx.IsFailed() {
            http.Error(w, "failed to load experiment data", http.StatusInternalServerError)
            return
        }
        defer ctx.Close()

        treatment, err := ctx.GetTreatment("exp_test_experiment")
        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }

        w.Write([]byte(fmt.Sprintf("Treatment: %d", treatment)))
    })

    http.ListenAndServe(":8080", r)
}
```

## Advanced Request Configuration

### HTTP Timeout and Retries

The SDK's HTTP client is configured on the builder. `Timeout` bounds how long a request may take and `Retries` sets how often a failed request (network error, 502 or 503) is retried:

```go
import (
    "time"
    "github.com/absmartly/go-sdk/sdk"
)

func newSDK() (sdk.ABsmartly, error) {
    return sdk.NewBuilder().
        Endpoint("https://your-company.absmartly.io/v1").
        ApiKey("YOUR-API-KEY").
        Application("website").
        Environment("development").
        Timeout(2 * time.Second).
        Retries(2).
        Build()
}
```

### Bounding How Long You Wait for a Context

The SDK does not accept a `context.Context`, so a Go context cannot cancel its HTTP requests. You can still bound how long your own code waits for the context to become ready; the request itself keeps running in the background and is limited only by the `Timeout` and `Retries` above.

```go
import (
    "context"
    "errors"
    "time"
    "github.com/absmartly/go-sdk/sdk"
)

func createContextWithDeadline(absmartly sdk.ABsmartly, wait time.Duration) (*sdk.Context, error) {
    ctx, cancel := context.WithTimeout(context.Background(), wait)
    defer cancel()

    absmartlyCtx := absmartly.CreateContext(sdk.ContextConfig{
        Units_: map[string]string{
            "session_id": "5ebf06d8cb5d8137290c4abb64155584fbdb64d8",
        },
    })

    done := make(chan struct{})
    go func() {
        absmartlyCtx.WaitUntilReady()
        close(done)
    }()

    select {
    case <-done:
        if absmartlyCtx.IsFailed() {
            return nil, errors.New("failed to load experiment data")
        }
        return absmartlyCtx, nil
    case <-ctx.Done():
        // Stop using the context. Its background request is not cancelled.
        return nil, ctx.Err()
    }
}
```

## Releasing

Run `./test.sh` and `go vet ./...` before releasing.

Go modules are published by git tags. To release:

1. Set `SDKVersion` in `sdk/Version.go` to the new version (it is sent in the
   `X-Agent` header) and move the `CHANGELOG.md` entry from unreleased to that
   version.
2. Merge to `main`, then tag that commit and push the tag:
   `git tag vX.Y.Z && git push origin vX.Y.Z` (the version in `SDKVersion`).

The `Release` workflow checks that the tag matches `SDKVersion`, runs the
tests, creates the GitHub release and asks the Go module proxy to fetch the
new version.

## About A/B Smartly

**A/B Smartly** is the leading provider of state-of-the-art, on-premises, full-stack experimentation platforms for engineering and product teams that want to confidently deploy features as fast as they can develop them.
A/B Smartly's real-time analytics helps engineering and product teams ensure that new features will improve the customer experience without breaking or degrading performance and/or business metrics.

### Have a look at our growing list of clients and SDKs:
- [JavaScript SDK](https://www.github.com/absmartly/javascript-sdk)
- [Java SDK](https://www.github.com/absmartly/java-sdk)
- [PHP SDK](https://www.github.com/absmartly/php-sdk)
- [Swift SDK](https://www.github.com/absmartly/swift-sdk)
- [Vue2 SDK](https://www.github.com/absmartly/vue2-sdk)
- [Vue3 SDK](https://www.github.com/absmartly/vue3-sdk)
- [React SDK](https://www.github.com/absmartly/react-sdk)
- [Python3 SDK](https://www.github.com/absmartly/python3-sdk)
- [Go SDK](https://www.github.com/absmartly/go-sdk) (this package)
- [Ruby SDK](https://www.github.com/absmartly/ruby-sdk)
- [.NET SDK](https://www.github.com/absmartly/dotnet-sdk)
- [Dart SDK](https://www.github.com/absmartly/dart-sdk)
- [Flutter SDK](https://www.github.com/absmartly/flutter-sdk)

## Documentation

- [Full Documentation](https://docs.absmartly.com/)

## License

See [LICENSE](LICENSE) for details.
