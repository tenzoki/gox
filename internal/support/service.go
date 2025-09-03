package support

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/tenzoki/gox/internal/broker"
	"gopkg.in/yaml.v3"
)

type Service struct {
	port          string
	debug         bool
	listener      net.Listener
	agents        map[string]*AgentRegistration
	agentsMux     sync.RWMutex
	broker        *broker.Info
	brokerMux     sync.RWMutex
	agentTypes    map[string]AgentTypeSpec
	agentTypesMux sync.RWMutex
}

type AgentRegistration struct {
	ID           string                 `json:"agent_id"`
	AgentType    string                 `json:"agent_type"`
	Protocol     string                 `json:"protocol"`
	Address      string                 `json:"address"`
	Port         string                 `json:"port"`
	Codec        string                 `json:"codec"`
	Capabilities []string               `json:"capabilities"`
	RegisteredAt time.Time              `json:"registered_at"`
	LastPing     time.Time              `json:"last_ping"`
	State        string                 `json:"state"` // installed, configured, ready, running, paused, stopped, error
	Config       map[string]interface{} `json:"config"`
	StateHistory []StateChangeEvent     `json:"state_history"`
}

type StateChangeEvent struct {
	FromState string    `json:"from_state"`
	ToState   string    `json:"to_state"`
	Timestamp time.Time `json:"timestamp"`
	Reason    string    `json:"reason,omitempty"`
}

