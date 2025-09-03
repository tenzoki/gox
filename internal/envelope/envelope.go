package envelope

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Envelope wraps all messages with metadata for routing, tracing, and debugging
type Envelope struct {
	// Core identification
	ID            string `json:"id"`                       // Unique message ID
	CorrelationID string `json:"correlation_id,omitempty"` // For request/response tracking

	// Routing information
	Source      string `json:"source"`       // Source agent ID
	Destination string `json:"destination"`  // Target topic/pipe
	MessageType string `json:"message_type"` // Semantic message type

	// Timing and sequencing
	Timestamp time.Time `json:"timestamp"`          // Message creation time
	TTL       int64     `json:"ttl,omitempty"`      // Time-to-live in seconds
	Sequence  int64     `json:"sequence,omitempty"` // Message sequence number

	// Content and metadata
	Payload    json.RawMessage        `json:"payload"`              // Actual message data
	Headers    map[string]string      `json:"headers,omitempty"`    // Custom headers
	Properties map[string]interface{} `json:"properties,omitempty"` // Additional properties

	// Tracing and debugging
	TraceID  string   `json:"trace_id,omitempty"`  // Distributed tracing ID
	SpanID   string   `json:"span_id,omitempty"`   // Tracing span ID
	HopCount int      `json:"hop_count,omitempty"` // Number of processing hops
	Route    []string `json:"route,omitempty"`     // Processing path history

	// Quality of service
	Priority   int  `json:"priority,omitempty"`   // Message priority (0-9, 9=highest)
	Persistent bool `json:"persistent,omitempty"` // Should message survive broker restart
}

// NewEnvelope creates a new envelope with basic required fields
func NewEnvelope(source, destination, messageType string, payload interface{}) (*Envelope, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	return &Envelope{
		ID:          uuid.New().String(),
		Source:      source,
		Destination: destination,
		MessageType: messageType,
		Timestamp:   time.Now(),
		Payload:     payloadBytes,
		Headers:     make(map[string]string),
		Properties:  make(map[string]interface{}),
		Route:       make([]string, 0),
	}, nil
}

// NewReplyEnvelope creates a reply envelope for request/response patterns
func NewReplyEnvelope(originalEnvelope *Envelope, source string, payload interface{}) (*Envelope, error) {
	replyEnvelope, err := NewEnvelope(source, originalEnvelope.Source, "reply", payload)
	if err != nil {
		return nil, err
	}

	replyEnvelope.CorrelationID = originalEnvelope.ID
	replyEnvelope.TraceID = originalEnvelope.TraceID
	replyEnvelope.SpanID = uuid.New().String()

	return replyEnvelope, nil
}

// AddHop records that this message was processed by an agent
func (e *Envelope) AddHop(agentID string) {
	e.HopCount++
	e.Route = append(e.Route, agentID)
}

// SetHeader sets a custom header
func (e *Envelope) SetHeader(key, value string) {
	if e.Headers == nil {
		e.Headers = make(map[string]string)
	}
	e.Headers[key] = value
}

// GetHeader retrieves a custom header
func (e *Envelope) GetHeader(key string) (string, bool) {
	if e.Headers == nil {
		return "", false
	}
	value, exists := e.Headers[key]
	return value, exists
}

// SetProperty sets a custom property
func (e *Envelope) SetProperty(key string, value interface{}) {
	if e.Properties == nil {
		e.Properties = make(map[string]interface{})
	}
	e.Properties[key] = value
}

// GetProperty retrieves a custom property
func (e *Envelope) GetProperty(key string) (interface{}, bool) {
	if e.Properties == nil {
		return nil, false
	}
	value, exists := e.Properties[key]
	return value, exists
}

// UnmarshalPayload unmarshals the payload into the provided struct
func (e *Envelope) UnmarshalPayload(v interface{}) error {
	return json.Unmarshal(e.Payload, v)
}

// IsExpired checks if the message has exceeded its TTL
func (e *Envelope) IsExpired() bool {
	if e.TTL <= 0 {
		return false
	}
	return time.Now().Unix() > e.Timestamp.Unix()+e.TTL
}

// Clone creates a deep copy of the envelope
func (e *Envelope) Clone() *Envelope {
	clone := *e

	// Deep copy maps
	if e.Headers != nil {
		clone.Headers = make(map[string]string)
		for k, v := range e.Headers {
			clone.Headers[k] = v
		}
	}

	if e.Properties != nil {
		clone.Properties = make(map[string]interface{})
		for k, v := range e.Properties {
			clone.Properties[k] = v
		}
	}

	// Deep copy route
	if e.Route != nil {
		clone.Route = make([]string, len(e.Route))
		copy(clone.Route, e.Route)
	}

	// Copy payload
	if e.Payload != nil {
		clone.Payload = make(json.RawMessage, len(e.Payload))
		copy(clone.Payload, e.Payload)
	}

	return &clone
}

// ToJSON serializes the envelope to JSON
func (e *Envelope) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// FromJSON deserializes an envelope from JSON
func FromJSON(data []byte) (*Envelope, error) {
	var envelope Envelope
	err := json.Unmarshal(data, &envelope)
	return &envelope, err
}

// MessageSize returns the approximate size of the envelope in bytes
func (e *Envelope) MessageSize() int {
	data, err := e.ToJSON()
	if err != nil {
		return 0
	}
	return len(data)
}

// Validate checks if the envelope has all required fields
func (e *Envelope) Validate() error {
	if e.ID == "" {
		return &ValidationError{Field: "id", Message: "envelope ID is required"}
	}
	if e.Source == "" {
		return &ValidationError{Field: "source", Message: "source agent ID is required"}
	}
	if e.Destination == "" {
		return &ValidationError{Field: "destination", Message: "destination is required"}
	}
	if e.MessageType == "" {
		return &ValidationError{Field: "message_type", Message: "message type is required"}
	}
	if e.Payload == nil {
		return &ValidationError{Field: "payload", Message: "payload is required"}
	}
	return nil
}

// ValidationError represents an envelope validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}
