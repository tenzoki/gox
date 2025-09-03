package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/tenzoki/gox/internal/envelope"
)

// AdapterAgent provides data transformation services to other agents
type AdapterAgent struct {
	*BaseAgent
	adapters map[string]AdapterFunc
	running  bool
}

// AdapterFunc defines the signature for transformation functions
type AdapterFunc func(input []byte) ([]byte, error)

// AdapterRequest represents a transformation request
type AdapterRequest struct {
	RequestID    string `json:"request_id"`
	SourceFormat string `json:"source_format"`
	TargetFormat string `json:"target_format"`
	Data         []byte `json:"data"`
	ReplyTo      string `json:"reply_to,omitempty"`
}

// AdapterResponse represents a transformation response
type AdapterResponse struct {
	RequestID       string `json:"request_id"`
	Success         bool   `json:"success"`
	TransformedData []byte `json:"transformed_data,omitempty"`
	Error           string `json:"error,omitempty"`
}

// NewAdapterAgent creates a new adapter agent with common transformations
func NewAdapterAgent(config AgentConfig) (*AdapterAgent, error) {
	baseAgent, err := NewBaseAgent(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create base agent: %w", err)
	}

	adapter := &AdapterAgent{
		BaseAgent: baseAgent,
		adapters:  make(map[string]AdapterFunc),
	}

	// Register common adapters
	adapter.registerCommonAdapters()

	if config.Debug {
		log.Printf("Adapter agent %s created with %d adapters", config.ID, len(adapter.adapters))
	}

	return adapter, nil
}

// registerCommonAdapters registers built-in transformation functions
func (a *AdapterAgent) registerCommonAdapters() {
	// Text transformations
	a.RegisterAdapter("text-upper", func(input []byte) ([]byte, error) {
		return []byte(strings.ToUpper(string(input))), nil
	})

	a.RegisterAdapter("text-lower", func(input []byte) ([]byte, error) {
		return []byte(strings.ToLower(string(input))), nil
	})

	a.RegisterAdapter("text-trim", func(input []byte) ([]byte, error) {
		return []byte(strings.TrimSpace(string(input))), nil
	})

	// JSON transformations
	a.RegisterAdapter("json-pretty", func(input []byte) ([]byte, error) {
		var obj interface{}
		if err := json.Unmarshal(input, &obj); err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
		return json.MarshalIndent(obj, "", "  ")
	})

	a.RegisterAdapter("json-compact", func(input []byte) ([]byte, error) {
		var obj interface{}
		if err := json.Unmarshal(input, &obj); err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
		return json.Marshal(obj)
	})

	// Format conversions
	a.RegisterAdapter("csv-to-json", func(input []byte) ([]byte, error) {
		return a.csvToJSON(input)
	})

	a.RegisterAdapter("json-to-csv", func(input []byte) ([]byte, error) {
		return a.jsonToCSV(input)
	})

	// Base64 encoding/decoding
	a.RegisterAdapter("base64-encode", func(input []byte) ([]byte, error) {
		encoded := make([]byte, len(input)*4/3+4) // rough estimate
		n := copy(encoded, []byte(fmt.Sprintf("%x", input)))
		return encoded[:n], nil
	})

	a.RegisterAdapter("base64-decode", func(input []byte) ([]byte, error) {
		// Simple hex decode for demo
		decoded := make([]byte, len(input)/2)
		_, err := fmt.Sscanf(string(input), "%x", &decoded)
		return decoded, err
	})
}

// RegisterAdapter registers a new transformation function
func (a *AdapterAgent) RegisterAdapter(name string, fn AdapterFunc) {
	a.adapters[name] = fn
	a.LogInfo("Registered adapter: %s", name)
}

// GetAvailableAdapters returns a list of available adapters
func (a *AdapterAgent) GetAvailableAdapters() []string {
	adapters := make([]string, 0, len(a.adapters))
	for name := range a.adapters {
		adapters = append(adapters, name)
	}
	return adapters
}

