package service

import (
	"encoding/json"
	"sync"
	"time"
)

// EventType identifies the kind of event published on the bus.
type EventType string

const (
	// Project events
	EventProjectsLoaded   EventType = "projects.loaded"
	EventProjectAdded     EventType = "project.added"
	EventProjectRemoved   EventType = "project.removed"
	EventProjectRescan    EventType = "project.rescan"

	// Workspace events
	EventWorkspaceCreated  EventType = "workspace.created"
	EventWorkspaceDeleted  EventType = "workspace.deleted"
	EventWorkspaceRenamed  EventType = "workspace.renamed"
	EventWorkspaceArchived EventType = "workspace.archived"

	// Tab events (Claude structured)
	EventTabCreated         EventType = "tab.created"
	EventTabStateChanged    EventType = "tab.state_changed"
	EventTabMessageReceived EventType = "tab.message"
	EventTabClosed          EventType = "tab.closed"
	EventTabCostUpdated     EventType = "tab.cost_updated"

	// Git events
	EventGitStatusChanged EventType = "git.status_changed"

	// Config events
	EventSettingsChanged    EventType = "config.settings_changed"
	EventProfileCreated     EventType = "config.profile_created"
	EventProfileDeleted     EventType = "config.profile_deleted"
	EventPermissionsChanged EventType = "config.permissions_changed"

	// Permission request events
	EventPermissionRequest EventType = "permission.request"

	// Group events
	EventGroupCreated          EventType = "group.created"
	EventGroupDeleted          EventType = "group.deleted"
	EventGroupWorkspaceCreated EventType = "group.workspace_created"
	EventGroupWorkspaceDeleted EventType = "group.workspace_deleted"

	// Notifications
	EventToast EventType = "toast"
)

// Event is a single occurrence published on the event bus.
type Event struct {
	Type      EventType       `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// NewEvent creates an event with the current timestamp and a JSON-marshaled payload.
// If payload is nil, the event has no payload.
func NewEvent(eventType EventType, payload any) Event {
	var raw json.RawMessage
	if payload != nil {
		raw, _ = json.Marshal(payload)
	}
	return Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Payload:   raw,
	}
}

// subscriber tracks a single subscriber channel.
type subscriber struct {
	ch chan Event
}

// EventBus provides typed publish/subscribe for server events.
// It is safe for concurrent use.
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[string]*subscriber
}

// NewEventBus creates a new event bus.
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[string]*subscriber),
	}
}

// Subscribe creates a new subscription with the given ID and buffer size.
// Returns a channel that receives events. The caller must call Unsubscribe
// when done to avoid leaking goroutines.
func (eb *EventBus) Subscribe(id string, bufSize int) <-chan Event {
	if bufSize < 1 {
		bufSize = 64
	}
	ch := make(chan Event, bufSize)
	eb.mu.Lock()
	eb.subscribers[id] = &subscriber{ch: ch}
	eb.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscription and closes its channel.
func (eb *EventBus) Unsubscribe(id string) {
	eb.mu.Lock()
	sub, ok := eb.subscribers[id]
	if ok {
		delete(eb.subscribers, id)
	}
	eb.mu.Unlock()

	if ok && sub.ch != nil {
		close(sub.ch)
	}
}

// Publish sends an event to all current subscribers.
// Slow subscribers that have full buffers will have events dropped.
func (eb *EventBus) Publish(event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	eb.mu.RLock()
	defer eb.mu.RUnlock()

	for _, sub := range eb.subscribers {
		select {
		case sub.ch <- event:
		default:
			// Drop event for slow subscriber rather than blocking
		}
	}
}

// SubscriberCount returns the number of active subscribers.
func (eb *EventBus) SubscriberCount() int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return len(eb.subscribers)
}
