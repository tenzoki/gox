package agent

import "github.com/tenzoki/gox/internal/client"

// AgentRunner defines the interface that all agents must implement
// This interface isolates business logic from framework boilerplate
type AgentRunner interface {
	// ProcessMessage handles a single message and returns the result
	// This is where the agent's core business logic resides
	ProcessMessage(msg *client.BrokerMessage, base *BaseAgent) (*client.BrokerMessage, error)

	// Init is called once during agent startup after BaseAgent initialization
	// Agents can use this for custom initialization logic (optional)
	Init(base *BaseAgent) error

	// Cleanup is called during agent shutdown before BaseAgent cleanup  
	// Agents can use this for custom cleanup logic (optional)
	Cleanup(base *BaseAgent)
}

// DefaultAgentRunner provides default implementations for optional methods
// Agents can embed this to only implement ProcessMessage if desired
type DefaultAgentRunner struct{}

func (d *DefaultAgentRunner) Init(base *BaseAgent) error {
	return nil
}

func (d *DefaultAgentRunner) Cleanup(base *BaseAgent) {
	// Default: no custom cleanup needed
}