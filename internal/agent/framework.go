package agent

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tenzoki/gox/internal/client"
)

// AgentFramework provides the complete agent runtime framework
// This eliminates all boilerplate code from individual agents
type AgentFramework struct {
	runner    AgentRunner
	baseAgent *BaseAgent
	handlers  *ConnectionHandlers
	agentType string
}

// NewFramework creates a new agent framework instance
func NewFramework(runner AgentRunner, agentType string) *AgentFramework {
	return &AgentFramework{
		runner:    runner,
		agentType: agentType,
	}
}

// Run executes the complete agent lifecycle
// This handles ALL the boilerplate that was repeated across agents:
// - BaseAgent initialization 
// - Connection setup (ingress/egress parsing)
// - Signal handling
// - Message processing loop
// - Graceful shutdown
func (f *AgentFramework) Run() error {
	// Step 1: Initialize BaseAgent (replaces lines 15-37 from all agents)
	if err := f.initializeBaseAgent(); err != nil {
		return fmt.Errorf("failed to initialize base agent: %w", err)
	}
	defer f.baseAgent.Stop()

	// Step 2: Setup connections (replaces connection parsing from all agents)
	if err := f.setupConnections(); err != nil {
		return fmt.Errorf("failed to setup connections: %w", err)
	}

	// Step 3: Call agent-specific initialization
	if err := f.runner.Init(f.baseAgent); err != nil {
		return fmt.Errorf("failed to initialize agent runner: %w", err)
	}
	defer f.runner.Cleanup(f.baseAgent)

	// Step 4: Start message processing (replaces message loops from all agents)
	msgChan, err := f.startMessageProcessing()
	if err != nil {
		return fmt.Errorf("failed to start message processing: %w", err)
	}

	f.baseAgent.LogInfo("%s started successfully (PID: %d), waiting for shutdown signal", 
		f.agentType, os.Getpid())

	// Step 5: Handle shutdown signals (replaces lines 110-124 from all agents)
	return f.handleShutdown(msgChan)
}

// initializeBaseAgent handles the common BaseAgent setup
func (f *AgentFramework) initializeBaseAgent() error {
	debug := GetDebugFromEnv()
	supportAddr := GetEnvConfig("SUPPORT_ADDR", "localhost:9000")

	agentType := GetAgentType(f.agentType)
	agentID := GetAgentID(agentType)

	// This config structure is identical across all agents
	agentConfig := AgentConfig{
		ID:             agentID,
		AgentType:      agentType,
		Debug:          debug,
		SupportAddress: supportAddr,
		Capabilities:   f.getCapabilities(agentType),
		RebootTimeout:  5 * time.Minute,
	}

	baseAgent, err := NewBaseAgent(agentConfig)
	if err != nil {
		return err
	}

	f.baseAgent = baseAgent
	return nil
}

// getCapabilities returns agent-type-specific capabilities
func (f *AgentFramework) getCapabilities(agentType string) []string {
	switch agentType {
	case "text-transformer":
		return []string{"text-processing", "transformation"}
	case "file-ingester":
		return []string{"file-ingestion", "directory-watching"}
	case "file-writer":
		return []string{"file-writing", "data-persistence"}
	default:
		return []string{"message-processing"}
	}
}

// setupConnections handles ingress/egress configuration and connection setup
func (f *AgentFramework) setupConnections() error {
	ingress := f.baseAgent.GetConfigString("ingress", "")
	egress := f.baseAgent.GetConfigString("egress", "")

	if ingress == "" {
		f.baseAgent.LogError("No ingress configuration provided by support service")
		return fmt.Errorf("missing ingress configuration")
	}
	if egress == "" {
		f.baseAgent.LogError("No egress configuration provided by support service")
		return fmt.Errorf("missing egress configuration")
	}

	f.baseAgent.LogInfo("Using ingress: %s, egress: %s", ingress, egress)
	f.baseAgent.LogDebug("DEBUG: Config map contents: %+v", f.baseAgent.Config)

	// Create connection handlers
	handlers, err := NewConnectionHandlers(ingress, egress, f.baseAgent)
	if err != nil {
		return err
	}

	f.handlers = handlers
	return nil
}

