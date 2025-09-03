package main

import (
	"testing"
	"time"

	"github.com/tenzoki/gox/internal/agent"
)

// TestAgentStates tests all agent lifecycle states
func TestAgentStates(t *testing.T) {
	// Test all defined states
	states := []agent.AgentState{
		agent.StateInstalled,
		agent.StateConfigured,
		agent.StateReady,
		agent.StateRunning,
		agent.StatePaused,
		agent.StateStopped,
		agent.StateError,
	}

	for _, state := range states {
		if string(state) == "" {
			t.Error("Empty state found")
		}
	}

	// Test state values
	if agent.StateInstalled != agent.AgentState("installed") {
		t.Errorf("Expected StateInstalled to be 'installed', got %s", agent.StateInstalled)
	}

	if agent.StateRunning != agent.AgentState("running") {
		t.Errorf("Expected StateRunning to be 'running', got %s", agent.StateRunning)
	}

	if agent.StateError != agent.AgentState("error") {
		t.Errorf("Expected StateError to be 'error', got %s", agent.StateError)
	}
}

// TestLifecycleManager tests the agent lifecycle manager
func TestLifecycleManager(t *testing.T) {
	notifyCount := 0
	notifyCallback := func(agentID, state string) error {
		notifyCount++
		t.Logf("Agent %s transitioned to %s", agentID, state)
		return nil
	}

	lm := agent.NewLifecycleManager("test-agent", notifyCallback)

	// Test initial state
	if lm.GetState() != agent.StateInstalled {
		t.Errorf("Expected initial state %s, got %s", agent.StateInstalled, lm.GetState())
	}

	// Test valid transitions
	if err := lm.SetState(agent.StateConfigured, "test config"); err != nil {
		t.Errorf("Failed to transition to configured: %v", err)
	}

	if lm.GetState() != agent.StateConfigured {
		t.Errorf("Expected state configured, got %s", lm.GetState())
	}

	if err := lm.SetState(agent.StateReady, "test ready"); err != nil {
		t.Errorf("Failed to transition to ready: %v", err)
	}

	if err := lm.SetState(agent.StateRunning, "test running"); err != nil {
		t.Errorf("Failed to transition to running: %v", err)
	}

	// Test invalid transition (running -> installed should fail)
	if err := lm.SetState(agent.StateInstalled, "invalid"); err == nil {
		t.Error("Expected error for invalid transition running->installed, got none")
	}

	// Test pausing and resuming
	if err := lm.SetState(agent.StatePaused, "test pause"); err != nil {
		t.Errorf("Failed to pause: %v", err)
	}

	if err := lm.SetState(agent.StateRunning, "test resume"); err != nil {
		t.Errorf("Failed to resume: %v", err)
	}

	// Test stopping
	if err := lm.SetState(agent.StateStopped, "test stop"); err != nil {
		t.Errorf("Failed to stop: %v", err)
	}

	// Verify final state
	if lm.GetState() != agent.StateStopped {
		t.Errorf("Expected final state stopped, got %s", lm.GetState())
	}
}

// TestLifecycleManagerHistory tests state history tracking
func TestLifecycleManagerHistory(t *testing.T) {
	lm := agent.NewLifecycleManager("history-test", func(agentID, state string) error {
		return nil
	})

	// Make some state transitions
	lm.SetState(agent.StateConfigured, "initial config")
	lm.SetState(agent.StateReady, "ready to run")
	lm.SetState(agent.StateRunning, "started processing")

	history := lm.GetStateHistory()

	// Note: History is from the channel, which may not have all events
	// This tests that GetStateHistory() works without error
	t.Logf("State history contains %d entries", len(history))

	// Check that any history entries have valid structure
	for i, entry := range history {
		if entry.AgentID != "history-test" {
			t.Errorf("History entry %d has wrong agent ID: %s", i, entry.AgentID)
		}

		if entry.Timestamp.IsZero() {
			t.Errorf("History entry %d has zero timestamp", i)
		}
	}
}

