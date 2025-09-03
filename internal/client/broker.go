package client

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/tenzoki/gox/internal/envelope"
)

type BrokerClient struct {
	address       string
	agentID       string
	debug         bool
	conn          net.Conn
	encoder       *json.Encoder
	decoder       *json.Decoder
	mux           sync.Mutex
	reqID         int64
	listeners     map[string]chan *BrokerMessage
	envListeners  map[string]chan *envelope.Envelope
	listenersMux  sync.RWMutex
	responseChans map[string]chan *BrokerResponse
	responseChMux sync.RWMutex
}

type BrokerRequest struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type BrokerResponse struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *BrokerError    `json:"error,omitempty"`
}

type BrokerError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type BrokerMessage struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Target    string                 `json:"target"`
	Payload   interface{}            `json:"payload"`
	Meta      map[string]interface{} `json:"meta"`
	Timestamp time.Time              `json:"timestamp"`
}

func NewBrokerClient(address, agentID string, debug bool) *BrokerClient {
	return &BrokerClient{
		address:       address,
		agentID:       agentID,
		debug:         debug,
		listeners:     make(map[string]chan *BrokerMessage),
		envListeners:  make(map[string]chan *envelope.Envelope),
		responseChans: make(map[string]chan *BrokerResponse),
	}
}

func (c *BrokerClient) Connect() error {
	c.mux.Lock()

	if c.conn != nil {
		c.mux.Unlock()
		return nil // Already connected
	}

	conn, err := net.Dial("tcp", c.address)
	if err != nil {
		c.mux.Unlock()
		return fmt.Errorf("failed to connect to broker at %s: %w", c.address, err)
	}

	c.conn = conn
	c.encoder = json.NewEncoder(conn)
	c.decoder = json.NewDecoder(conn)

	// Start message listener goroutine before making any calls
	go c.messageListener()

	// Release mutex before making calls to avoid deadlock
	c.mux.Unlock()

	// Give listener time to start
	time.Sleep(10 * time.Millisecond)

	// Send initial connect request
	params := map[string]interface{}{
		"agent_id": c.agentID,
	}
	if _, err := c.call("connect", params); err != nil {
		// Re-acquire mutex to clean up connection
		c.mux.Lock()
		conn.Close()
		c.conn = nil
		c.encoder = nil
		c.decoder = nil
		c.mux.Unlock()
		return fmt.Errorf("failed to register with broker: %w", err)
	}

	if c.debug {
		log.Printf("Connected to broker at %s", c.address)
	}

	return nil
}