// Start begins processing adapter requests
func (a *AdapterAgent) Start(ctx context.Context) error {
	a.LogInfo("Starting adapter agent")

	// Transition to ready state
	if err := a.Lifecycle.SetState(StateReady, "adapter service initialized"); err != nil {
		return fmt.Errorf("failed to set ready state: %w", err)
	}

	// Get ingress configuration
	ingress := a.GetIngress()
	if ingress == "" {
		return fmt.Errorf("no ingress configuration specified")
	}

	// Subscribe to requests
	var requestChan <-chan *envelope.Envelope
	var err error

	if strings.HasPrefix(ingress, "sub:") {
		topic := strings.TrimPrefix(ingress, "sub:")
		requestChan, err = a.BrokerClient.SubscribeEnvelopes(topic)
		if err != nil {
			return fmt.Errorf("failed to subscribe to topic %s: %w", topic, err)
		}
		a.LogInfo("Subscribed to topic: %s", topic)
	} else if strings.HasPrefix(ingress, "pipe:") {
		// For pipes, we'll use a polling approach
		pipeName := strings.TrimPrefix(ingress, "pipe:")
		requestChan = a.createPipeListener(ctx, pipeName)
		a.LogInfo("Listening on pipe: %s", pipeName)
	} else {
		return fmt.Errorf("unsupported ingress type: %s", ingress)
	}

	// Transition to running state
	if err := a.Lifecycle.SetState(StateRunning, "processing requests"); err != nil {
		a.LogError("Failed to set running state: %v", err)
	}

	a.running = true

	// Process requests
	for {
		select {
		case <-ctx.Done():
			a.LogInfo("Stopping adapter agent")
			a.running = false
			if err := a.Lifecycle.SetState(StateStopped, "context cancelled"); err != nil {
				a.LogError("Failed to set stopped state: %v", err)
			}
			return nil

		case env := <-requestChan:
			if env != nil {
				go a.handleRequest(env)
			}
		}
	}
}

