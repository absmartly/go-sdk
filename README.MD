# A/B Smartly SDK

A/B Smartly - Go SDK

## Compatibility

The A/B Smartly Go SDK is compatible with Go versions 1.15 and later.
It provides both a blocking and an asynchronous interfaces.

## Getting Started

### Install the SDK
 ```
   go get github.com/absmartly/go-sdk/sdk
 ```

### Dependencies
```
github.com/go-resty/resty/v2@v2.7.0
```


## Import and Initialize the SDK

Once the SDK is installed, it can be initialized in your project.
```go

    func main() {
        var clientConfig = ClientConfig{
            Endpoint_:    "https://acme.absmartly.io/v1",
            ApiKey_:      os.Getenv("ABSMARTLY_APIKEY"), 
            Application_: os.Getenv(`ABSMARTLY_APPLICATION`), // created in the ABSmartly web console 
            Environment_: os.Getenv(`ABSMARTLY_ENVIRONMENT`) // created in the ABSmartly web console
        }
        var sdkConfig = ABSmartlyConfig{Client_: CreateDefaultClient(clientConfig)}
    
        var sdk = Create(sdkConfig)
    
        // ...
    }
```

**SDK Options**

| Config      | Type                                          | Required? |                 Default                 | Description                                                                                                                                                                   |
| :---------- |:----------------------------------------------| :-------: |:---------------------------------------:|:------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Endpoint_    | `string`                                      |  &#9989;  |               `undefined`               | The URL to your API endpoint. Most commonly `"your-company.absmartly.io"`                                                                                                     |
| ApiKey_      | `string`                                      |  &#9989;  |               `undefined`               | Your API key which can be found on the Web Console.                                                                                                                           |
| Environment_ | `"production"` or `"development"`             |  &#9989;  |               `undefined`               | The environment of the platform where the SDK is installed. Environments are created on the Web Console and should match the available environments in your infrastructure.   |
| Application_ | `string`                                      |  &#9989;  |               `undefined`               | The name of the application where the SDK is installed. Applications are created on the Web Console and should match the applications where your experiments will be running. |
| MaxRetries_     | `number`                                      | &#10060;  |                   `5`                   | The number of retries before the SDK stops trying to connect.                                                                                                                 |
| ConnectTimeout_     | `number`                                      | &#10060;  |                 `3000`                  | An amount of time, in millisecond, before the SDK will stop trying to connect.                                                                                                |
| ContextEventLogger_ | `(self, event_type: EventType, data: object)` | &#10060;  | See "Using a Custom Event Logger" below | A callback function which runs after SDK events.                                                                                                                              

#### Using a custom Event Logger
The A/B Smartly SDK can be instantiated with an event logger used for all contexts.
In addition, an event logger can be specified when creating a particular context, in the `ContextConfig`.
```go
    type ContextEventLogger interface {
        HandleEvent(context Context, types EventType, data interface{})
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
The data parameter depends on the type of event.
Currently, the SDK logs the following events:

| event | when                                                       | data |
|:---: |------------------------------------------------------------|---|
| `Error` | `Context` receives an error                                | `Throwable` object |
| `Ready` | `Context` turns ready                                      | `ContextData` used to initialize the context |
| `Refresh` | `Context.refresh()` method succeeds                        | `ContextData` used to refresh the context |
| `Publish` | `Context.publish()` method succeeds                        | `PublishEvent` sent to the A/B Smartly event collector |
| `Exposure` | `Context.getTreatment()` method succeeds on first exposure | `Exposure` enqueued for publishing |
| `Goal` | `Context.track()` method succeeds                          | `GoalAchievement` enqueued for publishing |
| `Close` | `Context.close()` method succeeds the first time           | `null` |

## Create a New Context Request

**Synchronously**
```go
    // define a new context request
    var contextConfig = ContextConfig{}
    
    var ctx = sdk.CreateContext(contextConfig)
    ctx.WaitUntilReady()
```

**Asynchronously**
```go
    // define a new context request
    var contextConfig = ContextConfig{}
    
    var ctx = sdk.CreateContext(contextConfig)RIJJHH
    ctx.WaitUntilReadyAsync()
```

**With Prefetched Data**
```go
    // define a new context request
    var contextConfig = ContextConfig{Units_: map[string]string{
    "session_id": "bf06d8cb5d8137290c4abb64155584fbdb64d8",
    "user_id":    "123456",
    }}
    
    var ctx = sdk.CreateContext(contextConfig)
    ctx.WaitUntilReady()
```

**Refreshing the Context with Fresh Experiment Data**
For long-running contexts, the context is usually created once when the application is first started.
However, any experiments being tracked in your production code, but started after the context was created, will not be triggered.
To mitigate this, we can use the `setRefreshInterval()` method on the context config.

```go
    var contextConfig = ContextConfig{Units_: map[string]string{
            "session_id": "bf06d8cb5d8137290c4abb64155584fbdb64d8",
            "user_id":    "123456",
    }, RefreshInteval_: 4 * Time.Hour}
