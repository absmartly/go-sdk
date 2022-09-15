package main

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
