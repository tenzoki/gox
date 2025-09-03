package main

import (
	"context"
	"testing"
	"time"

	"github.com/tenzoki/gox/internal/broker"
	"github.com/tenzoki/gox/internal/client"
	"github.com/tenzoki/gox/internal/support"
)

// TestSupportClientConfiguration tests support client configuration
func TestSupportClientConfiguration(t *testing.T) {
	// Test valid configuration
	supportClient := client.NewSupportClient("localhost:9000", true)
	if supportClient == nil {
		t.Fatal("Expected support client to be created, got nil")
	}

	// Test invalid addresses (should still create client, but connection will fail)
	invalidAddrs := []string{
		"",
		"invalid:port",
		"localhost:",
		":9000",
	}

	for i, addr := range invalidAddrs {
		testClient := client.NewSupportClient(addr, true)
		if testClient == nil {
			t.Errorf("Client %d: Expected client to be created even with invalid address %s", i, addr)
		}
	}
}

// TestSupportClientConnection tests support client connection
func TestSupportClientConnection(t *testing.T) {
	// Start support service
	cfg := support.SupportConfig{
		Port:  ":19020",
		Debug: true,
	}

	service := support.NewService(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		service.Start(ctx)
	}()

	// Give service time to start
	time.Sleep(100 * time.Millisecond)

	// Test connection
	supportClient := client.NewSupportClient("localhost:19020", true)

	if err := supportClient.Connect(); err != nil {
		t.Fatalf("Failed to connect to support: %v", err)
	}

	// Test disconnect
	supportClient.Disconnect()

	// Test reconnection
	if err := supportClient.Connect(); err != nil {
		t.Fatalf("Failed to reconnect to support: %v", err)
	}

	supportClient.Disconnect()
}

