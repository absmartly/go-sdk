# ABsmartly Go SDK

Go SDK for [ABsmartly](https://www.absmartly.com/) A/B testing platform.

## Compatibility

The A/B Smartly Go SDK is compatible with Go versions 1.20 and later. CI builds and tests on Go 1.20, 1.23 and the latest stable Go. The module pins `resty` v2.16.5, the last release that supports Go 1.20; upgrading `resty` to v2.17+ in your own module raises your Go floor to 1.23.
It provides both a blocking and an asynchronous interfaces.

`Context.GetVariableKeys()` keeps its v1.1 signature (`map[string]string`), returning, for each key, the first experiment that defines it by experiment id. Use the new `Context.GetVariableKeysAll()` to get every experiment per key (`map[string][]string`, in experiment-id order), as the other ABsmartly SDKs return. When several experiments share a variable key, `GetVariableValue`/`PeekVariableValue` read from the first experiment, by id, that the unit is assigned to or overridden in; `GetVariableValue` exposes each experiment visited along the way and `PeekVariableValue` exposes none (v1.1.1 used the last experiment in the payload); this matches the JS SDK.

**Behavior changes:** Read methods (`GetTreatment`, `PeekTreatment`, `GetVariableValue`, `PeekVariableValue`, `GetVariableKeys`, `GetExperiments`, `GetCustomFieldValueKeys`, `GetData`) return `sdk.ErrContextNotReady` (`ABSmartly Context is not yet ready`) before the context is ready; callers must not treat the zero value as a result. After close they return `sdk.ErrContextClosing`/`sdk.ErrContextClosed`; the texts are unchanged from v1.1 (`ABSmartly Context is closing`/`ABSmartly Context is closed`), and the errors can also be matched with `errors.Is`. `GetCustomFieldValue` keeps its signature and returns nil on any error; use the new `GetCustomFieldValueE` to receive the error. Refreshing a context whose initial load failed is now a successful no-op; it does not call the data provider or replace the failed context's data. Create a new context to retry initialization. Panics in event loggers are contained and do not change the publisher's success or failure result.

## Finalization and event delivery

Use `f, err := ctx.FinalizeAsync()` and then `f.Get(context.Background())` when an adapter must return all publish/error and finalize logger events before responding. Failed final publishes return the publisher error, retain queued events, and leave the context retryable. On success, `Close()` returns after the `Close` event has been logged, as in v1.1, and so does `CloseAsync()` when no events are queued. Called from inside one of the context's event loggers, it only waits for the close itself. `CloseAsync()` completes on transport and sealing and is not a logger-delivery barrier. A failed close does not restart the refresh timer.

Logger callbacks may call `Close`, `Publish`, or `Refresh`, including through a captured original context. Do not block on `FinalizeAsync()` inside a logger: it waits for logger callbacks to return.

The finalization event is still `sdk.Close` with the value `"Close"`; `sdk.Finalize` is an alias with the same value.

## Getting Started

### Install the SDK

```bash
go get github.com/absmartly/go-sdk/sdk
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
| ApplicationVersion | `string`        | `""`    | The version of your application. Sent as the `X-Application-Version` header when it is a whole number such as `"1042"`; any other value, or none, is sent as `"0"`. |
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
| ApplicationVersion_ | `string` | &#10060; | `""`     | The version of your application. Sent as the `X-Application-Version` header when it is a whole number; otherwise `"0"` is sent.                                         |
| MaxRetries_      | `int`    | &#10060;  | `5`       | The number of retries before the SDK stops trying to connect.                                                                                                                 |
| ConnectTimeout_  | `int`    | &#10060;  | `3000`    | An amount of time, in milliseconds, before the SDK will stop trying to connect.                                                                                               |

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

var absmartlySDK *sdk.ABsmartly

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

var absmartlySDK *sdk.ABsmartly

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

var absmartlySDK *sdk.ABsmartly

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

Go modules are published by git tags. To release:

1. Set `SDKVersion` in `sdk/Version.go` to the new version (it is sent in the
   `X-Agent` header) and move the `CHANGELOG.md` entry from unreleased to that
   version.
2. Merge to `main`, then tag that commit and push the tag:
   `git tag v1.2.0 && git push origin v1.2.0`.

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
