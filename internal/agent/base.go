package agent

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/tenzoki/gox/internal/client"
)

// BaseAgent provides common functionality for all v3 agents
type BaseAgent struct {
	ID            string
	AgentType     string
	Debug         bool
	SupportClient *client.SupportClient
	BrokerClient  *client.BrokerClient
	Config        map[string]interface{}
	ctx           context.Context
	cancel        context.CancelFunc
	Lifecycle     *LifecycleManager
}

// AgentConfig holds the configuration for an agent
type AgentConfig struct {
	ID             string
	AgentType      string // Agent type (file-ingester, text-transformer, etc.)
	Debug          bool
	SupportAddress string
	Capabilities   []string
	RebootTimeout  time.Duration
}

// NewBaseAgent creates a new base agent with support and broker clients
func NewBaseAgent(config AgentConfig) (*BaseAgent, error) {
	supportClient := client.NewSupportClient(config.SupportAddress, config.Debug)
	if err := supportClient.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to support service: %w", err)
	}

	// Get broker info from support service
	brokerInfo, err := supportClient.GetBroker()
	if err != nil {
		return nil, fmt.Errorf("failed to get broker info: %w", err)
	}

	brokerAddress := brokerInfo.Address + brokerInfo.Port
	brokerClient := client.NewBrokerClient(brokerAddress, config.ID, config.Debug)
	if err := brokerClient.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to broker: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create lifecycle manager with support notification callback
	supportNotifyCallback := func(agentID, state string) error {
		// This will be called when agent state changes
		return supportClient.ReportStateChange(agentID, state)
	}
	lifecycle := NewLifecycleManager(config.ID, supportNotifyCallback)

	agent := &BaseAgent{
		ID:            config.ID,
		AgentType:     config.AgentType,
		Debug:         config.Debug,
		SupportClient: supportClient,
		BrokerClient:  brokerClient,
		Config:        make(map[string]interface{}),
		ctx:           ctx,
		cancel:        cancel,
		Lifecycle:     lifecycle,
	}

	// Register with support service
	registration := client.AgentRegistration{
		ID:           config.ID,
		AgentType:    config.AgentType,
		Protocol:     "tcp",
		Address:      "localhost",
		Port:         "0",
		Codec:        "json",
		Capabilities: config.Capabilities,
	}

	if err := supportClient.RegisterAgent(registration); err != nil {
		return nil, fmt.Errorf("failed to register agent: %w", err)
	}

	// Fetch cell-specific configuration
	cellConfig, err := supportClient.GetAgentCellConfig(config.ID)
	if err != nil {
		agent.LogDebug("No cell-specific config available: %v", err)
		// Not a fatal error - agent can work with default config
	} else {
		// Apply cell configuration to agent
		if cellConfig.Config != nil {
			for key, value := range cellConfig.Config {
				agent.Config[key] = value
			}
		}

		// Store ingress/egress information
		agent.Config["ingress"] = cellConfig.Ingress
		agent.Config["egress"] = cellConfig.Egress

		if config.Debug {
			log.Printf("Agent %s loaded cell config: ingress=%s, egress=%s",
				config.ID, cellConfig.Ingress, cellConfig.Egress)
		}

		// Transition to configured state
		if err := agent.Lifecycle.SetState(StateConfigured, "cell configuration loaded"); err != nil {
			agent.LogError("Failed to transition to configured state: %v", err)
		}
	}

	if config.Debug {
		log.Printf("Agent %s initialized successfully", config.ID)
	}

	return agent, nil
}

// Stop gracefully shuts down the agent
func (a *BaseAgent) Stop() error {
	if a.Debug {
		log.Printf("Agent %s shutting down", a.ID)
	}

	a.cancel()

	if a.BrokerClient != nil {
		a.BrokerClient.Disconnect()
	}

	if a.SupportClient != nil {
		a.SupportClient.Disconnect()
	}

	return nil
}

// GetConfigString retrieves a string configuration value
func (a *BaseAgent) GetConfigString(key, defaultValue string) string {
	if value, exists := a.Config[key]; exists {
		if str, ok := value.(string); ok {
			return str
		}
	}
	return defaultValue
}

// GetConfigBool retrieves a boolean configuration value
func (a *BaseAgent) GetConfigBool(key string, defaultValue bool) bool {
	if value, exists := a.Config[key]; exists {
		if b, ok := value.(bool); ok {
			return b
		}
	}
	return defaultValue
}

// GetIngress returns the agent's ingress configuration
func (a *BaseAgent) GetIngress() string {
	return a.GetConfigString("ingress", "")
}

// GetEgress returns the agent's egress configuration
func (a *BaseAgent) GetEgress() string {
	return a.GetConfigString("egress", "")
}

// Log helper functions
func (a *BaseAgent) LogInfo(format string, args ...interface{}) {
	log.Printf("Agent %s: "+format, append([]interface{}{a.ID}, args...)...)
}

func (a *BaseAgent) LogDebug(format string, args ...interface{}) {
	if a.Debug {
		log.Printf("Agent %s [DEBUG]: "+format, append([]interface{}{a.ID}, args...)...)
	}
}

func (a *BaseAgent) LogError(format string, args ...interface{}) {
	log.Printf("Agent %s [ERROR]: "+format, append([]interface{}{a.ID}, args...)...)
}

// Context returns the agent's context for cancellation
func (a *BaseAgent) Context() context.Context {
	return a.ctx
}

// GetAgentID resolves agent ID from CLI args, environment, or auto-generates
func GetAgentID(agentType string) string {
	// Priority 1: CLI argument --id=agent-id
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "--id=") {
			return strings.TrimPrefix(arg, "--id=")
		}
	}

	// Priority 2: Environment variable
	if id := os.Getenv("GOX_AGENT_ID"); id != "" {
		return id
	}

	// Priority 3: Auto-generate unique ID
	hostname, _ := os.Hostname()
	pid := os.Getpid()
	return fmt.Sprintf("%s-%s-%d", agentType, hostname, pid)
}

// GetAgentType resolves agent type from CLI args or environment
func GetAgentType(defaultType string) string {
	// Priority 1: CLI argument --type=agent-type
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "--type=") {
			return strings.TrimPrefix(arg, "--type=")
		}
	}

	// Priority 2: Environment variable
	if agentType := os.Getenv("GOX_AGENT_TYPE"); agentType != "" {
		return agentType
	}

	// Priority 3: Default
	return defaultType
}

// Helper to get configuration from environment
func GetEnvConfig(key, defaultValue string) string {
	if value := os.Getenv("GOX_" + key); value != "" {
		return value
	}
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// GetDebugFromEnv checks for debug flag
func GetDebugFromEnv() bool {
	if os.Getenv("GOX_DEBUG") == "true" {
		return true
	}
	for _, arg := range os.Args {
		if arg == "--debug" {
			return true
		}
	}
	return false
}