// startMessageProcessing starts the message processing loop
func (f *AgentFramework) startMessageProcessing() (<-chan *client.BrokerMessage, error) {
	// Connect to ingress
	msgChan, err := f.handlers.Connect()
	if err != nil {
		return nil, err
	}

	f.baseAgent.LogInfo("%s listening for messages", f.agentType)

	// Start processing goroutine
	ctx := f.baseAgent.Context()
	go func() {
		for {
			select {
			case <-ctx.Done():
				f.baseAgent.LogInfo("Message processor shutting down")
				return
			case msg, ok := <-msgChan:
				if !ok {
					f.baseAgent.LogInfo("Message channel closed")
					return
				}

				// For file_ingester type agents that generate messages,
				// the message comes from FileIngressHandler and should be forwarded
				if f.agentType == "file-ingester" {
					if err := f.processGeneratedMessage(msg); err != nil {
						f.baseAgent.LogError("Failed to process generated message: %v", err)
					}
				} else {
					// For regular processing agents
					if err := f.processMessage(msg); err != nil {
						f.baseAgent.LogError("Failed to process message: %v", err)
					}
				}
			}
		}
	}()

	return msgChan, nil
}

// processMessage handles a single message using the agent's business logic
func (f *AgentFramework) processMessage(msg *client.BrokerMessage) error {
	f.baseAgent.LogDebug("Processing message %s", msg.ID)

	// Call agent-specific processing logic
	resultMsg, err := f.runner.ProcessMessage(msg, f.baseAgent)
	if err != nil {
		return fmt.Errorf("agent processing failed: %w", err)
	}

	// If agent returned a result, send it via egress
	if resultMsg != nil {
		if err := f.handlers.Send(resultMsg); err != nil {
			return fmt.Errorf("failed to send result message: %w", err)
		}
		f.baseAgent.LogInfo("Processed and forwarded message %s", msg.ID)
	} else {
		// For agents like file_writer that handle egress internally, 
		// we still call the egress handler but with the original message
		// The handler decides whether to process it or not
		if err := f.handlers.Send(msg); err != nil {
			return fmt.Errorf("failed to send message: %w", err)
		}
		f.baseAgent.LogInfo("Processed message %s", msg.ID)
	}

	return nil
}

// processGeneratedMessage handles messages generated by ingress handlers (like file_ingester)
func (f *AgentFramework) processGeneratedMessage(msg *client.BrokerMessage) error {
	f.baseAgent.LogDebug("Processing generated message %s", msg.ID)

	// For message generators, call the agent's ProcessMessage for any custom logic
	resultMsg, err := f.runner.ProcessMessage(msg, f.baseAgent)
	if err != nil {
		return fmt.Errorf("agent processing failed: %w", err)
	}

	// Send the result (or original if no transformation) via egress
	msgToSend := resultMsg
	if msgToSend == nil {
		msgToSend = msg // Use original if agent didn't transform
	}

	if err := f.handlers.Send(msgToSend); err != nil {
		return fmt.Errorf("failed to send generated message: %w", err)
	}

	f.baseAgent.LogInfo("Processed and published generated message %s", msg.ID)
	return nil
}

// handleShutdown manages graceful shutdown with signal handling
func (f *AgentFramework) handleShutdown(msgChan <-chan *client.BrokerMessage) error {
	// Setup signal handling (identical across all agents)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	ctx := f.baseAgent.Context()

	// Wait for shutdown signal or context cancellation
	select {
	case sig := <-sigChan:
		f.baseAgent.LogInfo("Received OS signal: %s, stopping gracefully...", sig)
	case <-ctx.Done():
		f.baseAgent.LogInfo("Context cancelled, stopping gracefully...")
	}

	f.baseAgent.LogInfo("%s stopped gracefully", f.agentType)
	return nil
}

// --- CONVENIENCE FUNCTIONS FOR AGENTS ---

// Run is a convenience function for creating and running an agent framework
func Run(runner AgentRunner, agentType string) error {
	framework := NewFramework(runner, agentType)
	return framework.Run()
}