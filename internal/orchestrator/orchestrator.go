package orchestrator

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/tenzoki/gox/internal/agent"
	"github.com/tenzoki/gox/internal/client"
)

// Agent states (copied from agent package to avoid circular import)
const (
	StateInstalled  agent.AgentState = "installed"
	StateConfigured agent.AgentState = "configured"
	StateReady      agent.AgentState = "ready"
	StateRunning    agent.AgentState = "running"
	StatePaused     agent.AgentState = "paused"
	StateStopped    agent.AgentState = "stopped"
	StateError      agent.AgentState = "error"
)

// PipelineOrchestrator manages agent startup/shutdown with dependency resolution
type PipelineOrchestrator struct {
	supportClient *client.SupportClient
	debug         bool

	// Dependency graph and state
	dependencyGraph map[string][]string   // agentID -> list of dependencies
	agents          map[string]*AgentNode // agentID -> agent metadata
	agentsMux       sync.RWMutex

	// Orchestration state
	startupOrder     []string // Resolved dependency order
	shutdownOrder    []string // Reverse order for shutdown
	orchestrationMux sync.Mutex

	// Event channels
	stateChanges    chan agent.StateChange
	orchestratorCtx context.Context
	cancel          context.CancelFunc
}

// AgentNode represents an agent in the dependency graph
type AgentNode struct {
	ID           string                 `json:"id"`
	AgentType    string                 `json:"agent_type"`
	State        agent.AgentState       `json:"state"`
	Dependencies []string               `json:"dependencies"`
	Dependents   []string               `json:"dependents"`
	Config       map[string]interface{} `json:"config"`
	LastPing     time.Time              `json:"last_ping"`

	// Runtime state
	startAttempts    int
	lastStartAttempt time.Time
	isStarting       bool
	isStopping       bool
}

// DependencyConfig represents agent dependencies in cells.yaml
type DependencyConfig struct {
	AgentID      string   `yaml:"agent_id"`
	Dependencies []string `yaml:"dependencies"`
	StartTimeout string   `yaml:"start_timeout,omitempty"`
	Retries      int      `yaml:"retries,omitempty"`
}

// OrchestrationConfig holds orchestrator settings
type OrchestrationConfig struct {
	SupportAddress      string        `yaml:"support_address"`
	Debug               bool          `yaml:"debug"`
	StartupTimeout      time.Duration `yaml:"startup_timeout"`
	MaxRetries          int           `yaml:"max_retries"`
	RetryDelay          time.Duration `yaml:"retry_delay"`
	HealthCheckInterval time.Duration `yaml:"health_check_interval"`
}

// NewPipelineOrchestrator creates a new orchestrator instance
func NewPipelineOrchestrator(config OrchestrationConfig) (*PipelineOrchestrator, error) {
	supportClient := client.NewSupportClient(config.SupportAddress, config.Debug)
	if err := supportClient.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to support service: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	orchestrator := &PipelineOrchestrator{
		supportClient:   supportClient,
		debug:           config.Debug,
		dependencyGraph: make(map[string][]string),
		agents:          make(map[string]*AgentNode),
		stateChanges:    make(chan agent.StateChange, 100),
		orchestratorCtx: ctx,
		cancel:          cancel,
	}

	return orchestrator, nil
}

// LoadDependencies loads agent dependencies from configuration
func (o *PipelineOrchestrator) LoadDependencies(dependencies []DependencyConfig) error {
	o.orchestrationMux.Lock()
	defer o.orchestrationMux.Unlock()

	if o.debug {
		log.Printf("Loading %d agent dependencies", len(dependencies))
	}

	// Build dependency graph
	for _, dep := range dependencies {
		o.dependencyGraph[dep.AgentID] = dep.Dependencies

		// Initialize agent node if not exists
		if _, exists := o.agents[dep.AgentID]; !exists {
			o.agents[dep.AgentID] = &AgentNode{
				ID:           dep.AgentID,
				Dependencies: dep.Dependencies,
				Dependents:   make([]string, 0),
				Config:       make(map[string]interface{}),
				State:        agent.StateInstalled,
			}
		}
	}

	// Build reverse dependencies (dependents)
	for agentID, deps := range o.dependencyGraph {
		for _, depID := range deps {
			if depAgent, exists := o.agents[depID]; exists {
				depAgent.Dependents = append(depAgent.Dependents, agentID)
			}
		}
	}

	// Validate and resolve dependency order
	if err := o.resolveDependencyOrder(); err != nil {
		return fmt.Errorf("failed to resolve dependencies: %w", err)
	}

	if o.debug {
		log.Printf("Dependency resolution complete. Startup order: %v", o.startupOrder)
	}

	return nil
}