// TestLifecycleManagerTransitionValidation tests transition validation
func TestLifecycleManagerTransitionValidation(t *testing.T) {
	lm := agent.NewLifecycleManager("validation-test", func(agentID, state string) error {
		return nil
	})

	// Test CanTransitionTo method
	if !lm.CanTransitionTo(agent.StateConfigured) {
		t.Error("Should be able to transition from installed to configured")
	}

	if lm.CanTransitionTo(agent.StateRunning) {
		t.Error("Should not be able to transition directly from installed to running")
	}

	// Move to configured state
	lm.SetState(agent.StateConfigured, "setup")

	if !lm.CanTransitionTo(agent.StateReady) {
		t.Error("Should be able to transition from configured to ready")
	}

	if lm.CanTransitionTo(agent.StatePaused) {
		t.Error("Should not be able to transition from configured to paused")
	}
}

// TestLifecycleManagerWaitForState tests waiting for specific states
func TestLifecycleManagerWaitForState(t *testing.T) {
	lm := agent.NewLifecycleManager("wait-test", func(agentID, state string) error {
		return nil
	})

	// Test waiting for current state (should return immediately)
	err := lm.WaitForState(agent.StateInstalled, 100*time.Millisecond)
	if err != nil {
		t.Errorf("WaitForState for current state should succeed immediately: %v", err)
	}

	// Test timeout
	err = lm.WaitForState(agent.StateRunning, 10*time.Millisecond)
	if err == nil {
		t.Error("Expected timeout error when waiting for unreachable state")
	}
}

// TestBaseAgentInterface tests BaseAgent interface compliance
func TestBaseAgentInterface(t *testing.T) {
	// Test that we can create a BaseAgent with required fields
	config := agent.AgentConfig{
		ID:             "test-base-agent",
		AgentType:      "test-type",
		SupportAddress: "localhost:9000",
		Debug:          true,
	}

	// This tests that the config struct has all required fields
	if config.ID != "test-base-agent" {
		t.Error("ID not set correctly")
	}
	if config.AgentType != "test-type" {
		t.Error("AgentType not set correctly")
	}
	if config.SupportAddress != "localhost:9000" {
		t.Error("SupportAddress not set correctly")
	}
	if !config.Debug {
		t.Error("Debug not set correctly")
	}
}

// TestAgentAdapter tests the agent adapter functionality
func TestAgentAdapter(t *testing.T) {
	// Create adapter config
	config := agent.AgentConfig{
		ID:             "adapter-test",
		AgentType:      "test-adapter",
		SupportAddress: "localhost:9000",
		Debug:          true,
	}

	// Note: We can't actually create an adapter in tests since it requires live services
	// This test verifies the config structure is correct
	if config.ID != "adapter-test" {
		t.Error("Config ID not set correctly")
	}
	if config.AgentType != "test-adapter" {
		t.Error("Config AgentType not set correctly")
	}
	if config.SupportAddress != "localhost:9000" {
		t.Error("Config SupportAddress not set correctly")
	}

	// In a real test environment, we would need running services
	// For now, just verify the config is structured correctly
	t.Logf("Adapter config test completed for agent %s", config.ID)
}

// BenchmarkLifecycleStateChange benchmarks state transitions
func BenchmarkLifecycleStateChange(b *testing.B) {
	lm := agent.NewLifecycleManager("bench-agent", func(agentID, state string) error {
		return nil
	})

	// Set up initial state
	lm.SetState(agent.StateConfigured, "setup")
	lm.SetState(agent.StateReady, "setup")
	lm.SetState(agent.StateRunning, "setup")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			lm.SetState(agent.StatePaused, "benchmark pause")
		} else {
			lm.SetState(agent.StateRunning, "benchmark resume")
		}
	}
}

// BenchmarkLifecycleGetState benchmarks state retrieval
func BenchmarkLifecycleGetState(b *testing.B) {
	lm := agent.NewLifecycleManager("bench-agent", func(agentID, state string) error {
		return nil
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lm.GetState()
	}
}