type Request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type Response struct {
	ID     string      `json:"id"`
	Result interface{} `json:"result,omitempty"`
	Error  *Error      `json:"error,omitempty"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type SupportConfig struct {
	Port  string
	Debug bool
}

type AgentTypeSpec struct {
	AgentType    string   `yaml:"agent_type"`
	Binary       string   `yaml:"binary"`
	Operator     string   `yaml:"operator"`
	Capabilities []string `yaml:"capabilities"`
	Description  string   `yaml:"description"`
}

type PoolConfig struct {
	Pool struct {
		AgentTypes []AgentTypeSpec `yaml:"agent_types"`
	} `yaml:"pool"`
}

func NewService(config interface{}) *Service {
	// Extract values from config interface (could be config.SupportConfig)
	port := ":9000"
	debug := false

	if sc, ok := config.(SupportConfig); ok {
		port = sc.Port
		debug = sc.Debug
	} else if sc, ok := config.(struct {
		Port  string
		Debug bool
	}); ok {
		port = sc.Port
		debug = sc.Debug
	}
	service := &Service{
		port:       port,
		debug:      debug,
		agents:     make(map[string]*AgentRegistration),
		agentTypes: make(map[string]AgentTypeSpec),
	}

	// Load agent types from pool.yaml
	if err := service.loadAgentTypes("pool.yaml"); err != nil {
		if debug {
			log.Printf("Warning: failed to load agent types: %v", err)
		}
	}

	return service
}

func (s *Service) Start(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.port)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.port, err)
	}
	s.listener = listener

	if s.debug {
		log.Printf("Support service listening on %s", s.port)
	}

	// Handle context cancellation in a separate goroutine
	go func() {
		<-ctx.Done()
		if s.debug {
			log.Printf("Support service shutting down")
		}
		s.listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if s.debug && ctx.Err() == nil {
				log.Printf("Support service accept error: %v", err)
			}
			continue
		}

		go s.handleConnection(conn)
	}
}

func (s *Service) handleConnection(conn net.Conn) {
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	for {
		var req Request
		if err := decoder.Decode(&req); err != nil {
			if s.debug {
				log.Printf("Support service decode error: %v", err)
			}
			return
		}

		if s.debug {
			log.Printf("Support service received: %s", req.Method)
		}

		resp := s.handleRequest(&req)
		if err := encoder.Encode(resp); err != nil {
			if s.debug {
				log.Printf("Support service encode error: %v", err)
			}
			return
		}
	}
}

func (s *Service) handleRequest(req *Request) *Response {
	switch req.Method {
	case "register_agent":
		return s.handleRegisterAgent(req)
	case "get_broker":
		return s.handleGetBroker(req)
	case "report_state_change":
		return s.handleReportStateChange(req)
	case "get_agent_state":
		return s.handleGetAgentState(req)
	case "wait_for_state":
		return s.handleWaitForState(req)
	case "get_agent_cell_config":
		return s.handleGetAgentCellConfig(req)
	case "get_pipeline_dependencies":
		return s.handleGetPipelineDependencies(req)
	case "get_cell_orchestration_config":
		return s.handleGetCellOrchestrationConfig(req)
	default:
		return &Response{
			ID: req.ID,
			Error: &Error{
				Code:    -32601,
				Message: fmt.Sprintf("Method not found: %s", req.Method),
			},
		}
	}
}

func (s *Service) handleRegisterAgent(req *Request) *Response {
	var params AgentRegistration
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &Response{
			ID:    req.ID,
			Error: &Error{Code: -32602, Message: "Invalid params"},
		}
	}

	// Validate agent type
	if !s.validateAgentType(params.AgentType) {
		return &Response{
			ID:    req.ID,
			Error: &Error{Code: -32603, Message: fmt.Sprintf("Unknown agent type: %s", params.AgentType)},
		}
	}

	params.RegisteredAt = time.Now()
	params.LastPing = time.Now()
	params.State = "installed" // Initial lifecycle state
	if params.Config == nil {
		params.Config = make(map[string]interface{})
	}
	if params.StateHistory == nil {
		params.StateHistory = make([]StateChangeEvent, 0)
	}

	s.agentsMux.Lock()
	s.agents[params.ID] = &params
	s.agentsMux.Unlock()

	if s.debug {
		log.Printf("Registered agent: %s (%s) at %s:%s", params.ID, params.AgentType, params.Address, params.Port)
	}

	return &Response{
		ID:     req.ID,
		Result: "registered",
	}
}

func (s *Service) handleGetBroker(req *Request) *Response {
	s.brokerMux.RLock()
	brokerInfo := s.broker
	s.brokerMux.RUnlock()

	if brokerInfo == nil {
		return &Response{
			ID:    req.ID,
			Error: &Error{Code: -32603, Message: "Broker not available"},
		}
	}

	result := map[string]interface{}{
		"protocol": brokerInfo.Protocol,
		"address":  brokerInfo.Address,
		"port":     brokerInfo.Port,
		"codec":    brokerInfo.Codec,
	}

	return &Response{
		ID:     req.ID,
		Result: result,
	}
}

func (s *Service) SetBrokerAddress(info broker.Info) error {
	s.brokerMux.Lock()
	s.broker = &info
	s.brokerMux.Unlock()

	if s.debug {
		log.Printf("Broker registered: %s://%s", info.Protocol, info.Address)
	}

	return nil
}

// LoadAgentTypesFromFile loads agent types from pool configuration file
func (s *Service) LoadAgentTypesFromFile(filename string) error {
	return s.loadAgentTypes(filename)
}

func (s *Service) loadAgentTypes(filename string) error {
	// Try multiple locations for the pool config file
	locations := []string{
		filename,            // Current directory
		"../" + filename,    // Parent directory
		"../../" + filename, // Grandparent directory (for deeply nested tests)
	}

	var data []byte
	var err error

	for _, location := range locations {
		data, err = os.ReadFile(location)
		if err == nil {
			if s.debug {
				log.Printf("Loaded agent types from: %s", location)
			}
			break
		}
	}

	if err != nil {
		return fmt.Errorf("failed to read pool config from any location %v: %w", locations, err)
	}

	var poolConfig PoolConfig
	if err := yaml.Unmarshal(data, &poolConfig); err != nil {
		return fmt.Errorf("failed to parse pool config: %w", err)
	}

	s.agentTypesMux.Lock()
	defer s.agentTypesMux.Unlock()

	for _, agentType := range poolConfig.Pool.AgentTypes {
		s.agentTypes[agentType.AgentType] = agentType
	}

	if s.debug {
		log.Printf("Loaded %d agent types from %s", len(s.agentTypes), filename)
	}

	return nil
}

func (s *Service) validateAgentType(agentType string) bool {
	s.agentTypesMux.RLock()
	defer s.agentTypesMux.RUnlock()

	_, exists := s.agentTypes[agentType]
	return exists
}

func (s *Service) handleReportStateChange(req *Request) *Response {
	var params struct {
		AgentID string `json:"agent_id"`
		State   string `json:"state"`
		Reason  string `json:"reason,omitempty"`
	}

	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &Response{
			ID:    req.ID,
			Error: &Error{Code: -32602, Message: "Invalid params"},
		}
	}

	s.agentsMux.Lock()
	agent, exists := s.agents[params.AgentID]
	if !exists {
		s.agentsMux.Unlock()
		return &Response{
			ID:    req.ID,
			Error: &Error{Code: -32603, Message: "Agent not found"},
		}
	}

	// Update agent state
	oldState := agent.State
	agent.State = params.State
	agent.LastPing = time.Now()

	// Add to state history
	stateChange := StateChangeEvent{
		FromState: oldState,
		ToState:   params.State,
		Timestamp: time.Now(),
		Reason:    params.Reason,
	}
	agent.StateHistory = append(agent.StateHistory, stateChange)
	s.agentsMux.Unlock()

	if s.debug {
		log.Printf("Agent %s state change: %s → %s", params.AgentID, oldState, params.State)
	}

	return &Response{
		ID:     req.ID,
		Result: "state_updated",
	}
}

func (s *Service) handleGetAgentState(req *Request) *Response {
	var params struct {
		AgentID string `json:"agent_id"`
	}

	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &Response{
			ID:    req.ID,
			Error: &Error{Code: -32602, Message: "Invalid params"},
		}
	}

	s.agentsMux.RLock()
	agent, exists := s.agents[params.AgentID]
	s.agentsMux.RUnlock()

	if !exists {
		return &Response{
			ID:    req.ID,
			Error: &Error{Code: -32603, Message: "Agent not found"},
		}
	}

	result := map[string]interface{}{
		"agent_id":      agent.ID,
		"state":         agent.State,
		"last_ping":     agent.LastPing,
		"state_history": agent.StateHistory,
	}

	return &Response{
		ID:     req.ID,
		Result: result,
	}
}

func (s *Service) handleWaitForState(req *Request) *Response {
	var params struct {
		AgentID     string `json:"agent_id"`
		TargetState string `json:"target_state"`
		TimeoutSec  int    `json:"timeout_seconds"`
	}

	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &Response{
			ID:    req.ID,
			Error: &Error{Code: -32602, Message: "Invalid params"},
		}
	}

	// Check if agent exists and is already in target state
	s.agentsMux.RLock()
	agent, exists := s.agents[params.AgentID]
	if !exists {
		s.agentsMux.RUnlock()
		return &Response{
			ID:    req.ID,
			Error: &Error{Code: -32603, Message: "Agent not found"},
		}
	}

	if agent.State == params.TargetState {
		s.agentsMux.RUnlock()
		return &Response{
			ID:     req.ID,
			Result: "target_state_reached",
		}
	}
	s.agentsMux.RUnlock()

	// Set timeout (default 30 seconds)
	timeout := time.Duration(params.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	// Poll for state change
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()

	for {
		select {
		case <-timeoutTimer.C:
			return &Response{
				ID:    req.ID,
				Error: &Error{Code: -32603, Message: "Timeout waiting for target state"},
			}
		case <-ticker.C:
			s.agentsMux.RLock()
			agent, exists := s.agents[params.AgentID]
			if !exists {
				s.agentsMux.RUnlock()
				return &Response{
					ID:    req.ID,
					Error: &Error{Code: -32603, Message: "Agent disappeared"},
				}
			}
			if agent.State == params.TargetState {
				s.agentsMux.RUnlock()
				return &Response{
					ID:     req.ID,
					Result: "target_state_reached",
				}
			}
			if agent.State == "error" {
				s.agentsMux.RUnlock()
				return &Response{
					ID:    req.ID,
					Error: &Error{Code: -32603, Message: "Agent entered error state"},
				}
			}
			s.agentsMux.RUnlock()
		}
	}
}

func (s *Service) handleGetAgentCellConfig(req *Request) *Response {
	var params struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &Response{
			ID:    req.ID,
			Error: &Error{Code: -32602, Message: "Invalid params"},
		}
	}

	agentMap, err := s.loadCellConfig("cells.yaml")
	if err != nil {
		return &Response{
			ID:    req.ID,
			Error: &Error{Code: -32603, Message: "Failed to load cell configs: " + err.Error()},
		}
	}
	agent, ok := agentMap[params.AgentID]
	if !ok {
		return &Response{
			ID:    req.ID,
			Error: &Error{Code: -32604, Message: fmt.Sprintf("Agent config not found: %s", params.AgentID)},
		}
	}

	result := map[string]interface{}{
		"agent_id":     agent.ID,
		"agent_type":   agent.AgentType,
		"ingress":      agent.Ingress,
		"egress":       agent.Egress,
		"dependencies": agent.Dependencies,
		"config":       agent.Config,
	}
	return &Response{
		ID:     req.ID,
		Result: result,
	}
}

// For loading cells.yaml and looking up agent cell configs
type CellsFile struct {
	Cell struct {
		ID            string                `yaml:"id"`
		Description   string                `yaml:"description"`
		Debug         bool                  `yaml:"debug"`
		Orchestration OrchestrationSettings `yaml:"orchestration,omitempty"`
		Agents        []AgentCellConfig     `yaml:"agents"`
	} `yaml:"cell"`
}

type OrchestrationSettings struct {
	StartupTimeout      string `yaml:"startup_timeout,omitempty"`
	ShutdownTimeout     string `yaml:"shutdown_timeout,omitempty"`
	MaxRetries          int    `yaml:"max_retries,omitempty"`
	RetryDelay          string `yaml:"retry_delay,omitempty"`
	HealthCheckInterval string `yaml:"health_check_interval,omitempty"`
}

type AgentCellConfig struct {
	ID           string                 `yaml:"id"`
	AgentType    string                 `yaml:"agent_type"`
	Ingress      string                 `yaml:"ingress"`
	Egress       string                 `yaml:"egress"`
	Dependencies []string               `yaml:"dependencies,omitempty"`
	Config       map[string]interface{} `yaml:"config"`
}

// Loads cells.yaml and returns a map[agent id]AgentCellConfig
func (s *Service) loadCellConfig(filename string) (map[string]AgentCellConfig, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	dec := yaml.NewDecoder(f)
	agentMap := make(map[string]AgentCellConfig)
	for {
		var doc CellsFile
		err := dec.Decode(&doc)
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}
		for _, ac := range doc.Cell.Agents {
			agentMap[ac.ID] = ac
		}
	}
	return agentMap, nil
}

// handleGetPipelineDependencies returns dependency information for all agents in a cell
func (s *Service) handleGetPipelineDependencies(req *Request) *Response {
	var params struct {
		CellID string `json:"cell_id,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &Response{
			ID:    req.ID,
			Error: &Error{Code: -32602, Message: "Invalid params"},
		}
	}

	agentMap, err := s.loadCellConfig("cells.yaml")
	if err != nil {
		return &Response{
			ID:    req.ID,
			Error: &Error{Code: -32603, Message: "Failed to load cell configs: " + err.Error()},
		}
	}

	// Build dependency list for orchestrator
	dependencies := make([]map[string]interface{}, 0)
	for agentID, agent := range agentMap {
		dep := map[string]interface{}{
			"agent_id":     agentID,
			"agent_type":   agent.AgentType,
			"dependencies": agent.Dependencies,
		}
		dependencies = append(dependencies, dep)
	}

	result := map[string]interface{}{
		"dependencies": dependencies,
	}

	return &Response{
		ID:     req.ID,
		Result: result,
	}
}

