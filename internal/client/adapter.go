package client

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/tenzoki/gox/internal/envelope"
)

// AdapterClient provides a simple interface for using adapter services
type AdapterClient struct {
	brokerClient *BrokerClient
	agentID      string
	debug        bool
}

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

// NewAdapterClient creates a new adapter client
func NewAdapterClient(brokerClient *BrokerClient, agentID string, debug bool) *AdapterClient {
	return &AdapterClient{
		brokerClient: brokerClient,
		agentID:      agentID,
		debug:        debug,
	}
}

// Transform requests data transformation from an adapter service
func (ac *AdapterClient) Transform(sourceFormat, targetFormat string, data []byte, adapterTopic string) ([]byte, error) {
	// Generate unique request ID
	requestID := uuid.New().String()

	// Create adapter request
	request := AdapterRequest{
		RequestID:    requestID,
		SourceFormat: sourceFormat,
		TargetFormat: targetFormat,
		Data:         data,
		ReplyTo:      ac.agentID,
	}

	// Create envelope for the request
	env, err := envelope.NewEnvelope(ac.agentID, fmt.Sprintf("pub:%s", adapterTopic), "adapter.request", request)
	if err != nil {
		return nil, fmt.Errorf("failed to create request envelope: %w", err)
	}

	// Set up response listener
	responsesTopic := fmt.Sprintf("transform-responses-%s", ac.agentID)
	responseChan, err := ac.brokerClient.SubscribeEnvelopes(responsesTopic)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to responses: %w", err)
	}

	// Send request
	if err := ac.brokerClient.PublishEnvelope(adapterTopic, env); err != nil {
		return nil, fmt.Errorf("failed to send adapter request: %w", err)
	}

	// Wait for response with timeout
	timeout := time.After(30 * time.Second)
	for {
		select {
		case <-timeout:
			return nil, fmt.Errorf("timeout waiting for adapter response")

		case responseEnv := <-responseChan:
			if responseEnv == nil {
				continue
			}

			// Check if this is our response
			var response AdapterResponse
			if err := responseEnv.UnmarshalPayload(&response); err != nil {
				continue // Not a valid adapter response
			}

			if response.RequestID != requestID {
				continue // Not our response
			}

			if !response.Success {
				return nil, fmt.Errorf("adapter transformation failed: %s", response.Error)
			}

			return response.TransformedData, nil
		}
	}
}

// TransformAsync requests data transformation asynchronously
func (ac *AdapterClient) TransformAsync(sourceFormat, targetFormat string, data []byte, adapterTopic string, callback func([]byte, error)) error {
	go func() {
		result, err := ac.Transform(sourceFormat, targetFormat, data, adapterTopic)
		callback(result, err)
	}()
	return nil
}

// GetAvailableTransformations returns common transformation patterns
func (ac *AdapterClient) GetAvailableTransformations() map[string]string {
	return map[string]string{
		"text-upper":    "Convert text to uppercase",
		"text-lower":    "Convert text to lowercase",
		"text-trim":     "Trim whitespace from text",
		"json-pretty":   "Pretty-format JSON",
		"json-compact":  "Compact JSON formatting",
		"csv-to-json":   "Convert CSV to JSON array",
		"json-to-csv":   "Convert JSON array to CSV",
		"base64-encode": "Encode data as base64",
		"base64-decode": "Decode base64 data",
	}
}

// Example usage documentation
const AdapterUsageExample = `
// Example usage of AdapterClient:

brokerClient := NewBrokerClient("localhost:9001", "my-agent", true)
err := brokerClient.Connect()
if err != nil {
    log.Fatal(err)
}

adapterClient := NewAdapterClient(brokerClient, "my-agent", true)

// Transform JSON to pretty format
jsonData := []byte('{"name":"John","age":30}')
prettyJSON, err := adapterClient.Transform("json", "json-pretty", jsonData, "transform-requests")
if err != nil {
    log.Printf("Transformation failed: %v", err) 
} else {
    fmt.Printf("Pretty JSON: %s\n", prettyJSON)
}

// Transform CSV to JSON
csvData := []byte("name,age\nJohn,30\nJane,25")
jsonResult, err := adapterClient.Transform("csv", "json", csvData, "transform-requests")
if err != nil {
    log.Printf("CSV to JSON failed: %v", err)
} else {
    fmt.Printf("JSON result: %s\n", jsonResult)
}
`
