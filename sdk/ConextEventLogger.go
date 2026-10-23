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
	Finalize EventType = "finalize"
	// Close is retained as a source-compatible alias for the finalize event.
	Close EventType = Finalize
)
