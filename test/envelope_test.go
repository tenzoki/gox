package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/tenzoki/gox/internal/envelope"
)

// TestEnvelopeCreation tests envelope creation and basic properties
func TestEnvelopeCreation(t *testing.T) {
	testData := map[string]interface{}{
		"message": "test message",
		"value":   42,
		"nested": map[string]interface{}{
			"key": "value",
		},
	}

	env, err := envelope.NewEnvelope("test-agent", "test-topic", "test-message", testData)
	if err != nil {
		t.Fatalf("Failed to create envelope: %v", err)
	}

	// Test basic properties
	if env.Source != "test-agent" {
		t.Errorf("Expected source 'test-agent', got %s", env.Source)
	}

	if env.Destination != "test-topic" {
		t.Errorf("Expected destination 'test-topic', got %s", env.Destination)
	}

	if env.MessageType != "test-message" {
		t.Errorf("Expected message type 'test-message', got %s", env.MessageType)
	}

	if env.ID == "" {
		t.Error("Expected non-empty ID")
	}

	if env.Timestamp.IsZero() {
		t.Error("Expected non-zero timestamp")
	}

	// Test data integrity
	if env.Payload == nil {
		t.Fatal("Expected payload to be set")
	}

	var payload map[string]interface{}
	if err := env.UnmarshalPayload(&payload); err != nil {
		t.Fatalf("Failed to unmarshal payload: %v", err)
	}

	if msg, ok := payload["message"]; !ok || msg != "test message" {
		t.Error("Payload not properly set")
	}
}

// TestEnvelopeTimestampPrecision tests timestamp precision
func TestEnvelopeTimestampPrecision(t *testing.T) {
	env1, err := envelope.NewEnvelope("test-agent", "test-topic", "test", nil)
	if err != nil {
		t.Fatalf("Failed to create envelope1: %v", err)
	}

	time.Sleep(1 * time.Millisecond) // Ensure time difference

	env2, err := envelope.NewEnvelope("test-agent", "test-topic", "test", nil)
	if err != nil {
		t.Fatalf("Failed to create envelope2: %v", err)
	}

	if !env2.Timestamp.After(env1.Timestamp) {
		t.Error("Expected env2 timestamp to be after env1")
	}

	// Test that timestamps are recent (within last second)
	now := time.Now()
	if now.Sub(env1.Timestamp) > time.Second {
		t.Error("Envelope timestamp is too old")
	}
}

// TestEnvelopeIDUniqueness tests that envelope IDs are unique
func TestEnvelopeIDUniqueness(t *testing.T) {
	ids := make(map[string]bool)

	for i := 0; i < 1000; i++ {
		env, err := envelope.NewEnvelope("test-agent", "test-topic", "test", nil)
		if err != nil {
			t.Fatalf("Failed to create envelope %d: %v", i, err)
		}

		if ids[env.ID] {
			t.Fatalf("Duplicate ID found: %s", env.ID)
		}
		ids[env.ID] = true
	}
}

// TestEnvelopeJSONSerialization tests JSON marshaling/unmarshaling
func TestEnvelopeJSONSerialization(t *testing.T) {
	originalData := map[string]interface{}{
		"string":  "test",
		"number":  42,
		"boolean": true,
		"array":   []interface{}{1, 2, 3},
		"object": map[string]interface{}{
			"nested": "value",
		},
	}

	original, err := envelope.NewEnvelope("test-agent", "test-topic", "test-serialization", originalData)
	if err != nil {
		t.Fatalf("Failed to create envelope: %v", err)
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal envelope: %v", err)
	}

	// Unmarshal from JSON
	restored, err := envelope.FromJSON(jsonData)
	if err != nil {
		t.Fatalf("Failed to unmarshal envelope: %v", err)
	}

	// Verify all fields
	if restored.ID != original.ID {
		t.Errorf("ID mismatch: expected %s, got %s", original.ID, restored.ID)
	}

	if restored.Source != original.Source {
		t.Errorf("Source mismatch: expected %s, got %s", original.Source, restored.Source)
	}

	if restored.Destination != original.Destination {
		t.Errorf("Destination mismatch: expected %s, got %s", original.Destination, restored.Destination)
	}

	if restored.MessageType != original.MessageType {
		t.Errorf("MessageType mismatch: expected %s, got %s", original.MessageType, restored.MessageType)
	}

	// Timestamp comparison (allowing for some precision loss)
	timeDiff := original.Timestamp.Sub(restored.Timestamp)
	if timeDiff < -time.Microsecond || timeDiff > time.Microsecond {
		t.Errorf("Timestamp mismatch: expected %v, got %v", original.Timestamp, restored.Timestamp)
	}

	// Verify payload integrity
	var originalPayload, restoredPayload map[string]interface{}

	if err := original.UnmarshalPayload(&originalPayload); err != nil {
		t.Fatalf("Failed to unmarshal original payload: %v", err)
	}

	if err := restored.UnmarshalPayload(&restoredPayload); err != nil {
		t.Fatalf("Failed to unmarshal restored payload: %v", err)
	}

	if restoredPayload["string"] != "test" {
		t.Error("String data not preserved")
	}

	if restoredPayload["number"].(float64) != 42 { // JSON numbers are float64
		t.Error("Number data not preserved")
	}

	if restoredPayload["boolean"] != true {
		t.Error("Boolean data not preserved")
	}
}

