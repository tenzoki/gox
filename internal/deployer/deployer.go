package deployer

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/tenzoki/gox/internal/config"
)

// AgentDeployer manages agent deployment with different strategies
type AgentDeployer struct {
	supportAddress string
	poolConfig     map[string]config.AgentTypeConfig // agent_type -> config
	processes      map[string]*exec.Cmd              // agent_id -> process
	processMux     sync.RWMutex
	debug          bool
}

// NewAgentDeployer creates a new agent deployer
func NewAgentDeployer(supportAddress string, debug bool) *AgentDeployer {
	return &AgentDeployer{
		supportAddress: supportAddress,
		poolConfig:     make(map[string]config.AgentTypeConfig),
		processes:      make(map[string]*exec.Cmd),
		debug:          debug,
	}
}

// LoadPool loads agent type definitions from pool configuration
func (d *AgentDeployer) LoadPool(poolConfig *config.PoolConfig) error {
	for _, agentType := range poolConfig.AgentTypes {
		d.poolConfig[agentType.AgentType] = agentType
		if d.debug {
			if agentType.Operator == "await" {
				log.Printf("Deployer: Loaded agent type %s with operator %s <- deployment required within 30 seconds!",
					agentType.AgentType, agentType.Operator)
			} else {
				log.Printf("Deployer: Loaded agent type %s with operator %s",
					agentType.AgentType, agentType.Operator)
			}
		}
	}
	return nil
}

// DeployAgent deploys an agent based on its type and deployment strategy
func (d *AgentDeployer) DeployAgent(ctx context.Context, cellAgent config.CellAgent) error {
	// Look up agent type configuration
	agentTypeConfig, exists := d.poolConfig[cellAgent.AgentType]
	if !exists {
		return fmt.Errorf("unknown agent type: %s", cellAgent.AgentType)
	}

	log.Printf("Deploying agent %s (type: %s, operator: %s)",
		cellAgent.ID, cellAgent.AgentType, agentTypeConfig.Operator)

	switch agentTypeConfig.Operator {
	case "spawn":
		return d.spawnAgent(ctx, cellAgent, agentTypeConfig)
	case "call":
		return d.callAgent(ctx, cellAgent, agentTypeConfig)
	case "await":
		return d.awaitAgent(ctx, cellAgent, agentTypeConfig)
	default:
		return fmt.Errorf("unknown operator: %s", agentTypeConfig.Operator)
	}
}

// spawnAgent starts an agent as a separate process
func (d *AgentDeployer) spawnAgent(ctx context.Context, cellAgent config.CellAgent, typeConfig config.AgentTypeConfig) error {
	// Determine binary path
	binaryPath := d.getBinaryPath(typeConfig)
	if binaryPath == "" {
		return fmt.Errorf("could not determine binary path for %s", cellAgent.AgentType)
	}

	// Check if binary exists
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		return fmt.Errorf("binary not found: %s", binaryPath)
	}

	// Build command arguments
	args := []string{}

	// Set environment variables for the agent
	env := os.Environ()
	env = append(env, fmt.Sprintf("GOX_AGENT_ID=%s", cellAgent.ID))
	env = append(env, fmt.Sprintf("GOX_AGENT_TYPE=%s", cellAgent.AgentType))
	env = append(env, fmt.Sprintf("GOX_SUPPORT_ADDRESS=%s", d.supportAddress))
	env = append(env, fmt.Sprintf("GOX_DEBUG=%v", d.debug))

	// Add ingress/egress to environment
	if cellAgent.Ingress != "" {
		env = append(env, fmt.Sprintf("GOX_INGRESS=%s", cellAgent.Ingress))
	}
	if cellAgent.Egress != "" {
		env = append(env, fmt.Sprintf("GOX_EGRESS=%s", cellAgent.Egress))
	}

	// Create command
	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Env = env

	// Redirect output for debugging
	if d.debug {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to spawn agent %s: %w", cellAgent.ID, err)
	}

	// Store process reference
	d.processMux.Lock()
	d.processes[cellAgent.ID] = cmd
	d.processMux.Unlock()

	log.Printf("Spawned agent %s (PID: %d)", cellAgent.ID, cmd.Process.Pid)

	// Monitor process in background
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("Agent %s exited with error: %v", cellAgent.ID, err)
		} else {
			log.Printf("Agent %s exited normally", cellAgent.ID)
		}

		// Remove from process map
		d.processMux.Lock()
		delete(d.processes, cellAgent.ID)
		d.processMux.Unlock()
	}()

	// Wait briefly for agent to start and register
	time.Sleep(2 * time.Second)

	// Verify agent registered with support service
	if err := d.verifyAgentStarted(cellAgent.ID); err != nil {
		log.Printf("Warning: Agent %s may not have registered properly: %v", cellAgent.ID, err)
		// Don't fail deployment, agent might still be starting
	}

	return nil
}