```

Alternatively, the `Refresh()` method can be called manually.
The `Refresh()` method pulls updated experiment data from the A/B Smartly collector and will trigger recently started experiments when `GetTreatment()` is called again.
```go
    context.Refresh()
```

**Setting Extra Units**
You can add additional units to a context by calling the `SetUnit()` or the `SetUnits()` method.
This method may be used for example, when a user logs in to your application, and you want to use the new unit type to the context.
Please note that **you cannot override an already set unit type** as that would be a change of identity, and will throw an exception. In this case, you must create a new context instead.
The `SetUnit()` and `SetUnits()` methods can be called before the context is ready.

```go
    context.SetUnit("db_user_id", "1000013");
    
    context.setUnits(map[string]string{
            "db_user_id": "1000013",
    }
```

## Basic Usage

#### Selecting a treatment
```go
    var res, _ = context.GetTreament("exp_test_experiment")
    if res == 0 {
            // user is in control group (variant 0)
    } else {
            // user is in treatment group
    }
```

### Treatment Variables

```go
     var res, err = context.GetVariableValue(key, 17)
```

#### Peek at treatment variants
Although generally not recommended, it is sometimes necessary to peek at a treatment or variable without triggering an exposure.
The A/B Smartly SDK provides a `PeekTreatment()` method for that.

```go
    var res, _ = context.PeekTreatment("exp_test_experiment")
    if res == 0 {
        // user is in control group (variant 0)
    } else {
        // user is in treatment group
    }
```

##### Peeking at variables
```go
var variable = context.PeekVariable("my_variable")
```

#### Overriding treatment variants
During development, for example, it is useful to force a treatment for an experiment. This can be achieved with the `Override()` and/or `Overrides()` methods.
The `SetOverride()` and `SetOverrides()` methods can be called before the context is ready.
```go
    context.SetOverride("exp_test_experiment", 1) // force variant 1 of treatment
    context.SetOverrides(map[string]int{
        "exp_test_experiment": 1,
        "exp_another_experiment": 0
    })
```

## Advanced

### Context Attributes
Attributes are used to pass meta-data about the user and/or the request.
They can be used later in the Web Console to create segments or audiences.
The `SetAttribute()` and `SetAttributes()` methods can be called before the context is ready.
```go
    context.SetAttribute("user_agent", req.GetHeader("User-Agent"));
    
    context.setAttributes(map[string]string{
            "customer_age": "new_customer",
    }
```

### Custom Assignments

Sometimes it may be necessary to override the automatic selection of a
variant. For example, if you wish to have your variant chosen based on
data from an API call. This can be accomplished using the
`SetCustomAssignment()` method.

```go
    var err = context.SetCustomAssignment("db_user_id", 1)
```

If you are running multiple experiments and need to choose different
custom assignments for each one, you can do so using the
`SetCustomAssignments()` method.

```go
    var err = context.SetCustomAssignments(map[string]int{"db_user_id2": 1})
```

### Publish
Sometimes it is necessary to ensure all events have been published to the A/B Smartly collector, before proceeding.
You can explicitly call the `Publish()` or `PublishAsync()` methods.
```go
    context.Publish()
```

#### Finalize
The `Close()` and `CloseAsync()` methods will ensure all events have been published to the A/B Smartly collector, like `Publish()`, and will also "seal" the context, throwing an error if any method that could generate an event is called.
```go
    context.Close()
```

#### Tracking Goals
Goals are created in the A/B Smartly web console.
```go
    context.Track("payment", map[string]interface{}{
            "item_count": 1,
            "total_amount": 1999.99
    })
```



## About A/B Smartly
**A/B Smartly** is the leading provider of state-of-the-art, on-premises, full-stack experimentation platforms for engineering and product teams that want to confidently deploy features as fast as they can develop them.
A/B Smartly's real-time analytics helps engineering and product teams ensure that new features will improve the customer experience without breaking or degrading performance and/or business metrics.

### Have a look at our growing list of clients and SDKs:
- [Java SDK](https://www.github.com/absmartly/java-sdk)
- [JavaScript SDK](https://www.github.com/absmartly/javascript-sdk)
- [PHP SDK](https://www.github.com/absmartly/php-sdk)
- [Swift SDK](https://www.github.com/absmartly/swift-sdk)
- [Vue2 SDK](https://www.github.com/absmartly/vue2-sdk)
- [Python SDK](https://github.com/absmartly/python3-sdk)
- [Ruby SDK](https://www.github.com/absmartly/ruby-sdk)