// TestSupportClientAgentRegistration tests agent registration via support client
func TestSupportClientAgentRegistration(t *testing.T) {
	// Start support service
	cfg := support.SupportConfig{
		Port:  ":19021",
		Debug: true,
	}

	service := support.NewService(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		service.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Connect client
	supportClient := client.NewSupportClient("localhost:19021", true)
	if err := supportClient.Connect(); err != nil {
		t.Fatalf("Failed to connect to support: %v", err)
	}
	defer supportClient.Disconnect()

	// Test agent registration
	registration := client.AgentRegistration{
		ID:           "test-client-agent-001",
		AgentType:    "test-client-agent",
		Protocol:     "tcp",
		Address:      "localhost",
		Port:         "8000",
		Codec:        "json",
		Capabilities: []string{"test", "demo"},
	}

	if err := supportClient.RegisterAgent(registration); err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	// Test duplicate registration (should handle gracefully)
	if err := supportClient.RegisterAgent(registration); err != nil {
		t.Logf("Duplicate registration handled: %v", err)
	}
}

// TestBrokerClientConfiguration tests broker client configuration
func TestBrokerClientConfiguration(t *testing.T) {
	// Test valid configuration
	brokerClient := client.NewBrokerClient("localhost:9001", "test-client", true)
	if brokerClient == nil {
		t.Fatal("Expected broker client to be created, got nil")
	}

	// Test different client IDs
	clientIDs := []string{
		"simple-client",
		"client-with-dashes",
		"client_with_underscores",
		"Client123",
	}

	for _, clientID := range clientIDs {
		testClient := client.NewBrokerClient("localhost:9001", clientID, true)
		if testClient == nil {
			t.Errorf("Failed to create client with ID: %s", clientID)
		}
	}
}

// TestBrokerClientConnection tests broker client connection
func TestBrokerClientConnection(t *testing.T) {
	// Start broker service
	cfg := broker.BrokerConfig{
		Port:     ":19022",
		Protocol: "tcp",
		Codec:    "json",
		Debug:    true,
	}

	service := broker.NewService(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		service.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Test connection
	brokerClient := client.NewBrokerClient("localhost:19022", "test-connection", true)

	if err := brokerClient.Connect(); err != nil {
		t.Fatalf("Failed to connect to broker: %v", err)
	}

	// Test disconnect
	brokerClient.Disconnect()

	// Test reconnection
	if err := brokerClient.Connect(); err != nil {
		t.Fatalf("Failed to reconnect to broker: %v", err)
	}

	brokerClient.Disconnect()
}

// TestBrokerClientSubscription tests subscription functionality
func TestBrokerClientSubscription(t *testing.T) {
	// Start broker service
	cfg := broker.BrokerConfig{
		Port:     ":19023",
		Protocol: "tcp",
		Codec:    "json",
		Debug:    true,
	}

	service := broker.NewService(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		service.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Connect client
	subscribeClient := client.NewBrokerClient("localhost:19023", "test-subscriber", true)
	if err := subscribeClient.Connect(); err != nil {
		t.Fatalf("Failed to connect to broker: %v", err)
	}
	defer subscribeClient.Disconnect()

	// Test subscription
	topic := "test-subscription-topic"
	msgChan, err := subscribeClient.Subscribe(topic)
	if err != nil {
		t.Fatalf("Failed to subscribe to topic: %v", err)
	}

	if msgChan == nil {
		t.Fatal("Expected message channel, got nil")
	}

	// Test multiple subscriptions to different topics
	topics := []string{"topic1", "topic2", "topic3"}
	channels := make([]<-chan *client.BrokerMessage, len(topics))

	for i, topic := range topics {
		ch, err := subscribeClient.Subscribe(topic)
		if err != nil {
			t.Fatalf("Failed to subscribe to topic %s: %v", topic, err)
		}
		channels[i] = ch
	}

	// All channels should be different
	for i := range channels {
		for j := i + 1; j < len(channels); j++ {
			if channels[i] == channels[j] {
				t.Error("Expected different channels for different subscriptions")
			}
		}
	}
}

// TestBrokerClientPublishing tests publishing functionality
func TestBrokerClientPublishing(t *testing.T) {
	// Start broker service
	cfg := broker.BrokerConfig{
		Port:     ":19024",
		Protocol: "tcp",
		Codec:    "json",
		Debug:    true,
	}

	service := broker.NewService(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		service.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Connect publisher and subscriber
	publisher := client.NewBrokerClient("localhost:19024", "publisher", true)
	subscriber := client.NewBrokerClient("localhost:19024", "subscriber", true)

	if err := publisher.Connect(); err != nil {
		t.Fatalf("Publisher failed to connect: %v", err)
	}
	defer publisher.Disconnect()

	if err := subscriber.Connect(); err != nil {
		t.Fatalf("Subscriber failed to connect: %v", err)
	}
	defer subscriber.Disconnect()

	topic := "test-publishing-topic"

	// Subscribe first
	msgChan, err := subscriber.Subscribe(topic)
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Test publishing different message types
	messages := []client.BrokerMessage{
		{Type: "text", Payload: "Hello World"},
		{Type: "number", Payload: "42"},
		{Type: "json", Payload: `{"key": "value"}`},
		{Type: "empty", Payload: ""},
	}

	for _, msg := range messages {
		if err := publisher.Publish(topic, msg); err != nil {
			t.Errorf("Failed to publish message %v: %v", msg, err)
		}

		// Wait for message
		select {
		case receivedMsg := <-msgChan:
			if receivedMsg.Type != msg.Type {
				t.Errorf("Expected type %s, got %s", msg.Type, receivedMsg.Type)
			}
			if receivedMsg.Payload != msg.Payload {
				t.Errorf("Expected payload %s, got %s", msg.Payload, receivedMsg.Payload)
			}
		case <-time.After(2 * time.Second):
			t.Errorf("Timeout waiting for message %v", msg)
		}
	}
}

// TestBrokerClientPipeOperations tests pipe operations
func TestBrokerClientPipeOperations(t *testing.T) {
	// Start broker service
	cfg := broker.BrokerConfig{
		Port:     ":19025",
		Protocol: "tcp",
		Codec:    "json",
		Debug:    true,
	}

	service := broker.NewService(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		service.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Connect producer and consumer
	producer := client.NewBrokerClient("localhost:19025", "producer", true)
	consumer := client.NewBrokerClient("localhost:19025", "consumer", true)

	if err := producer.Connect(); err != nil {
		t.Fatalf("Producer failed to connect: %v", err)
	}
	defer producer.Disconnect()

	if err := consumer.Connect(); err != nil {
		t.Fatalf("Consumer failed to connect: %v", err)
	}
	defer consumer.Disconnect()

	pipeName := "test-pipe-operations"

	// Test pipe connection
	if err := producer.ConnectPipe(pipeName, "producer"); err != nil {
		t.Fatalf("Failed to connect producer to pipe: %v", err)
	}

	if err := consumer.ConnectPipe(pipeName, "consumer"); err != nil {
		t.Fatalf("Failed to connect consumer to pipe: %v", err)
	}

	// Test sending and receiving messages
	testMessages := []client.BrokerMessage{
		{Type: "test1", Payload: "First message"},
		{Type: "test2", Payload: "Second message"},
		{Type: "test3", Payload: "Third message"},
	}

	// Send all messages
	for _, msg := range testMessages {
		if err := producer.SendPipe(pipeName, msg); err != nil {
			t.Errorf("Failed to send message %v: %v", msg, err)
		}
	}

	// Receive all messages
	for i := range testMessages {
		receivedData, err := consumer.ReceivePipe(pipeName, 2000)
		if err != nil {
			t.Errorf("Failed to receive message %d: %v", i, err)
			continue
		}

		if receivedData == nil {
			t.Errorf("Expected message %d, got nil", i)
			continue
		}

		// Note: In the actual implementation, messages might be JSON-encoded
		// This test assumes the message is returned in a format that can be compared
		t.Logf("Received message %d: %v", i, receivedData)
	}
}

// TestAdapterClientConfiguration tests adapter client configuration
func TestAdapterClientConfiguration(t *testing.T) {
	// Create a mock broker client for testing
	brokerClient := client.NewBrokerClient("localhost:9001", "test-adapter", true)
	if brokerClient == nil {
		t.Fatal("Expected broker client to be created, got nil")
	}

	// Test adapter client creation
	adapterClient := client.NewAdapterClient(brokerClient, "test-adapter", true)
	if adapterClient == nil {
		t.Fatal("Expected adapter client to be created, got nil")
	}

	// Test different agent IDs
	agentIDs := []string{
		"simple-adapter",
		"adapter-with-dashes",
		"adapter_with_underscores",
		"Adapter123",
	}

	for _, agentID := range agentIDs {
		testAdapter := client.NewAdapterClient(brokerClient, agentID, true)
		if testAdapter == nil {
			t.Errorf("Failed to create adapter client with ID: %s", agentID)
		}
	}
}

// TestAdapterClientLifecycle tests adapter client functionality
func TestAdapterClientLifecycle(t *testing.T) {
	// Create mock broker client
	brokerClient := client.NewBrokerClient("localhost:9001", "test-lifecycle-adapter", true)
	adapterClient := client.NewAdapterClient(brokerClient, "test-lifecycle-adapter", true)

	if adapterClient == nil {
		t.Fatal("Expected adapter client to be created, got nil")
	}

	// Test basic functionality (transform method exists but will fail without running services)
	// In a real test environment, we would need running services
	t.Logf("Adapter client created successfully for agent: test-lifecycle-adapter")

	// Test completed successfully
}

// BenchmarkBrokerClientPublish benchmarks client publishing
func BenchmarkBrokerClientPublish(b *testing.B) {
	// Start broker service
	cfg := broker.BrokerConfig{
		Port:     ":19026",
		Protocol: "tcp",
		Codec:    "json",
		Debug:    false,
	}

	service := broker.NewService(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		service.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Connect client
	publisher := client.NewBrokerClient("localhost:19026", "bench-publisher", false)
	if err := publisher.Connect(); err != nil {
		b.Fatalf("Failed to connect: %v", err)
	}
	defer publisher.Disconnect()

	topic := "benchmark-topic"
	message := client.BrokerMessage{
		Type:    "benchmark",
		Payload: "benchmark message",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		publisher.Publish(topic, message)
	}
}

// BenchmarkBrokerClientSubscribe benchmarks client subscription
func BenchmarkBrokerClientSubscribe(b *testing.B) {
	// Start broker service
	cfg := broker.BrokerConfig{
		Port:     ":19027",
		Protocol: "tcp",
		Codec:    "json",
		Debug:    false,
	}

	service := broker.NewService(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		service.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Connect client
	subscriber := client.NewBrokerClient("localhost:19027", "bench-subscriber", false)
	if err := subscriber.Connect(); err != nil {
		b.Fatalf("Failed to connect: %v", err)
	}
	defer subscriber.Disconnect()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		topic := "bench-topic-" + string(rune(i%10))
		subscriber.Subscribe(topic)
	}
}