// resolveDependencyOrder performs topological sort to determine startup order
func (o *PipelineOrchestrator) resolveDependencyOrder() error {
	// Detect circular dependencies using DFS
	if err := o.detectCircularDependencies(); err != nil {
		return err
	}

	// Kahn's algorithm for topological sort
	inDegree := make(map[string]int)
	queue := make([]string, 0)
	result := make([]string, 0)

	// Calculate in-degrees
	for agentID := range o.agents {
		inDegree[agentID] = len(o.dependencyGraph[agentID])
	}

	// Find nodes with no dependencies (in-degree 0)
	for agentID, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, agentID)
		}
	}

	// Process queue
	for len(queue) > 0 {
		// Sort queue for deterministic ordering
		sort.Strings(queue)
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)

		// Update dependents
		if agent, exists := o.agents[current]; exists {
			for _, dependent := range agent.Dependents {
				inDegree[dependent]--
				if inDegree[dependent] == 0 {
					queue = append(queue, dependent)
				}
			}
		}
	}

	// Check if all nodes were processed
	if len(result) != len(o.agents) {
		return fmt.Errorf("circular dependency detected - could not resolve all dependencies")
	}

	o.startupOrder = result
	// Shutdown order is reverse of startup order
	o.shutdownOrder = make([]string, len(result))
	for i, agentID := range result {
		o.shutdownOrder[len(result)-1-i] = agentID
	}

	return nil
}

// detectCircularDependencies uses DFS to detect cycles
func (o *PipelineOrchestrator) detectCircularDependencies() error {
	color := make(map[string]int) // 0=white, 1=gray, 2=black
	path := make([]string, 0)

	var dfs func(string) error
	dfs = func(agentID string) error {
		if color[agentID] == 1 {
			// Found a back edge - circular dependency
			cycle := append(path, agentID)
			return fmt.Errorf("circular dependency detected: %v", cycle)
		}
		if color[agentID] == 2 {
			return nil // Already processed
		}

		color[agentID] = 1 // Mark as gray (visiting)
		path = append(path, agentID)

		for _, depID := range o.dependencyGraph[agentID] {
			if err := dfs(depID); err != nil {
				return err
			}
		}

		color[agentID] = 2 // Mark as black (done)
		path = path[:len(path)-1]
		return nil
	}

	// Check all nodes
	for agentID := range o.agents {
		if color[agentID] == 0 {
			if err := dfs(agentID); err != nil {
				return err
			}
		}
	}

	return nil
}

// StartPipeline orchestrates the startup of all agents in dependency order
func (o *PipelineOrchestrator) StartPipeline(ctx context.Context, startupTimeout time.Duration) error {
	o.orchestrationMux.Lock()
	defer o.orchestrationMux.Unlock()

	if o.debug {
		log.Printf("Starting pipeline with %d agents", len(o.startupOrder))
	}

	// Start monitoring state changes
	go o.monitorStateChanges()

	// Start agents in dependency order
	for _, agentID := range o.startupOrder {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("startup cancelled: %w", err)
		}

		if err := o.startAgent(ctx, agentID, startupTimeout); err != nil {
			return fmt.Errorf("failed to start agent %s: %w", agentID, err)
		}
	}

	if o.debug {
		log.Printf("Pipeline startup completed successfully")
	}

	return nil
}

// startAgent starts a single agent and waits for dependencies
func (o *PipelineOrchestrator) startAgent(ctx context.Context, agentID string, timeout time.Duration) error {
	agent, exists := o.agents[agentID]
	if !exists {
		return fmt.Errorf("agent not found: %s", agentID)
	}

	if o.debug {
		log.Printf("Starting agent %s (dependencies: %v)", agentID, agent.Dependencies)
	}

	// Wait for all dependencies to reach "ready" state
	for _, depID := range agent.Dependencies {
		if err := o.waitForAgentState(ctx, depID, StateReady, timeout); err != nil {
			return fmt.Errorf("dependency %s not ready: %w", depID, err)
		}
	}

	// Mark agent as starting
	o.agentsMux.Lock()
	agent.isStarting = true
	agent.startAttempts++
	agent.lastStartAttempt = time.Now()
	o.agentsMux.Unlock()

	// Signal agent to start (this would trigger agent startup logic)
	// In practice, this might send a start command to the agent
	if err := o.signalAgentStart(agentID); err != nil {
		return fmt.Errorf("failed to signal start for agent %s: %w", agentID, err)
	}

	// Wait for agent to reach ready state
	if err := o.waitForAgentState(ctx, agentID, StateReady, timeout); err != nil {
		return fmt.Errorf("agent %s failed to reach ready state: %w", agentID, err)
	}

	o.agentsMux.Lock()
	agent.isStarting = false
	o.agentsMux.Unlock()

	if o.debug {
		log.Printf("Agent %s started successfully", agentID)
	}

	return nil
}

