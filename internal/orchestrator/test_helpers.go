package orchestrator

import (
	"github.com/tenzoki/gox/internal/agent"
)

// Test helpers for orchestrator testing

// InitializeForTesting initializes the orchestrator for unit testing
func (o *PipelineOrchestrator) InitializeForTesting() {
	if o.dependencyGraph == nil {
		o.dependencyGraph = make(map[string][]string)
	}
	if o.agents == nil {
		o.agents = make(map[string]*AgentNode)
	}
}

// GetStartupOrder returns the startup order for testing
func (o *PipelineOrchestrator) GetStartupOrder() []string {
	return o.startupOrder
}

// GetShutdownOrder returns the shutdown order for testing
func (o *PipelineOrchestrator) GetShutdownOrder() []string {
	return o.shutdownOrder
}

// AddTestAgent adds a test agent to the orchestrator state
func (o *PipelineOrchestrator) AddTestAgent(id, agentType string, dependencies, dependents []string) {
	o.agentsMux.Lock()
	defer o.agentsMux.Unlock()

	if o.agents == nil {
		o.agents = make(map[string]*AgentNode)
	}

	o.agents[id] = &AgentNode{
		ID:           id,
		AgentType:    agentType,
		State:        agent.StateInstalled,
		Dependencies: dependencies,
		Dependents:   dependents,
		Config:       make(map[string]interface{}),
	}
}
