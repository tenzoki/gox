package main

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/tenzoki/gox/internal/agent"
	"github.com/tenzoki/gox/internal/broker"
	"github.com/tenzoki/gox/internal/client"
	"github.com/tenzoki/gox/internal/config"
	"github.com/tenzoki/gox/internal/envelope"
	"github.com/tenzoki/gox/internal/support"
)

// TestSupportServiceIntegration tests the support service functionality
func TestSupportServiceIntegration(t *testing.T) {
	// Create support service
	cfg := support.SupportConfig{
		Port:  ":19000", // Use different port for testing
		Debug: true,
	}

	service := support.NewService(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start service in background
	go func() {
		if err := service.Start(ctx); err != nil {
			t.Logf("Support service error: %v", err)
		}
	}()

	// Give service time to start
	time.Sleep(100 * time.Millisecond)

	// Test agent registration
	supportClient := client.NewSupportClient("localhost:19000", true)
	if err := supportClient.Connect(); err != nil {
		t.Fatalf("Failed to connect to support: %v", err)
	}
	defer supportClient.Disconnect()

	registration := client.AgentRegistration{
		ID:           "test-agent-001",
		AgentType:    "test-agent",
		Protocol:     "tcp",
		Address:      "localhost",
		Port:         "0",
		Codec:        "json",
		Capabilities: []string{"test"},
	}

	if err := supportClient.RegisterAgent(registration); err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	t.Logf("Successfully registered test agent")
}

// TestBrokerServiceIntegration tests the broker service functionality
func TestBrokerServiceIntegration(t *testing.T) {
	// Create broker service
	cfg := broker.BrokerConfig{
		Port:     ":19001",
		Protocol: "tcp",
		Codec:    "json",
		Debug:    true,
	}

	service := broker.NewService(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start service in background
	go func() {
		if err := service.Start(ctx); err != nil {
			t.Logf("Broker service error: %v", err)
		}
	}()

	// Give service time to start
	time.Sleep(100 * time.Millisecond)

	// Create separate publisher and subscriber clients
	publisher := client.NewBrokerClient("localhost:19001", "test-publisher", true)
	if err := publisher.Connect(); err != nil {
		t.Fatalf("Failed to connect publisher: %v", err)
	}
	defer publisher.Disconnect()

	subscriber := client.NewBrokerClient("localhost:19001", "test-subscriber", true)
	if err := subscriber.Connect(); err != nil {
		t.Fatalf("Failed to connect subscriber: %v", err)
	}
	defer subscriber.Disconnect()

	// Test publish/subscribe
	topic := "test-topic"
	testMessage := "Hello, Test!"

	// Subscribe first
	msgChan, err := subscriber.Subscribe(topic)
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Publish message
	msg := client.BrokerMessage{
		Type:    "test",
		Payload: testMessage,
	}

	if err := publisher.Publish(topic, msg); err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	// Wait for message
	select {
	case receivedMsg := <-msgChan:
		if receivedMsg.Payload != testMessage {
			t.Fatalf("Expected %s, got %s", testMessage, receivedMsg.Payload)
		}
		t.Logf("Successfully received message: %s", receivedMsg.Payload)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for message")
	}
}

// TestAgentLifecycle tests the agent lifecycle management
func TestAgentLifecycle(t *testing.T) {
	// Test lifecycle manager
	notifyCallback := func(agentID, state string) error {
		t.Logf("Agent %s state changed to: %s", agentID, state)
		return nil
	}

	lifecycle := agent.NewLifecycleManager("test-agent", notifyCallback)

	// Test initial state
	if lifecycle.GetState() != agent.StateInstalled {
		t.Errorf("Expected initial state %s, got %s", agent.StateInstalled, lifecycle.GetState())
	}

	// Test state transitions
	if err := lifecycle.SetState(agent.StateConfigured, "test transition"); err != nil {
		t.Fatalf("Failed to transition to configured: %v", err)
	}

	if err := lifecycle.SetState(agent.StateReady, "test transition"); err != nil {
		t.Fatalf("Failed to transition to ready: %v", err)
	}

	if err := lifecycle.SetState(agent.StateRunning, "test transition"); err != nil {
		t.Fatalf("Failed to transition to running: %v", err)
	}

	// Test invalid transition
	if err := lifecycle.SetState(agent.StateInstalled, "invalid"); err == nil {
		t.Error("Expected error for invalid transition, got none")
	}

	t.Logf("Agent lifecycle tests passed")
}

// TestEnvelopeProcessing tests envelope creation and processing
func TestEnvelopeProcessing(t *testing.T) {
	// Create test data
	testData := map[string]interface{}{
		"message": "test message",
		"value":   42,
	}

	// Create envelope
	env, err := envelope.NewEnvelope("test-agent", "test-topic", "test-type", testData)
	if err != nil {
		t.Fatalf("Failed to create envelope: %v", err)
	}
	if env.MessageType != "test-type" {
		t.Errorf("Expected message type 'test-type', got %s", env.MessageType)
	}

	if env.ID == "" {
		t.Error("Expected non-empty ID")
	}

	if env.Timestamp.IsZero() {
		t.Error("Expected non-zero timestamp")
	}

	// Test JSON serialization
	jsonData, err := env.ToJSON()
	if err != nil {
		t.Fatalf("Failed to marshal envelope: %v", err)
	}

	// Test deserialization
	env2, err := envelope.FromJSON(jsonData)
	if err != nil {
		t.Fatalf("Failed to unmarshal envelope: %v", err)
	}

	if env2.ID != env.ID {
		t.Errorf("Expected ID %s, got %s", env.ID, env2.ID)
	}

	t.Logf("Envelope processing tests passed")
}

// TestConfigLoading tests configuration loading
func TestConfigLoading(t *testing.T) {
	// Create temporary config file
	tempFile, err := os.CreateTemp("", "gox-test-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	testConfig := `
app_name: "test-app"
debug: true

support:
  port: ":9000"
  debug: true

broker:
  port: ":9001"
  protocol: "tcp"
  codec: "json"
`

	if _, err := tempFile.WriteString(testConfig); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	tempFile.Close()

	// Load configuration
	cfg, err := config.Load(tempFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if cfg.AppName != "test-app" {
		t.Errorf("Expected app name 'test-app', got %s", cfg.AppName)
	}

	if !cfg.Debug {
		t.Error("Expected debug to be true")
	}

	if cfg.Support.Port != ":9000" {
		t.Errorf("Expected support port ':9000', got %s", cfg.Support.Port)
	}

	t.Logf("Configuration loading tests passed")
}

// TestPipeHandling tests pipe message handling
func TestPipeHandling(t *testing.T) {
	// Create broker service for pipe testing
	cfg := broker.BrokerConfig{
		Port:     ":19002",
		Protocol: "tcp",
		Codec:    "json",
		Debug:    true,
	}

	service := broker.NewService(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start service
	go func() {
		if err := service.Start(ctx); err != nil {
			t.Logf("Broker service error: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	// Create two clients: producer and consumer
	producer := client.NewBrokerClient("localhost:19002", "producer", true)
	if err := producer.Connect(); err != nil {
		t.Fatalf("Failed to connect producer: %v", err)
	}
	defer producer.Disconnect()

	consumer := client.NewBrokerClient("localhost:19002", "consumer", true)
	if err := consumer.Connect(); err != nil {
		t.Fatalf("Failed to connect consumer: %v", err)
	}
	defer consumer.Disconnect()

	pipeName := "test-pipe"

	// Connect to pipe
	if err := producer.ConnectPipe(pipeName, "producer"); err != nil {
		t.Fatalf("Failed to connect producer to pipe: %v", err)
	}

	if err := consumer.ConnectPipe(pipeName, "consumer"); err != nil {
		t.Fatalf("Failed to connect consumer to pipe: %v", err)
	}

	// Send message through pipe
	testMsg := client.BrokerMessage{
		Type:    "test-pipe",
		Payload: "pipe message",
	}

	if err := producer.SendPipe(pipeName, testMsg); err != nil {
		t.Fatalf("Failed to send pipe message: %v", err)
	}

	// Receive message from pipe
	result, err := consumer.ReceivePipe(pipeName, 2000) // 2 second timeout
	if err != nil {
		t.Fatalf("Failed to receive pipe message: %v", err)
	}

	if result == nil {
		t.Fatal("Expected message, got nil")
	}

	t.Logf("Pipe handling tests passed")
}

// Helper function to check if port is available
func isPortAvailable(port string) bool {
	ln, err := net.Listen("tcp", port)
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

// TestPortAvailability ensures test ports are available
func TestPortAvailability(t *testing.T) {
	testPorts := []string{":19000", ":19001", ":19002"}

	for _, port := range testPorts {
		if !isPortAvailable(port) {
			t.Fatalf("Test port %s is not available", port)
		}
	}

	t.Logf("All test ports are available")
}