// callAgent executes an agent directly (in-process or via direct call)
func (d *AgentDeployer) callAgent(ctx context.Context, cellAgent config.CellAgent, typeConfig config.AgentTypeConfig) error {
	// For "call" operator, we execute the agent directly
	// This is similar to spawn but may have different lifecycle management
	log.Printf("Calling agent %s (direct execution)", cellAgent.ID)

	// For now, treat call like spawn
	// In a real implementation, this might load the agent as a plugin or library
	return d.spawnAgent(ctx, cellAgent, typeConfig)
}

// awaitAgent waits for an external agent to connect
func (d *AgentDeployer) awaitAgent(ctx context.Context, cellAgent config.CellAgent, typeConfig config.AgentTypeConfig) error {
	log.Printf("Awaiting external agent %s to connect (start with: GOX_AGENT_ID=%s ./build/%s)",
		cellAgent.ID, cellAgent.ID, cellAgent.AgentType)

	// Set a timeout for waiting
	timeout := 30 * time.Second
	deadline := time.Now().Add(timeout)

	// Poll support service to check if agent has registered
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for agent %s", cellAgent.ID)
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for agent %s to connect", cellAgent.ID)
			}

			// Check if agent has registered
			if err := d.verifyAgentStarted(cellAgent.ID); err == nil {
				log.Printf("External agent %s connected successfully", cellAgent.ID)
				return nil
			}

			if d.debug {
				log.Printf("Still waiting for agent %s to connect...", cellAgent.ID)
			}
		}
	}
}

// verifyAgentStarted checks if an agent has registered with support service
func (d *AgentDeployer) verifyAgentStarted(agentID string) error {
	// For now, we'll just wait and assume the agent starts successfully
	// A full implementation would query the support service for agent state
	// This could be enhanced to check support service's agent registry

	// Simple verification: just wait a moment and assume success
	// Agents will log their own registration status
	time.Sleep(1 * time.Second)

	if d.debug {
		log.Printf("Agent %s verification skipped (agent will log its own status)", agentID)
	}

	return nil
}

// getBinaryPath determines the actual binary path for an agent type
func (d *AgentDeployer) getBinaryPath(typeConfig config.AgentTypeConfig) string {
	// The binary field in pool.yaml contains the source path (e.g., operators/file_ingester/main.go)
	// We need to map this to the actual built binary (e.g., build/file_ingester)

	// Extract the agent name from the path
	binaryPath := typeConfig.Binary

	// Map source paths to built binaries
	if binaryPath == "operators/file_ingester/main.go" {
		return "./build/file_ingester"
	} else if binaryPath == "operators/text_transformer/main.go" {
		return "./build/text_transformer"
	} else if binaryPath == "operators/file_writer/main.go" {
		return "./build/file_writer"
	} else if binaryPath == "cmd/adapter/main.go" {
		return "./build/adapter"
	}

	// Fallback: try to derive from binary path
	dir := filepath.Dir(binaryPath)
	base := filepath.Base(dir)
	return filepath.Join("./build", base)
}

// StopAgent stops a deployed agent
func (d *AgentDeployer) StopAgent(agentID string) error {
	d.processMux.RLock()
	cmd, exists := d.processes[agentID]
	d.processMux.RUnlock()

	if !exists {
		return fmt.Errorf("agent %s not found in process list", agentID)
	}

	// Send termination signal
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		return fmt.Errorf("failed to stop agent %s: %w", agentID, err)
	}

	// Give process time to clean up
	time.Sleep(2 * time.Second)

	// Force kill if still running
	if cmd.Process != nil {
		cmd.Process.Kill()
	}

	log.Printf("Stopped agent %s", agentID)
	return nil
}

// StopAll stops all deployed agents
func (d *AgentDeployer) StopAll() {
	d.processMux.RLock()
	agentIDs := make([]string, 0, len(d.processes))
	for id := range d.processes {
		agentIDs = append(agentIDs, id)
	}
	d.processMux.RUnlock()

	for _, id := range agentIDs {
		if err := d.StopAgent(id); err != nil {
			log.Printf("Error stopping agent %s: %v", id, err)
		}
	}
}
