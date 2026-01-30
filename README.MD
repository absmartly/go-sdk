# ABsmartly Go SDK

Go SDK for [ABsmartly](https://www.absmartly.com/) A/B testing platform.

## Compatibility

The A/B Smartly Go SDK is compatible with Go versions 1.15 and later.
It provides both a blocking and an asynchronous interfaces.

## Getting Started

### Install the SDK

```bash
go get github.com/absmartly/go-sdk/sdk
```

### Dependencies
```
github.com/go-resty/resty/v2@v2.7.0
```

## Import and Initialize the SDK

Once the SDK is installed, it can be initialized in your project.

### Recommended: Using Builder Pattern

The builder pattern provides the most ergonomic way to initialize the SDK:

```go
package main

import (
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
absmartly, err := sdk.NewBuilder().
    Endpoint("https://your-company.absmartly.io/v1").
    ApiKey(os.Getenv("ABSMARTLY_APIKEY")).
    Application(os.Getenv("ABSMARTLY_APPLICATION")).
    Environment(os.Getenv("ABSMARTLY_ENVIRONMENT")).
    Timeout(5 * time.Second).
    Retries(3).
    Build()
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
| Timeout      | `time.Duration`       | `3s`    | Connection timeout duration.                                     |
| Retries      | `int`                 | `5`     | The number of retries before the SDK stops trying to connect.   |
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
if err := ctx.WaitUntilReady(); err != nil {
    log.Fatalf("Failed to initialize context: %v", err)
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
    if err := ctx.WaitUntilReady(); err != nil {
        log.Printf("Failed to initialize context: %v", err)
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
if err := serverCtx.WaitUntilReady(); err != nil {
    log.Fatalf("Failed to initialize context: %v", err)
}

// Get the context data to send to client
contextData := serverCtx.GetContextData()

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
    RefreshInterval_: 4 * time.Hour, // Refresh every 4 hours
}

ctx := absmartly.CreateContext(contextConfig)
if err := ctx.WaitUntilReady(); err != nil {
    log.Fatalf("Failed to initialize context: %v", err)
}
```

Alternatively, the `Refresh()` method can be called manually.
The `Refresh()` method pulls updated experiment data from the A/B Smartly collector and will trigger recently started experiments when `GetTreatment()` is called again.

```go
if err := ctx.Refresh(); err != nil {
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
variable, err := ctx.PeekVariable("my_variable")
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

The `Close()` method will ensure all events have been published to the A/B Smartly collector, like `Publish()`, and will also "seal" the context, returning an error if any method that could generate an event is called afterwards.

**Synchronously:**

```go
if err := ctx.Close(); err != nil {
    log.Printf("Failed to close context: %v", err)
}
```

**Asynchronously:**

```go
go func() {
    if err := ctx.Close(); err != nil {
        log.Printf("Failed to close context: %v", err)
    }
}()
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
        if err := ctx.WaitUntilReady(); err != nil {
            c.JSON(500, gin.H{"error": err.Error()})
            return
        }

        treatment, _ := ctx.GetTreatment("exp_test_experiment")

        if treatment == 0 {
            c.HTML(200, "control.html", nil)
        } else {
            c.HTML(200, "treatment.html", nil)
        }

        ctx.Close()
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
        cookie, _ := c.Cookie("session_id")

        contextConfig := sdk.ContextConfig{
            Units_: map[string]string{
                "session_id": cookie.Value,
            },
        }

        ctx := absmartlySDK.CreateContext(contextConfig)
        if err := ctx.WaitUntilReady(); err != nil {
            return c.JSON(500, map[string]string{"error": err.Error()})
        }

        treatment, _ := ctx.GetTreatment("exp_test_experiment")

        ctx.Close()
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
        cookie, _ := r.Cookie("session_id")

        contextConfig := sdk.ContextConfig{
            Units_: map[string]string{
                "session_id": cookie.Value,
            },
        }

        ctx := absmartlySDK.CreateContext(contextConfig)
        if err := ctx.WaitUntilReady(); err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }

        treatment, _ := ctx.GetTreatment("exp_test_experiment")

        w.Write([]byte(fmt.Sprintf("Treatment: %d", treatment)))
        ctx.Close()
    })

    http.ListenAndServe(":8080", r)
}
```

## Advanced Request Configuration

### Request Timeout with Context

Go supports context-based cancellation. You can use Go's context package for timeouts:

```go
import (
    "context"
    "time"
    "github.com/absmartly/go-sdk/sdk"
)

func createContextWithTimeout() (*sdk.Context, error) {
    ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
    defer cancel()

    contextConfig := sdk.ContextConfig{
        Units_: map[string]string{
            "session_id": "5ebf06d8cb5d8137290c4abb64155584fbdb64d8",
        },
    }

    absmartlyCtx := absmartlySDK.CreateContext(contextConfig)

    done := make(chan error, 1)
    go func() {
        done <- absmartlyCtx.WaitUntilReady()
    }()

    select {
    case err := <-done:
        if err != nil {
            return nil, err
        }
        return absmartlyCtx, nil
    case <-ctx.Done():
        return nil, ctx.Err()
    }
}
```

### Request Cancellation

```go
import (
    "context"
    "github.com/absmartly/go-sdk/sdk"
)

func createContextWithCancellation() (*sdk.Context, context.CancelFunc, error) {
    ctx, cancel := context.WithCancel(context.Background())

    contextConfig := sdk.ContextConfig{
        Units_: map[string]string{
            "session_id": "5ebf06d8cb5d8137290c4abb64155584fbdb64d8",
        },
    }

    absmartlyCtx := absmartlySDK.CreateContext(contextConfig)

    done := make(chan error, 1)
    go func() {
        done <- absmartlyCtx.WaitUntilReady()
    }()

    select {
    case err := <-done:
        if err != nil {
            return nil, cancel, err
        }
        return absmartlyCtx, cancel, nil
    case <-ctx.Done():
        return nil, cancel, ctx.Err()
    }
}

// Usage
absmartlyContext, cancel, err := createContextWithCancellation()
if err != nil {
    log.Fatal(err)
}

// Cancel if needed (e.g., user navigates away)
// cancel()
```

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