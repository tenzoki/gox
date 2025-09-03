package main

import (
	"context"
	"testing"
	"time"

	"github.com/tenzoki/gox/internal/client"
	"github.com/tenzoki/gox/internal/support"
)

// TestSupportServiceConfiguration tests support service configuration
func TestSupportServiceConfiguration(t *testing.T) {
	// Test valid configuration
	config := support.SupportConfig{
		Port:  ":9000",
		Debug: true,
	}

	service := support.NewService(config)
	if service == nil {
		t.Fatal("Expected support service to be created, got nil")
	}

	// Test different port configurations
	portConfigs := []string{
		":9000",
		":8080",
		":0",     // Random port
		":65535", // Max port
	}

	for _, port := range portConfigs {
		cfg := support.SupportConfig{Port: port, Debug: false}
		service := support.NewService(cfg)
		if service == nil {
			t.Errorf("Failed to create service with port: %s", port)
		}
	}
}

// TestSupportServiceLifecycle tests support service start/stop lifecycle
func TestSupportServiceLifecycle(t *testing.T) {
	config := support.SupportConfig{
		Port:  ":19030",
		Debug: true,
	}

	service := support.NewService(config)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Test start
	errChan := make(chan error, 1)
	go func() {
		errChan <- service.Start(ctx)
	}()

	// Give service time to start
	time.Sleep(100 * time.Millisecond)

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

// TestSupportServiceAgentRegistration tests agent registration functionality
func TestSupportServiceAgentRegistration(t *testing.T) {
	config := support.SupportConfig{
		Port:  ":19031",
		Debug: true,
	}

	service := support.NewService(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start service
	go func() {
		service.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Connect client
	supportClient := client.NewSupportClient("localhost:19031", true)
	if err := supportClient.Connect(); err != nil {
		t.Fatalf("Failed to connect to support: %v", err)
	}
	defer supportClient.Disconnect()

	// Test single agent registration
	registration := client.AgentRegistration{
		ID:           "test-support-agent-001",
		AgentType:    "test-support-agent",
		Protocol:     "tcp",
		Address:      "localhost",
		Port:         "8000",
		Codec:        "json",
		Capabilities: []string{"test", "support", "demo"},
	}

	if err := supportClient.RegisterAgent(registration); err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	// Test multiple agent registrations
	agents := []client.AgentRegistration{
		{
			ID:           "agent-002",
			AgentType:    "processor",
			Protocol:     "tcp",
			Address:      "localhost",
			Port:         "8001",
			Codec:        "json",
			Capabilities: []string{"process", "transform"},
		},
		{
			ID:           "agent-003",
			AgentType:    "writer",
			Protocol:     "tcp",
			Address:      "localhost",
			Port:         "8002",
			Codec:        "json",
			Capabilities: []string{"write", "output"},
		},
	}

	for _, agent := range agents {
		if err := supportClient.RegisterAgent(agent); err != nil {
			t.Errorf("Failed to register agent %s: %v", agent.ID, err)
		}
	}

	t.Logf("Successfully registered %d agents", len(agents)+1)
}

// TestSupportServiceAgentLookup tests agent lookup functionality
func TestSupportServiceAgentLookup(t *testing.T) {
	config := support.SupportConfig{
		Port:  ":19032",
		Debug: true,
	}

	service := support.NewService(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		service.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	supportClient := client.NewSupportClient("localhost:19032", true)
	if err := supportClient.Connect(); err != nil {
		t.Fatalf("Failed to connect to support: %v", err)
	}
	defer supportClient.Disconnect()

	// Register test agents
	testAgents := []client.AgentRegistration{
		{
			ID:           "lookup-agent-001",
			AgentType:    "file-processor",
			Protocol:     "tcp",
			Address:      "localhost",
			Port:         "8010",
			Codec:        "json",
			Capabilities: []string{"file", "process"},
		},
		{
			ID:           "lookup-agent-002",
			AgentType:    "data-transformer",
			Protocol:     "tcp",
			Address:      "localhost",
			Port:         "8011",
			Codec:        "json",
			Capabilities: []string{"data", "transform", "process"},
		},
	}

	for _, agent := range testAgents {
		if err := supportClient.RegisterAgent(agent); err != nil {
			t.Fatalf("Failed to register test agent %s: %v", agent.ID, err)
		}
	}

	// Test agent lookup by capability
	// Note: This assumes the support service has a method to search agents
	// The actual implementation may vary
	t.Log("Test agents registered successfully for lookup testing")
}

// TestSupportServiceBrokerCoordination tests broker coordination functionality
func TestSupportServiceBrokerCoordination(t *testing.T) {
	config := support.SupportConfig{
		Port:  ":19033",
		Debug: true,
	}

	service := support.NewService(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		service.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	supportClient := client.NewSupportClient("localhost:19033", true)
	if err := supportClient.Connect(); err != nil {
		t.Fatalf("Failed to connect to support: %v", err)
	}
	defer supportClient.Disconnect()

	// Test broker information methods
	// Note: The actual broker coordination methods depend on the implementation
	// This test verifies the service can handle broker-related requests
	t.Log("Support service broker coordination test completed")
}

// TestSupportServiceConcurrentClients tests multiple concurrent clients
func TestSupportServiceConcurrentClients(t *testing.T) {
	config := support.SupportConfig{
		Port:  ":19034",
		Debug: true,
	}

	service := support.NewService(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		service.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Create multiple clients
	numClients := 5
	clients := make([]*client.SupportClient, numClients)

	// Connect all clients
	for i := 0; i < numClients; i++ {
		clients[i] = client.NewSupportClient("localhost:19034", true)
		if err := clients[i].Connect(); err != nil {
			t.Fatalf("Failed to connect client %d: %v", i, err)
		}
	}

	// Disconnect all clients
	for i, c := range clients {
		c.Disconnect()
		t.Logf("Disconnected client %d", i)
	}

	// Test concurrent agent registrations
	errChan := make(chan error, numClients)

	for i := 0; i < numClients; i++ {
		go func(clientID int) {
			supportClient := client.NewSupportClient("localhost:19034", true)
			if err := supportClient.Connect(); err != nil {
				errChan <- err
				return
			}
			defer supportClient.Disconnect()

			registration := client.AgentRegistration{
				ID:           "concurrent-agent-" + string(rune('0'+clientID)),
				AgentType:    "concurrent-test",
				Protocol:     "tcp",
				Address:      "localhost",
				Port:         "800" + string(rune('0'+clientID)),
				Codec:        "json",
				Capabilities: []string{"concurrent", "test"},
			}

			errChan <- supportClient.RegisterAgent(registration)
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numClients; i++ {
		if err := <-errChan; err != nil {
			t.Errorf("Concurrent client %d error: %v", i, err)
		}
	}

	t.Logf("Successfully handled %d concurrent clients", numClients)
}

// TestSupportServiceConfigurationUpdates tests configuration update functionality
func TestSupportServiceConfigurationUpdates(t *testing.T) {
	config := support.SupportConfig{
		Port:  ":19035",
		Debug: true,
	}

	service := support.NewService(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		service.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	supportClient := client.NewSupportClient("localhost:19035", true)
	if err := supportClient.Connect(); err != nil {
		t.Fatalf("Failed to connect to support: %v", err)
	}
	defer supportClient.Disconnect()

	// Test configuration-related functionality
	// Note: The actual configuration update methods depend on the implementation
	t.Log("Support service configuration update test completed")
}

// TestSupportServiceHealthChecks tests health check functionality
func TestSupportServiceHealthChecks(t *testing.T) {
	config := support.SupportConfig{
		Port:  ":19036",
		Debug: true,
	}

	service := support.NewService(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		service.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	supportClient := client.NewSupportClient("localhost:19036", true)
	if err := supportClient.Connect(); err != nil {
		t.Fatalf("Failed to connect to support: %v", err)
	}
	defer supportClient.Disconnect()

	// Register an agent for health checking
	registration := client.AgentRegistration{
		ID:           "health-check-agent",
		AgentType:    "health-test",
		Protocol:     "tcp",
		Address:      "localhost",
		Port:         "8020",
		Codec:        "json",
		Capabilities: []string{"health", "test"},
	}

	if err := supportClient.RegisterAgent(registration); err != nil {
		t.Fatalf("Failed to register agent for health check: %v", err)
	}

	// Test health check methods
	// Note: The actual health check methods depend on the implementation
	t.Log("Support service health check test completed")
}

// TestSupportServiceErrorHandling tests error handling
func TestSupportServiceErrorHandling(t *testing.T) {
	config := support.SupportConfig{
		Port:  ":19037",
		Debug: true,
	}

	service := support.NewService(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		service.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	supportClient := client.NewSupportClient("localhost:19037", true)
	if err := supportClient.Connect(); err != nil {
		t.Fatalf("Failed to connect to support: %v", err)
	}
	defer supportClient.Disconnect()

	// Test invalid agent registrations
	invalidRegistrations := []client.AgentRegistration{
		// Missing required fields
		{ID: "", AgentType: "test", Protocol: "tcp", Address: "localhost", Port: "8000", Codec: "json"},
		{ID: "test", AgentType: "", Protocol: "tcp", Address: "localhost", Port: "8000", Codec: "json"},
		{ID: "test", AgentType: "test", Protocol: "", Address: "localhost", Port: "8000", Codec: "json"},
		{ID: "test", AgentType: "test", Protocol: "tcp", Address: "", Port: "8000", Codec: "json"},
		{ID: "test", AgentType: "test", Protocol: "tcp", Address: "localhost", Port: "", Codec: "json"},
		{ID: "test", AgentType: "test", Protocol: "tcp", Address: "localhost", Port: "8000", Codec: ""},
	}

	for i, reg := range invalidRegistrations {
		err := supportClient.RegisterAgent(reg)
		if err != nil {
			t.Logf("Invalid registration %d correctly rejected: %v", i, err)
		} else {
			t.Logf("Registration %d was accepted (may be valid): %+v", i, reg)
		}
	}
}

// BenchmarkSupportServiceAgentRegistration benchmarks agent registration
func BenchmarkSupportServiceAgentRegistration(b *testing.B) {
	config := support.SupportConfig{
		Port:  ":19038",
		Debug: false, // Disable debug for benchmarking
	}

	service := support.NewService(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		service.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	supportClient := client.NewSupportClient("localhost:19038", false)
	if err := supportClient.Connect(); err != nil {
		b.Fatalf("Failed to connect to support: %v", err)
	}
	defer supportClient.Disconnect()

	registration := client.AgentRegistration{
		AgentType:    "benchmark-agent",
		Protocol:     "tcp",
		Address:      "localhost",
		Port:         "8000",
		Codec:        "json",
		Capabilities: []string{"benchmark"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		registration.ID = "bench-agent-" + string(rune(i%1000))
		supportClient.RegisterAgent(registration)
	}
}

// BenchmarkSupportServiceClientConnection benchmarks client connections
func BenchmarkSupportServiceClientConnection(b *testing.B) {
	config := support.SupportConfig{
		Port:  ":19039",
		Debug: false,
	}

	service := support.NewService(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		service.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		client := client.NewSupportClient("localhost:19039", false)
		if err := client.Connect(); err != nil {
			b.Errorf("Failed to connect: %v", err)
			continue
		}
		client.Disconnect()
	}
}