func (c *BrokerClient) Disconnect() error {
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

func (c *BrokerClient) call(method string, params interface{}) (json.RawMessage, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("not connected to broker")
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

	req := BrokerRequest{
		ID:     reqID,
		Method: method,
		Params: paramsBytes,
	}

	// Create response channel for this request
	respChan := make(chan *BrokerResponse, 1)
	c.responseChMux.Lock()
	c.responseChans[reqID] = respChan
	c.responseChMux.Unlock()

	// Send request
	if err := c.encoder.Encode(req); err != nil {
		c.responseChMux.Lock()
		delete(c.responseChans, reqID)
		c.responseChMux.Unlock()
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Wait for response
	select {
	case resp := <-respChan:
		c.responseChMux.Lock()
		delete(c.responseChans, reqID)
		c.responseChMux.Unlock()

		if resp == nil {
			return nil, fmt.Errorf("response channel closed")
		}

		if resp.Error != nil {
			return nil, fmt.Errorf("broker error: %s (code: %d)", resp.Error.Message, resp.Error.Code)
		}

		return resp.Result, nil
	case <-time.After(30 * time.Second):
		c.responseChMux.Lock()
		delete(c.responseChans, reqID)
		c.responseChMux.Unlock()
		return nil, fmt.Errorf("request timeout")
	}
}

func (c *BrokerClient) messageListener() {
	defer func() {
		if r := recover(); r != nil {
			if c.debug {
				log.Printf("Broker message listener panic: %v", r)
			}
		}
	}()

	for {
		// Note: decoder is set once during Connect() and only cleared during Disconnect()
		// Since messageListener is only running between Connect() and Disconnect(),
		// it's safe to read decoder without mutex
		decoder := c.decoder

		if decoder == nil {
			return // Connection closed
		}

		// Read raw JSON to determine message type
		var rawMsg json.RawMessage
		if err := decoder.Decode(&rawMsg); err != nil {
			if c.debug {
				log.Printf("Broker message decode error: %v", err)
			}
			return
		}

		// Try to determine message type: envelope, regular message, or response
		var msgType struct {
			// Response fields
			ID     string          `json:"id"`
			Result json.RawMessage `json:"result,omitempty"`
			Error  *BrokerError    `json:"error,omitempty"`
			// Envelope fields
			Source      string `json:"source"`
			Destination string `json:"destination"`
			MessageType string `json:"message_type"`
			// Regular message fields
			Type   string `json:"type"`
			Target string `json:"target"`
		}

		if err := json.Unmarshal(rawMsg, &msgType); err != nil {
			if c.debug {
				log.Printf("Failed to parse message type: %v", err)
			}
			continue
		}

		// Check if this is a response message (has result or error field)
		if msgType.ID != "" && (msgType.Result != nil || msgType.Error != nil) {
			// This is a response message - route it to the waiting call
			var resp BrokerResponse
			if err := json.Unmarshal(rawMsg, &resp); err != nil {
				if c.debug {
					log.Printf("Failed to decode response: %v", err)
				}
				continue
			}

			c.responseChMux.RLock()
			if responseChan, exists := c.responseChans[resp.ID]; exists {
				select {
				case responseChan <- &resp:
					// Response delivered
				default:
					if c.debug {
						log.Printf("Warning: response channel full for request %s", resp.ID)
					}
				}
			}
			c.responseChMux.RUnlock()
			continue
		} else if msgType.Source != "" && msgType.Destination != "" && msgType.MessageType != "" {
			// This is an envelope
			var env envelope.Envelope
			if err := json.Unmarshal(rawMsg, &env); err != nil {
				if c.debug {
					log.Printf("Failed to decode envelope: %v", err)
				}
				continue
			}

			if c.debug {
				log.Printf("Received envelope: %s -> %s (%s)", env.Source, env.Destination, env.MessageType)
			}

			// Route envelope to appropriate listener
			c.listenersMux.RLock()
			if listener, exists := c.envListeners[env.Destination]; exists {
				select {
				case listener <- &env:
					// Envelope delivered
				default:
					if c.debug {
						log.Printf("Warning: envelope listener channel full for target %s", env.Destination)
					}
				}
			}
			c.listenersMux.RUnlock()
		} else if msgType.Type != "" && msgType.Target != "" {
			// This is a regular message
			var msg BrokerMessage
			if err := json.Unmarshal(rawMsg, &msg); err != nil {
				if c.debug {
					log.Printf("Failed to decode regular message: %v", err)
				}
				continue
			}

			if c.debug {
				log.Printf("Received message: ID=%s, Target=%s, Type=%s, Meta=%+v", msg.ID, msg.Target, msg.Type, msg.Meta)
			}

			// Route message to appropriate listener
			c.listenersMux.RLock()
			if listener, exists := c.listeners[msg.Target]; exists {
				select {
				case listener <- &msg:
					// Message delivered
				default:
					if c.debug {
						log.Printf("Warning: listener channel full for target %s", msg.Target)
					}
				}
			}
			c.listenersMux.RUnlock()
		} else {
			if c.debug {
				log.Printf("Unknown message format received: %s", string(rawMsg))
			}
		}
	}
}

func (c *BrokerClient) Publish(topic string, message BrokerMessage) error {
	c.mux.Lock()
	defer c.mux.Unlock()

	params := map[string]interface{}{
		"topic":   topic,
		"message": message,
	}

	_, err := c.call("publish", params)
	return err
}

func (c *BrokerClient) Subscribe(topic string) (<-chan *BrokerMessage, error) {
	c.mux.Lock()
	defer c.mux.Unlock()

	params := map[string]interface{}{
		"topic": topic,
	}

	if _, err := c.call("subscribe", params); err != nil {
		return nil, err
	}

	// Create message channel for this subscription
	target := fmt.Sprintf("pub:%s", topic)
	msgChan := make(chan *BrokerMessage, 100)

	c.listenersMux.Lock()
	c.listeners[target] = msgChan
	c.listenersMux.Unlock()

	if c.debug {
		log.Printf("Subscribed to topic: %s", topic)
	}

	return msgChan, nil
}

// Envelope-based publish method
func (c *BrokerClient) PublishEnvelope(topic string, env *envelope.Envelope) error {
	c.mux.Lock()
	defer c.mux.Unlock()

	params := map[string]interface{}{
		"topic":    topic,
		"envelope": env,
	}

	_, err := c.call("publish_envelope", params)
	return err
}

// Subscribe to envelopes on a topic
func (c *BrokerClient) SubscribeEnvelopes(topic string) (<-chan *envelope.Envelope, error) {
	c.mux.Lock()
	defer c.mux.Unlock()

	params := map[string]interface{}{
		"topic": topic,
	}

	if _, err := c.call("subscribe", params); err != nil {
		return nil, err
	}

	// Create envelope channel for this subscription
	target := fmt.Sprintf("sub:%s", topic)
	envChan := make(chan *envelope.Envelope, 100)

	c.listenersMux.Lock()
	c.envListeners[target] = envChan
	c.listenersMux.Unlock()

	if c.debug {
		log.Printf("Subscribed to topic for envelopes: %s", topic)
	}

	return envChan, nil
}

// Send envelope via pipe
func (c *BrokerClient) SendPipeEnvelope(pipeName string, env *envelope.Envelope) error {
	c.mux.Lock()
	defer c.mux.Unlock()

	params := map[string]interface{}{
		"pipe":     pipeName,
		"envelope": env,
	}

	_, err := c.call("send_pipe_envelope", params)
	return err
}

// Receive message or envelope from pipe
func (c *BrokerClient) ReceivePipe(pipeName string, timeoutMs int) (interface{}, error) {
	c.mux.Lock()
	defer c.mux.Unlock()

	params := map[string]interface{}{
		"pipe": pipeName,
	}
	if timeoutMs > 0 {
		params["timeout_ms"] = timeoutMs
	}

	result, err := c.call("receive_pipe", params)
	if err != nil {
		return nil, err
	}

	// Try to unmarshal as envelope first, then as regular message
	var env envelope.Envelope
	if err := json.Unmarshal(result, &env); err == nil && env.Source != "" {
		return &env, nil
	}

	var msg BrokerMessage
	if err := json.Unmarshal(result, &msg); err == nil {
		return &msg, nil
	}

	return result, nil
}

// Convenience method to create and publish an envelope
func (c *BrokerClient) PublishMessage(topic, messageType string, payload interface{}) error {
	env, err := envelope.NewEnvelope(c.agentID, fmt.Sprintf("pub:%s", topic), messageType, payload)
	if err != nil {
		return fmt.Errorf("failed to create envelope: %w", err)
	}

	return c.PublishEnvelope(topic, env)
}

// Pipe methods using the proper pipe functionality
func (c *BrokerClient) ConnectPipe(pipeName, role string) error {
	if c.debug {
		log.Printf("ConnectPipe: %s as %s", pipeName, role)
	}
	// Pipes are created automatically when first used
	return nil
}

func (c *BrokerClient) SendPipe(pipeName string, message BrokerMessage) error {
	c.mux.Lock()
	defer c.mux.Unlock()

	params := map[string]interface{}{
		"pipe":    pipeName,
		"message": message,
	}

	_, err := c.call("send_pipe", params)
	return err
}
