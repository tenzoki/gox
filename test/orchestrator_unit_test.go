package main

import (
	"testing"

	"github.com/tenzoki/gox/internal/orchestrator"
)

// TestDependencyResolution tests topological sorting of agent dependencies
func TestDependencyResolution(t *testing.T) {
	orch := &orchestrator.PipelineOrchestrator{}

	// Initialize internal state (normally done by NewPipelineOrchestrator)
	orch.InitializeForTesting()

	// Test case 1: Simple linear dependency chain
	dependencies := []orchestrator.DependencyConfig{
		{AgentID: "agent-a", Dependencies: []string{}},
		{AgentID: "agent-b", Dependencies: []string{"agent-a"}},
		{AgentID: "agent-c", Dependencies: []string{"agent-b"}},
	}

	err := orch.LoadDependencies(dependencies)
	if err != nil {
		t.Fatalf("Failed to load simple dependencies: %v", err)
	}

	expected := []string{"agent-a", "agent-b", "agent-c"}
	startupOrder := orch.GetStartupOrder()
	if !equalSlices(startupOrder, expected) {
		t.Errorf("Expected startup order %v, got %v", expected, startupOrder)
	}

	expectedShutdown := []string{"agent-c", "agent-b", "agent-a"}
	shutdownOrder := orch.GetShutdownOrder()
	if !equalSlices(shutdownOrder, expectedShutdown) {
		t.Errorf("Expected shutdown order %v, got %v", expectedShutdown, shutdownOrder)
	}
}

// TestCircularDependencyDetection tests detection of circular dependencies
func TestCircularDependencyDetection(t *testing.T) {
	orch := &orchestrator.PipelineOrchestrator{}
	orch.InitializeForTesting()

	// Test case: Circular dependency
	dependencies := []orchestrator.DependencyConfig{
		{AgentID: "agent-a", Dependencies: []string{"agent-b"}},
		{AgentID: "agent-b", Dependencies: []string{"agent-c"}},
		{AgentID: "agent-c", Dependencies: []string{"agent-a"}}, // Creates cycle
	}

	err := orch.LoadDependencies(dependencies)
	if err == nil {
		t.Fatal("Expected error for circular dependency, but got none")
	}

	if !contains(err.Error(), "circular") {
		t.Errorf("Expected circular dependency error, got: %v", err)
	}
}

// TestComplexDependencyGraph tests complex dependency resolution
func TestComplexDependencyGraph(t *testing.T) {
	orch := &orchestrator.PipelineOrchestrator{}
	orch.InitializeForTesting()

	// Test case: Complex dependency graph
	//     A   B
	//    / \ /
	//   C   D
	//   |   |
	//   E   F
	//    \ /
	//     G
	dependencies := []orchestrator.DependencyConfig{
		{AgentID: "A", Dependencies: []string{}},
		{AgentID: "B", Dependencies: []string{}},
		{AgentID: "C", Dependencies: []string{"A"}},
		{AgentID: "D", Dependencies: []string{"A", "B"}},
		{AgentID: "E", Dependencies: []string{"C"}},
		{AgentID: "F", Dependencies: []string{"D"}},
		{AgentID: "G", Dependencies: []string{"E", "F"}},
	}

	err := orch.LoadDependencies(dependencies)
	if err != nil {
		t.Fatalf("Failed to load complex dependencies: %v", err)
	}

	// Verify that dependencies are respected in startup order
	order := orch.GetStartupOrder()
	positionMap := make(map[string]int)
	for i, agentID := range order {
		positionMap[agentID] = i
	}

	// Check that each agent comes after its dependencies
	for _, dep := range dependencies {
		agentPos := positionMap[dep.AgentID]
		for _, depID := range dep.Dependencies {
			depPos := positionMap[depID]
			if depPos >= agentPos {
				t.Errorf("Agent %s (pos %d) should come after dependency %s (pos %d)",
					dep.AgentID, agentPos, depID, depPos)
			}
		}
	}

	// Verify G comes last
	if order[len(order)-1] != "G" {
		t.Errorf("Expected G to be last, got %s", order[len(order)-1])
	}
}

// TestSelfDependency tests detection of self-dependency
func TestSelfDependency(t *testing.T) {
	orch := &orchestrator.PipelineOrchestrator{}
	orch.InitializeForTesting()

	// Test case: Self dependency (should be detected as circular)
	dependencies := []orchestrator.DependencyConfig{
		{AgentID: "agent-a", Dependencies: []string{"agent-a"}},
	}

	err := orch.LoadDependencies(dependencies)
	if err == nil {
		t.Fatal("Expected error for self dependency, but got none")
	}

	if !contains(err.Error(), "circular") {
		t.Errorf("Expected circular dependency error for self-dependency, got: %v", err)
	}
}

// TestEmptyDependencies tests handling of no dependencies
func TestEmptyDependencies(t *testing.T) {
	orch := &orchestrator.PipelineOrchestrator{}
	orch.InitializeForTesting()

	// Test case: No dependencies
	dependencies := []orchestrator.DependencyConfig{}

	err := orch.LoadDependencies(dependencies)
	if err != nil {
		t.Fatalf("Failed to load empty dependencies: %v", err)
	}

	if len(orch.GetStartupOrder()) != 0 {
		t.Errorf("Expected empty startup order, got %v", orch.GetStartupOrder())
	}
}

// TestPipelineStatus tests pipeline status reporting
func TestPipelineStatus(t *testing.T) {
	orch := &orchestrator.PipelineOrchestrator{}
	orch.InitializeForTesting()

	// Add test agents directly to pipeline state
	orch.AddTestAgent("agent-1", "test-type", []string{"agent-0"}, []string{"agent-2"})
	orch.AddTestAgent("agent-2", "test-type", []string{"agent-1"}, []string{})

	status := orch.GetPipelineStatus()

	if len(status) != 2 {
		t.Errorf("Expected 2 agents in status, got %d", len(status))
	}

	agent1 := status["agent-1"]
	if agent1 == nil {
		t.Fatal("Expected agent-1 in status")
	}

	if len(agent1.Dependencies) != 1 || agent1.Dependencies[0] != "agent-0" {
		t.Errorf("Expected agent-1 dependencies [agent-0], got %v", agent1.Dependencies)
	}

	if len(agent1.Dependents) != 1 || agent1.Dependents[0] != "agent-2" {
		t.Errorf("Expected agent-1 dependents [agent-2], got %v", agent1.Dependents)
	}
}

// Helper functions
func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contains(str, substr string) bool {
	return len(str) >= len(substr) &&
		(str == substr ||
			str[:len(substr)] == substr ||
			str[len(str)-len(substr):] == substr ||
			findInString(str, substr))
}

func findInString(str, substr string) bool {
	for i := 0; i <= len(str)-len(substr); i++ {
		if str[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Benchmark tests
func BenchmarkDependencyResolution(b *testing.B) {
	dependencies := make([]orchestrator.DependencyConfig, 100)
	for i := 0; i < 100; i++ {
		var deps []string
		if i > 0 {
			deps = []string{formatAgentID(i - 1)}
		}
		dependencies[i] = orchestrator.DependencyConfig{
			AgentID:      formatAgentID(i),
			Dependencies: deps,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		orch := &orchestrator.PipelineOrchestrator{}
		orch.InitializeForTesting()
		orch.LoadDependencies(dependencies)
	}
}

func formatAgentID(i int) string {
	return "agent-" + string(rune('0'+i%10))
}