// handleGetCellOrchestrationConfig returns orchestration settings for a cell
func (s *Service) handleGetCellOrchestrationConfig(req *Request) *Response {
	var params struct {
		CellID string `json:"cell_id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &Response{
			ID:    req.ID,
			Error: &Error{Code: -32602, Message: "Invalid params"},
		}
	}

	// Load cell configurations
	cellConfigs, err := s.loadCellConfigs("cells.yaml")
	if err != nil {
		return &Response{
			ID:    req.ID,
			Error: &Error{Code: -32603, Message: "Failed to load cell configs: " + err.Error()},
		}
	}

	// Find the specified cell
	for _, cellConfig := range cellConfigs {
		if cellConfig.Cell.ID == params.CellID {
			result := map[string]interface{}{
				"cell_id":       cellConfig.Cell.ID,
				"description":   cellConfig.Cell.Description,
				"debug":         cellConfig.Cell.Debug,
				"orchestration": cellConfig.Cell.Orchestration,
			}
			return &Response{
				ID:     req.ID,
				Result: result,
			}
		}
	}

	return &Response{
		ID:    req.ID,
		Error: &Error{Code: -32604, Message: fmt.Sprintf("Cell not found: %s", params.CellID)},
	}
}

// loadCellConfigs loads all cell configurations from file
func (s *Service) loadCellConfigs(filename string) ([]CellsFile, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	dec := yaml.NewDecoder(f)
	cellConfigs := make([]CellsFile, 0)

	for {
		var doc CellsFile
		err := dec.Decode(&doc)
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}
		cellConfigs = append(cellConfigs, doc)
	}

	return cellConfigs, nil
}