// waitForAgentState waits for an agent to reach a specific state
func (o *PipelineOrchestrator) waitForAgentState(ctx context.Context, agentID string, targetState agent.AgentState, timeout time.Duration) error {
	// Check current state first
	if currentState := o.getAgentState(agentID); currentState == targetState {
		return nil
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			currentState := o.getAgentState(agentID)
			return fmt.Errorf("timeout waiting for agent %s to reach %s (current: %s)",
				agentID, targetState, currentState)
		case <-ticker.C:
			currentState := o.getAgentState(agentID)
			if currentState == targetState {
				return nil
			}
			if currentState == agent.StateError {
				return fmt.Errorf("agent %s entered error state", agentID)
			}
		}
	}
}

// getAgentState retrieves current state from support service
func (o *PipelineOrchestrator) getAgentState(agentID string) agent.AgentState {
	// This would make an RPC call to support service to get agent state
	// For now, return the cached state
	o.agentsMux.RLock()
	defer o.agentsMux.RUnlock()

	if agentNode, exists := o.agents[agentID]; exists {
		return agentNode.State
	}
	return agent.StateError
}

// signalAgentStart signals an agent to start (placeholder for actual implementation)
func (o *PipelineOrchestrator) signalAgentStart(agentID string) error {
	// This would send a start command to the agent
	// Implementation depends on how agents are controlled
	if o.debug {
		log.Printf("Signaling start for agent %s", agentID)
	}
	return nil
}

// StopPipeline gracefully shuts down all agents in reverse dependency order
func (o *PipelineOrchestrator) StopPipeline(ctx context.Context, shutdownTimeout time.Duration) error {
	o.orchestrationMux.Lock()
	defer o.orchestrationMux.Unlock()

	if o.debug {
		log.Printf("Stopping pipeline with %d agents", len(o.shutdownOrder))
	}

	// Stop agents in reverse dependency order
	for _, agentID := range o.shutdownOrder {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("shutdown cancelled: %w", err)
		}

		if err := o.stopAgent(ctx, agentID, shutdownTimeout); err != nil {
			// Log error but continue shutdown
			log.Printf("Error stopping agent %s: %v", agentID, err)
		}
	}

	if o.debug {
		log.Printf("Pipeline shutdown completed")
	}

	return nil
}

// stopAgent stops a single agent
func (o *PipelineOrchestrator) stopAgent(ctx context.Context, agentID string, timeout time.Duration) error {
	agent, exists := o.agents[agentID]
	if !exists {
		return fmt.Errorf("agent not found: %s", agentID)
	}

	if o.debug {
		log.Printf("Stopping agent %s", agentID)
	}

	// Mark agent as stopping
	o.agentsMux.Lock()
	agent.isStopping = true
	o.agentsMux.Unlock()

	// Signal agent to stop
	if err := o.signalAgentStop(agentID); err != nil {
		return fmt.Errorf("failed to signal stop for agent %s: %w", agentID, err)
	}

	// Wait for agent to reach stopped state
	if err := o.waitForAgentState(ctx, agentID, StateStopped, timeout); err != nil {
		return fmt.Errorf("agent %s failed to stop gracefully: %w", agentID, err)
	}

	o.agentsMux.Lock()
	agent.isStopping = false
	o.agentsMux.Unlock()

	if o.debug {
		log.Printf("Agent %s stopped successfully", agentID)
	}

	return nil
}

// signalAgentStop signals an agent to stop
func (o *PipelineOrchestrator) signalAgentStop(agentID string) error {
	if o.debug {
		log.Printf("Signaling stop for agent %s", agentID)
	}
	return nil
}

// monitorStateChanges listens for agent state changes
func (o *PipelineOrchestrator) monitorStateChanges() {
	for {
		select {
		case <-o.orchestratorCtx.Done():
			return
		case stateChange := <-o.stateChanges:
			o.handleStateChange(stateChange)
		}
	}
}

// handleStateChange processes agent state changes
func (o *PipelineOrchestrator) handleStateChange(change agent.StateChange) {
	o.agentsMux.Lock()
	defer o.agentsMux.Unlock()

	if agentNode, exists := o.agents[change.AgentID]; exists {
		agentNode.State = change.ToState
		agentNode.LastPing = change.Timestamp

		if o.debug {
			log.Printf("Agent %s state change: %s → %s",
				change.AgentID, change.FromState, change.ToState)
		}
	}
}

// GetPipelineStatus returns the current status of all agents
func (o *PipelineOrchestrator) GetPipelineStatus() map[string]*AgentNode {
	o.agentsMux.RLock()
	defer o.agentsMux.RUnlock()

	status := make(map[string]*AgentNode)
	for id, agent := range o.agents {
		// Return a copy to avoid race conditions
		status[id] = &AgentNode{
			ID:           agent.ID,
			AgentType:    agent.AgentType,
			State:        agent.State,
			Dependencies: agent.Dependencies,
			Dependents:   agent.Dependents,
			LastPing:     agent.LastPing,
		}
	}
	return status
}

// Shutdown gracefully shuts down the orchestrator
func (o *PipelineOrchestrator) Shutdown() error {
	o.cancel()

	if o.supportClient != nil {
		return o.supportClient.Disconnect()
	}

	return nil
}