// TestEnvelopeWithNilData tests envelope creation with nil data
func TestEnvelopeWithNilData(t *testing.T) {
	env, err := envelope.NewEnvelope("test-agent", "test-topic", "empty", nil)
	if err != nil {
		t.Fatalf("Failed to create envelope with nil data: %v", err)
	}

	if env.Payload == nil {
		t.Error("Expected payload to be non-nil (even for nil data)")
	}

	// Should still be serializable
	jsonData, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("Failed to marshal envelope with nil data: %v", err)
	}

	restored, err := envelope.FromJSON(jsonData)
	if err != nil {
		t.Fatalf("Failed to unmarshal envelope with nil data: %v", err)
	}

	if restored.ID != env.ID {
		t.Error("ID not preserved after serialization")
	}
}

// TestEnvelopeHeaders tests header functionality
func TestEnvelopeHeaders(t *testing.T) {
	env, err := envelope.NewEnvelope("test-agent", "test-topic", "header-test", "test data")
	if err != nil {
		t.Fatalf("Failed to create envelope: %v", err)
	}

	// Test setting headers
	env.SetHeader("content-type", "application/json")
	env.SetHeader("priority", "high")

	// Test getting headers
	contentType, exists := env.GetHeader("content-type")
	if !exists || contentType != "application/json" {
		t.Error("Header 'content-type' not set correctly")
	}

	priority, exists := env.GetHeader("priority")
	if !exists || priority != "high" {
		t.Error("Header 'priority' not set correctly")
	}

	// Test non-existent header
	_, exists = env.GetHeader("non-existent")
	if exists {
		t.Error("Non-existent header should not exist")
	}
}

// TestEnvelopeProperties tests property functionality
func TestEnvelopeProperties(t *testing.T) {
	env, err := envelope.NewEnvelope("test-agent", "test-topic", "property-test", "test data")
	if err != nil {
		t.Fatalf("Failed to create envelope: %v", err)
	}

	// Test setting properties
	env.SetProperty("retry_count", 3)
	env.SetProperty("timeout", 30.5)
	env.SetProperty("enabled", true)

	// Test getting properties
	retryCount, exists := env.GetProperty("retry_count")
	if !exists || retryCount != 3 {
		t.Error("Property 'retry_count' not set correctly")
	}

	timeout, exists := env.GetProperty("timeout")
	if !exists || timeout != 30.5 {
		t.Error("Property 'timeout' not set correctly")
	}

	enabled, exists := env.GetProperty("enabled")
	if !exists || enabled != true {
		t.Error("Property 'enabled' not set correctly")
	}

	// Test non-existent property
	_, exists = env.GetProperty("non-existent")
	if exists {
		t.Error("Non-existent property should not exist")
	}
}

// TestEnvelopeValidation tests envelope validation
func TestEnvelopeValidation(t *testing.T) {
	// Test valid envelope
	validEnv, err := envelope.NewEnvelope("test-agent", "test-topic", "valid", "data")
	if err != nil {
		t.Fatalf("Failed to create envelope: %v", err)
	}

	if err := validEnv.Validate(); err != nil {
		t.Errorf("Valid envelope failed validation: %v", err)
	}

	// Test invalid envelopes
	invalidEnv := &envelope.Envelope{}

	err = invalidEnv.Validate()
	if err == nil {
		t.Error("Expected validation error for empty envelope")
	}
}