// createPipeListener creates a channel that polls a pipe for messages
func (a *AdapterAgent) createPipeListener(ctx context.Context, pipeName string) <-chan *envelope.Envelope {
	envChan := make(chan *envelope.Envelope, 10)

	go func() {
		defer close(envChan)

		for {
			select {
			case <-ctx.Done():
				return
			default:
				// Poll pipe with short timeout
				result, err := a.BrokerClient.ReceivePipe(pipeName, 1000)
				if err != nil {
					// Timeout or other error, continue polling
					time.Sleep(100 * time.Millisecond)
					continue
				}

				// Check if result is an envelope
				if env, ok := result.(*envelope.Envelope); ok {
					select {
					case envChan <- env:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return envChan
}

// handleRequest processes a single adapter request
func (a *AdapterAgent) handleRequest(env *envelope.Envelope) {
	a.LogDebug("Processing adapter request: %s", env.MessageType)

	// Parse adapter request from envelope payload
	var request AdapterRequest
	if err := env.UnmarshalPayload(&request); err != nil {
		a.LogError("Failed to unmarshal adapter request: %v", err)
		a.sendErrorResponse(env, fmt.Sprintf("Invalid request format: %v", err))
		return
	}

	// Generate adapter key from source and target formats
	adapterKey := fmt.Sprintf("%s-to-%s", request.SourceFormat, request.TargetFormat)

	// Find appropriate adapter
	adapterFunc, exists := a.adapters[adapterKey]
	if !exists {
		a.LogError("No adapter found for: %s", adapterKey)
		a.sendErrorResponse(env, fmt.Sprintf("Adapter not found: %s", adapterKey))
		return
	}

	// Perform transformation
	transformedData, err := adapterFunc(request.Data)
	if err != nil {
		a.LogError("Transformation failed: %v", err)
		a.sendErrorResponse(env, fmt.Sprintf("Transformation error: %v", err))
		return
	}

	// Create response
	response := AdapterResponse{
		RequestID:       request.RequestID,
		Success:         true,
		TransformedData: transformedData,
	}

	// Send response back
	a.sendResponse(env, response)
}

// sendResponse sends a successful transformation response
func (a *AdapterAgent) sendResponse(originalEnv *envelope.Envelope, response AdapterResponse) {
	// Create reply envelope
	replyEnv, err := envelope.NewReplyEnvelope(originalEnv, a.ID, response)
	if err != nil {
		a.LogError("Failed to create reply envelope: %v", err)
		return
	}

	replyEnv.MessageType = "adapter.response"
	a.sendReply(replyEnv)
}

// sendErrorResponse sends an error response
func (a *AdapterAgent) sendErrorResponse(originalEnv *envelope.Envelope, errorMsg string) {
	response := AdapterResponse{
		Success: false,
		Error:   errorMsg,
	}

	replyEnv, err := envelope.NewReplyEnvelope(originalEnv, a.ID, response)
	if err != nil {
		a.LogError("Failed to create error reply envelope: %v", err)
		return
	}

	replyEnv.MessageType = "adapter.error"
	a.sendReply(replyEnv)
}

// sendReply sends a reply envelope via the configured egress
func (a *AdapterAgent) sendReply(replyEnv *envelope.Envelope) {
	egress := a.GetEgress()
	if egress == "" {
		a.LogError("No egress configuration for reply")
		return
	}

	if strings.HasPrefix(egress, "pub:") {
		topic := strings.TrimPrefix(egress, "pub:")
		if err := a.BrokerClient.PublishEnvelope(topic, replyEnv); err != nil {
			a.LogError("Failed to publish reply: %v", err)
		}
	} else if strings.HasPrefix(egress, "pipe:") {
		pipeName := strings.TrimPrefix(egress, "pipe:")
		if err := a.BrokerClient.SendPipeEnvelope(pipeName, replyEnv); err != nil {
			a.LogError("Failed to send reply via pipe: %v", err)
		}
	} else {
		a.LogError("Unsupported egress type: %s", egress)
	}
}

// Helper functions for format conversions

// csvToJSON converts CSV data to JSON array
func (a *AdapterAgent) csvToJSON(csvData []byte) ([]byte, error) {
	lines := strings.Split(string(csvData), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("CSV must have at least header and one data row")
	}

	// Parse header
	headers := strings.Split(lines[0], ",")
	for i := range headers {
		headers[i] = strings.TrimSpace(headers[i])
	}

	// Parse data rows
	var records []map[string]string
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		fields := strings.Split(line, ",")
		if len(fields) != len(headers) {
			continue // Skip malformed rows
		}

		record := make(map[string]string)
		for j, field := range fields {
			record[headers[j]] = strings.TrimSpace(field)
		}
		records = append(records, record)
	}

	return json.Marshal(records)
}

// jsonToCSV converts JSON array to CSV format
func (a *AdapterAgent) jsonToCSV(jsonData []byte) ([]byte, error) {
	var records []map[string]interface{}
	if err := json.Unmarshal(jsonData, &records); err != nil {
		return nil, fmt.Errorf("invalid JSON array: %w", err)
	}

	if len(records) == 0 {
		return []byte(""), nil
	}

	// Extract headers from first record
	var headers []string
	for key := range records[0] {
		headers = append(headers, key)
	}

	// Build CSV
	var csvLines []string
	csvLines = append(csvLines, strings.Join(headers, ","))

	for _, record := range records {
		var fields []string
		for _, header := range headers {
			value := ""
			if val, exists := record[header]; exists {
				value = fmt.Sprintf("%v", val)
			}
			fields = append(fields, value)
		}
		csvLines = append(csvLines, strings.Join(fields, ","))
	}

	return []byte(strings.Join(csvLines, "\n")), nil
}

// IsRunning returns whether the adapter agent is currently running
func (a *AdapterAgent) IsRunning() bool {
	return a.running
}
