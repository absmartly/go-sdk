package sdk

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
	// Finalize is an alias for Close; the event value stays "Close" for
	// compatibility with loggers written against v1.1.x.
	Finalize EventType = Close
)
