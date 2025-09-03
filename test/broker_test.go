package main

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/tenzoki/gox/internal/broker"
	"github.com/tenzoki/gox/internal/client"
)

// TestBrokerServiceConfiguration tests broker service configuration
func TestBrokerServiceConfiguration(t *testing.T) {
	config := broker.BrokerConfig{
		Port:     ":0", // Use random port
		Protocol: "tcp",
		Codec:    "json",
		Debug:    true,
	}

	service := broker.NewService(config)
	if service == nil {
		t.Fatal("Expected broker service to be created, got nil")
	}

	// Test config validation
	invalidConfigs := []broker.BrokerConfig{
		{Port: "", Protocol: "tcp", Codec: "json"},   // Missing port
		{Port: ":9001", Protocol: "", Codec: "json"}, // Missing protocol
		{Port: ":9001", Protocol: "tcp", Codec: ""},  // Missing codec
	}

	for i, cfg := range invalidConfigs {
		service := broker.NewService(cfg)
		if service == nil {
			t.Logf("Config %d correctly rejected invalid configuration", i)
		}
	}
}

// TestBrokerServiceLifecycle tests broker service start/stop
func TestBrokerServiceLifecycle(t *testing.T) {
	// Find available port
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	config := broker.BrokerConfig{
		Port:     fmt.Sprintf(":%d", port),
		Protocol: "tcp",
		Codec:    "json",
		Debug:    true,
	}

	service := broker.NewService(config)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Test start
	errChan := make(chan error, 1)
	go func() {
		errChan <- service.Start(ctx)
	}()

	// Give service time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context to stop service
	cancel()

	// Wait for service to stop
	select {
	case err := <-errChan:
		if err != nil && err != context.Canceled {
			t.Errorf("Service start failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("Service did not stop within timeout")
	}
}

// TestBrokerPubSub tests publish/subscribe functionality
func TestBrokerPubSub(t *testing.T) {
	// Start broker service
	config := broker.BrokerConfig{
		Port:     ":19010",
		Protocol: "tcp",
		Codec:    "json",
		Debug:    true,
	}

	service := broker.NewService(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		service.Start(ctx)
	}()

	// Give service time to start
	time.Sleep(100 * time.Millisecond)

	// Create publisher and subscriber clients
	publisher := client.NewBrokerClient("localhost:19010", "publisher", true)
	subscriber := client.NewBrokerClient("localhost:19010", "subscriber", true)

	if err := publisher.Connect(); err != nil {
		t.Fatalf("Publisher failed to connect: %v", err)
	}
	defer publisher.Disconnect()

	if err := subscriber.Connect(); err != nil {
		t.Fatalf("Subscriber failed to connect: %v", err)
	}
	defer subscriber.Disconnect()

	// Test pub/sub
	topic := "test-topic-pubsub"
	testMessage := client.BrokerMessage{
		Type:    "test",
		Payload: "Hello PubSub!",
	}

	// Subscribe first
	msgChan, err := subscriber.Subscribe(topic)
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Publish message
	if err := publisher.Publish(topic, testMessage); err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	// Wait for message
	select {
	case receivedMsg := <-msgChan:
		if receivedMsg.Type != testMessage.Type {
			t.Errorf("Expected type %s, got %s", testMessage.Type, receivedMsg.Type)
		}
		if receivedMsg.Payload != testMessage.Payload {
			t.Errorf("Expected payload %s, got %s", testMessage.Payload, receivedMsg.Payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for published message")
	}
}

// TestBrokerPipeMessaging tests pipe messaging functionality
func TestBrokerPipeMessaging(t *testing.T) {
	// Start broker service
	config := broker.BrokerConfig{
		Port:     ":19011",
		Protocol: "tcp",
		Codec:    "json",
		Debug:    true,
	}

	service := broker.NewService(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		service.Start(ctx)
	}()

	// Give service time to start
	time.Sleep(100 * time.Millisecond)

	// Create producer and consumer clients
	producer := client.NewBrokerClient("localhost:19011", "producer", true)
	consumer := client.NewBrokerClient("localhost:19011", "consumer", true)

	if err := producer.Connect(); err != nil {
		t.Fatalf("Producer failed to connect: %v", err)
	}
	defer producer.Disconnect()

	if err := consumer.Connect(); err != nil {
		t.Fatalf("Consumer failed to connect: %v", err)
	}
	defer consumer.Disconnect()

	// Test pipe messaging
	pipeName := "test-pipe-messaging"
	testMessage := client.BrokerMessage{
		Type:    "pipe-test",
		Payload: "Hello Pipe!",
	}

	// Connect to pipe
	if err := producer.ConnectPipe(pipeName, "producer"); err != nil {
		t.Fatalf("Failed to connect producer to pipe: %v", err)
	}

	if err := consumer.ConnectPipe(pipeName, "consumer"); err != nil {
		t.Fatalf("Failed to connect consumer to pipe: %v", err)
	}

	// Send message through pipe
	if err := producer.SendPipe(pipeName, testMessage); err != nil {
		t.Fatalf("Failed to send pipe message: %v", err)
	}

	// Receive message from pipe
	receivedMsg, err := consumer.ReceivePipe(pipeName, 2000) // 2 second timeout
	if err != nil {
		t.Fatalf("Failed to receive pipe message: %v", err)
	}

	if receivedMsg == nil {
		t.Fatal("Expected message, got nil")
	}

	// Verify message content
	msg, ok := receivedMsg.(*client.BrokerMessage)
	if !ok {
		t.Fatalf("Expected *client.BrokerMessage, got %T", receivedMsg)
	}

	if msg.Type != testMessage.Type {
		t.Errorf("Expected type %s, got %s", testMessage.Type, msg.Type)
	}

	if msg.Payload != testMessage.Payload {
		t.Errorf("Expected payload %s, got %s", testMessage.Payload, msg.Payload)
	}
}

// TestBrokerMultipleSubscribers tests multiple subscribers to same topic
func TestBrokerMultipleSubscribers(t *testing.T) {
	// Start broker service
	config := broker.BrokerConfig{
		Port:     ":19012",
		Protocol: "tcp",
		Codec:    "json",
		Debug:    true,
	}

	service := broker.NewService(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		service.Start(ctx)
	}()

	// Give service time to start
	time.Sleep(100 * time.Millisecond)

	// Create publisher and multiple subscriber clients
	publisher := client.NewBrokerClient("localhost:19012", "publisher", true)
	subscriber1 := client.NewBrokerClient("localhost:19012", "subscriber1", true)
	subscriber2 := client.NewBrokerClient("localhost:19012", "subscriber2", true)

	clients := []*client.BrokerClient{publisher, subscriber1, subscriber2}
	for _, c := range clients {
		if err := c.Connect(); err != nil {
			t.Fatalf("Client failed to connect: %v", err)
		}
		defer c.Disconnect()
	}

	topic := "multi-subscriber-test"
	testMessage := client.BrokerMessage{
		Type:    "broadcast",
		Payload: "Hello All!",
	}

	// Subscribe both subscribers
	msgChan1, err := subscriber1.Subscribe(topic)
	if err != nil {
		t.Fatalf("Subscriber1 failed to subscribe: %v", err)
	}

	msgChan2, err := subscriber2.Subscribe(topic)
	if err != nil {
		t.Fatalf("Subscriber2 failed to subscribe: %v", err)
	}

	// Publish message
	if err := publisher.Publish(topic, testMessage); err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	// Both subscribers should receive the message
	received := 0
	timeout := time.After(3 * time.Second)

	for received < 2 {
		select {
		case msg1 := <-msgChan1:
			if msg1.Payload == testMessage.Payload {
				received++
				t.Log("Subscriber1 received message")
			}
		case msg2 := <-msgChan2:
			if msg2.Payload == testMessage.Payload {
				received++
				t.Log("Subscriber2 received message")
			}
		case <-timeout:
			t.Fatalf("Timeout: only %d of 2 subscribers received message", received)
		}
	}

	if received != 2 {
		t.Errorf("Expected 2 subscribers to receive message, got %d", received)
	}
}

// TestBrokerConnectionCleanup tests connection cleanup on disconnect
func TestBrokerConnectionCleanup(t *testing.T) {
	// Start broker service
	config := broker.BrokerConfig{
		Port:     ":19013",
		Protocol: "tcp",
		Codec:    "json",
		Debug:    true,
	}

	service := broker.NewService(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		service.Start(ctx)
	}()

	// Give service time to start
	time.Sleep(100 * time.Millisecond)

	// Create and connect client
	testClient := client.NewBrokerClient("localhost:19013", "cleanup-test", true)

	if err := testClient.Connect(); err != nil {
		t.Fatalf("Client failed to connect: %v", err)
	}

	// Subscribe to topic
	topic := "cleanup-test-topic"
	_, err := testClient.Subscribe(topic)
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Disconnect client
	testClient.Disconnect()

	// Give broker time to clean up
	time.Sleep(100 * time.Millisecond)

	// Reconnect and test that previous subscriptions are cleaned up
	client2 := client.NewBrokerClient("localhost:19013", "cleanup-test-2", true)
	if err := client2.Connect(); err != nil {
		t.Fatalf("Client2 failed to connect: %v", err)
	}
	defer client2.Disconnect()

	// This should work without issues from the previous connection
	_, err = client2.Subscribe(topic)
	if err != nil {
		t.Fatalf("Failed to subscribe after cleanup: %v", err)
	}

	t.Log("Connection cleanup test passed")
}

// BenchmarkBrokerPublish benchmarks message publishing
func BenchmarkBrokerPublish(b *testing.B) {
	// Start broker service
	config := broker.BrokerConfig{
		Port:     ":19014",
		Protocol: "tcp",
		Codec:    "json",
		Debug:    false, // Disable debug for benchmarking
	}

	service := broker.NewService(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		service.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Create publisher
	publisher := client.NewBrokerClient("localhost:19014", "bench-publisher", false)
	if err := publisher.Connect(); err != nil {
		b.Fatalf("Publisher failed to connect: %v", err)
	}
	defer publisher.Disconnect()

	topic := "bench-topic"
	message := client.BrokerMessage{
		Type:    "benchmark",
		Payload: "benchmark message payload",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		publisher.Publish(topic, message)
	}
}

// BenchmarkBrokerPipeMessage benchmarks pipe messaging
func BenchmarkBrokerPipeMessage(b *testing.B) {
	// Start broker service
	config := broker.BrokerConfig{
		Port:     ":19015",
		Protocol: "tcp",
		Codec:    "json",
		Debug:    false,
	}

	service := broker.NewService(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		service.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Create producer and consumer
	producer := client.NewBrokerClient("localhost:19015", "bench-producer", false)
	consumer := client.NewBrokerClient("localhost:19015", "bench-consumer", false)

	if err := producer.Connect(); err != nil {
		b.Fatalf("Producer failed to connect: %v", err)
	}
	defer producer.Disconnect()

	if err := consumer.Connect(); err != nil {
		b.Fatalf("Consumer failed to connect: %v", err)
	}
	defer consumer.Disconnect()

	pipeName := "bench-pipe"
	producer.ConnectPipe(pipeName, "producer")
	consumer.ConnectPipe(pipeName, "consumer")

	message := client.BrokerMessage{
		Type:    "benchmark",
		Payload: "benchmark pipe message",
	}

	// Start consumer in background
	go func() {
		for {
			consumer.ReceivePipe(pipeName, 100)
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		producer.SendPipe(pipeName, message)
	}
}
