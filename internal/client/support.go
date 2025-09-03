package client

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
)

type SupportClient struct {
	address string
	debug   bool
	conn    net.Conn
	encoder *json.Encoder
	decoder *json.Decoder
	mux     sync.Mutex
	reqID   int64
}

type SupportRequest struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type SupportResponse struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *SupportError   `json:"error,omitempty"`
}

type SupportError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type AgentRegistration struct {
	ID           string   `json:"agent_id"`
	AgentType    string   `json:"agent_type"`
	Protocol     string   `json:"protocol"`
	Address      string   `json:"address"`
	Port         string   `json:"port"`
	Codec        string   `json:"codec"`
	Capabilities []string `json:"capabilities"`
}

type AgentCellConfig struct {
	ID           string                 `json:"id"`
	AgentType    string                 `json:"agent_type"`
	Ingress      string                 `json:"ingress"`
	Egress       string                 `json:"egress"`
	Dependencies []string               `json:"dependencies"`
	Config       map[string]interface{} `json:"config"`
}

type BrokerInfo struct {
	Protocol string `json:"protocol"`
	Address  string `json:"address"`
	Port     string `json:"port"`
	Codec    string `json:"codec"`
}

func NewSupportClient(address string, debug bool) *SupportClient {
	return &SupportClient{
		address: address,
		debug:   debug,
	}
}

func (c *SupportClient) Connect() error {
	c.mux.Lock()
	defer c.mux.Unlock()

	if c.conn != nil {
		return nil // Already connected
	}

	conn, err := net.Dial("tcp", c.address)
	if err != nil {
		return fmt.Errorf("failed to connect to support service at %s: %w", c.address, err)
	}

	c.conn = conn
	c.encoder = json.NewEncoder(conn)
	c.decoder = json.NewDecoder(conn)

	if c.debug {
		log.Printf("Connected to support service at %s", c.address)
	}

	return nil
}

func (c *SupportClient) Disconnect() error {
	c.mux.Lock()
	defer c.mux.Unlock()

	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		c.encoder = nil
		c.decoder = nil
		return err
	}
	return nil
}

func (c *SupportClient) call(method string, params interface{}) (json.RawMessage, error) {
	c.mux.Lock()
	defer c.mux.Unlock()

	if c.conn == nil {
		return nil, fmt.Errorf("not connected to support service")
	}

	c.reqID++
	reqID := fmt.Sprintf("req_%d", c.reqID)

	var paramsBytes json.RawMessage
	if params != nil {
		var err error
		paramsBytes, err = json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal params: %w", err)
		}
	}

	req := SupportRequest{
		ID:     reqID,
		Method: method,
		Params: paramsBytes,
	}

	if err := c.encoder.Encode(req); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	var resp SupportResponse
	if err := c.decoder.Decode(&resp); err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("support service error: %s (code: %d)", resp.Error.Message, resp.Error.Code)
	}

	return resp.Result, nil
}

func (c *SupportClient) RegisterAgent(registration AgentRegistration) error {
	_, err := c.call("register_agent", registration)
	return err
}

func (c *SupportClient) GetBroker() (*BrokerInfo, error) {
	result, err := c.call("get_broker", nil)
	if err != nil {
		return nil, err
	}

	var broker BrokerInfo
	if err := json.Unmarshal(result, &broker); err != nil {
		return nil, fmt.Errorf("failed to unmarshal broker info: %w", err)
	}

	return &broker, nil
}

func (c *SupportClient) GetAgentCellConfig(agentID string) (*AgentCellConfig, error) {
	params := map[string]interface{}{
		"agent_id": agentID,
	}

	result, err := c.call("get_agent_cell_config", params)
	if err != nil {
		return nil, err
	}

	var cellConfig AgentCellConfig
	if err := json.Unmarshal(result, &cellConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal agent cell config: %w", err)
	}

	return &cellConfig, nil
}

func (c *SupportClient) ReportStateChange(agentID, state string) error {
	params := map[string]interface{}{
		"agent_id": agentID,
		"state":    state,
	}

	_, err := c.call("report_state_change", params)
	return err
}

func (c *SupportClient) GetPipelineDependencies(cellID string) ([]DependencyInfo, error) {
	params := map[string]interface{}{}
	if cellID != "" {
		params["cell_id"] = cellID
	}

	result, err := c.call("get_pipeline_dependencies", params)
	if err != nil {
		return nil, err
	}

	var response struct {
		Dependencies []DependencyInfo `json:"dependencies"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal dependencies: %w", err)
	}

	return response.Dependencies, nil
}

func (c *SupportClient) GetCellOrchestrationConfig(cellID string) (*CellOrchestrationConfig, error) {
	params := map[string]interface{}{
		"cell_id": cellID,
	}

	result, err := c.call("get_cell_orchestration_config", params)
	if err != nil {
		return nil, err
	}

	var config CellOrchestrationConfig
	if err := json.Unmarshal(result, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal orchestration config: %w", err)
	}

	return &config, nil
}

type DependencyInfo struct {
	AgentID      string   `json:"agent_id"`
	AgentType    string   `json:"agent_type"`
	Dependencies []string `json:"dependencies"`
}

type CellOrchestrationConfig struct {
	CellID        string                `json:"cell_id"`
	Description   string                `json:"description"`
	Debug         bool                  `json:"debug"`
	Orchestration OrchestrationSettings `json:"orchestration"`
}

type OrchestrationSettings struct {
	StartupTimeout      string `json:"startup_timeout,omitempty"`
	ShutdownTimeout     string `json:"shutdown_timeout,omitempty"`
	MaxRetries          int    `json:"max_retries,omitempty"`
	RetryDelay          string `json:"retry_delay,omitempty"`
	HealthCheckInterval string `json:"health_check_interval,omitempty"`
}