// TestEnvelopeHops tests hop tracking
func TestEnvelopeHops(t *testing.T) {
	env, err := envelope.NewEnvelope("test-agent", "test-topic", "hop-test", "data")
	if err != nil {
		t.Fatalf("Failed to create envelope: %v", err)
	}

	// Initial state
	if env.HopCount != 0 {
		t.Errorf("Expected initial hop count 0, got %d", env.HopCount)
	}

	if len(env.Route) != 0 {
		t.Errorf("Expected empty route, got %v", env.Route)
	}

	// Add hops
	env.AddHop("agent-1")
	env.AddHop("agent-2")
	env.AddHop("agent-3")

	if env.HopCount != 3 {
		t.Errorf("Expected hop count 3, got %d", env.HopCount)
	}

	expectedRoute := []string{"agent-1", "agent-2", "agent-3"}
	if len(env.Route) != len(expectedRoute) {
		t.Errorf("Expected route length %d, got %d", len(expectedRoute), len(env.Route))
	}

	for i, expected := range expectedRoute {
		if env.Route[i] != expected {
			t.Errorf("Expected route[%d] = %s, got %s", i, expected, env.Route[i])
		}
	}
}

// TestEnvelopeClone tests envelope cloning
func TestEnvelopeClone(t *testing.T) {
	original, err := envelope.NewEnvelope("test-agent", "test-topic", "clone-test", "data")
	if err != nil {
		t.Fatalf("Failed to create envelope: %v", err)
	}

	// Add some metadata
	original.SetHeader("test-header", "value")
	original.SetProperty("test-property", 42)
	original.AddHop("agent-1")

	// Clone the envelope
	clone := original.Clone()

	// Verify clone is identical but separate
	if clone.ID != original.ID {
		t.Error("Clone ID doesn't match original")
	}

	if clone.Source != original.Source {
		t.Error("Clone source doesn't match original")
	}

	// Verify headers are cloned
	headerValue, exists := clone.GetHeader("test-header")
	if !exists || headerValue != "value" {
		t.Error("Clone headers not properly copied")
	}

	// Verify properties are cloned
	propValue, exists := clone.GetProperty("test-property")
	if !exists || propValue != 42 {
		t.Error("Clone properties not properly copied")
	}

	// Verify route is cloned
	if len(clone.Route) != 1 || clone.Route[0] != "agent-1" {
		t.Error("Clone route not properly copied")
	}

	// Verify it's a deep copy by modifying original
	original.SetHeader("test-header", "modified")
	cloneHeaderValue, _ := clone.GetHeader("test-header")
	if cloneHeaderValue != "value" {
		t.Error("Clone was not deep copied - header was modified")
	}
}

// TestEnvelopeReply tests reply envelope creation
func TestEnvelopeReply(t *testing.T) {
	original, err := envelope.NewEnvelope("sender-agent", "receiver-agent", "request", "request data")
	if err != nil {
		t.Fatalf("Failed to create original envelope: %v", err)
	}

	original.TraceID = "test-trace-123"

	reply, err := envelope.NewReplyEnvelope(original, "receiver-agent", "response data")
	if err != nil {
		t.Fatalf("Failed to create reply envelope: %v", err)
	}

	// Verify reply properties
	if reply.Source != "receiver-agent" {
		t.Errorf("Expected reply source 'receiver-agent', got %s", reply.Source)
	}

	if reply.Destination != "sender-agent" {
		t.Errorf("Expected reply destination 'sender-agent', got %s", reply.Destination)
	}

	if reply.MessageType != "reply" {
		t.Errorf("Expected reply message type 'reply', got %s", reply.MessageType)
	}

	if reply.CorrelationID != original.ID {
		t.Errorf("Expected correlation ID %s, got %s", original.ID, reply.CorrelationID)
	}

	if reply.TraceID != original.TraceID {
		t.Errorf("Expected trace ID %s, got %s", original.TraceID, reply.TraceID)
	}
}

// BenchmarkEnvelopeCreation benchmarks envelope creation
func BenchmarkEnvelopeCreation(b *testing.B) {
	testData := map[string]interface{}{
		"message": "benchmark test",
		"value":   42,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		envelope.NewEnvelope("test-agent", "test-topic", "benchmark", testData)
	}
}

// BenchmarkEnvelopeSerialization benchmarks JSON serialization
func BenchmarkEnvelopeSerialization(b *testing.B) {
	testData := map[string]interface{}{
		"message": "benchmark test",
		"value":   42,
		"array":   []interface{}{1, 2, 3, 4, 5},
	}

	env, _ := envelope.NewEnvelope("test-agent", "test-topic", "benchmark", testData)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		env.ToJSON()
	}
}

// BenchmarkEnvelopeDeserialization benchmarks JSON deserialization
func BenchmarkEnvelopeDeserialization(b *testing.B) {
	testData := map[string]interface{}{
		"message": "benchmark test",
		"value":   42,
		"array":   []interface{}{1, 2, 3, 4, 5},
	}

	env, _ := envelope.NewEnvelope("test-agent", "test-topic", "benchmark", testData)
	jsonData, _ := env.ToJSON()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		envelope.FromJSON(jsonData)
	}
}
