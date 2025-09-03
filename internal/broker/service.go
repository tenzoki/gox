package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/tenzoki/gox/internal/envelope"
)

type Service struct {
	port     string
	protocol string
	codec    string
	debug    bool
	listener net.Listener

	// pub/sub topics
	topics    map[string]*Topic
	topicsMux sync.RWMutex

	// pipe connections (point-to-point)
	pipes    map[string]*Pipe
	pipesMux sync.RWMutex

	// active connections
	connections map[string]*Connection
	connMux     sync.RWMutex
}

type Topic struct {
	Name        string
	Subscribers []*Connection
	Messages    []*Message
	Envelopes   []*envelope.Envelope
	mux         sync.RWMutex
}

type Pipe struct {
	Name      string
	Producer  *Connection
	Consumer  *Connection
	Messages  chan *Message
	Envelopes chan *envelope.Envelope
	mux       sync.RWMutex
}

type Connection struct {
	ID       string
	Conn     net.Conn
	Encoder  *json.Encoder
	Decoder  *json.Decoder
	AgentID  string
	LastSeen time.Time
}

type Message struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Target    string                 `json:"target"`
	Payload   interface{}            `json:"payload"`
	Meta      map[string]interface{} `json:"meta"`
	Timestamp time.Time              `json:"timestamp"`
}

type BrokerRequest struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type BrokerResponse struct {
	ID     string       `json:"id"`
	Result interface{}  `json:"result,omitempty"`
	Error  *BrokerError `json:"error,omitempty"`
}

type BrokerError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Info struct {
	Protocol string
	Address  string
	Port     string
	Codec    string
}

type BrokerConfig struct {
	Port     string
	Protocol string
	Codec    string
	Debug    bool
}

func NewService(config interface{}) *Service {
	// Extract values from config interface
	port := ":9001"
	protocol := "tcp"
	codec := "json"
	debug := false

	if bc, ok := config.(BrokerConfig); ok {
		port = bc.Port
		protocol = bc.Protocol
		codec = bc.Codec
		debug = bc.Debug
	} else if bc, ok := config.(struct {
		Port, Protocol, Codec string
		Debug                 bool
	}); ok {
		port = bc.Port
		protocol = bc.Protocol
		codec = bc.Codec
		debug = bc.Debug
	}

	return &Service{
		port:        port,
		protocol:    protocol,
		codec:       codec,
		debug:       debug,
		topics:      make(map[string]*Topic),
		pipes:       make(map[string]*Pipe),
		connections: make(map[string]*Connection),
	}
}

func (s *Service) Start(ctx context.Context) error {
	listener, err := net.Listen(s.protocol, s.port)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.port, err)
	}
	s.listener = listener

	if s.debug {
		log.Printf("Broker service listening on %s (%s/%s)", s.port, s.protocol, s.codec)
	}

	// Use a separate goroutine to handle context cancellation
	go func() {
		<-ctx.Done()
		if s.debug {
			log.Printf("Broker service shutting down")
		}
		s.listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil // Context was cancelled
			}
			log.Printf("Broker service accept error: %v", err)
			continue
		}

		go s.handleConnection(conn)
	}
}

func (s *Service) handleConnection(netConn net.Conn) {
	defer netConn.Close()

	connID := fmt.Sprintf("conn_%d", time.Now().UnixNano())
	conn := &Connection{
		ID:       connID,
		Conn:     netConn,
		Encoder:  json.NewEncoder(netConn),
		Decoder:  json.NewDecoder(netConn),
		LastSeen: time.Now(),
	}

	s.connMux.Lock()
	s.connections[connID] = conn
	s.connMux.Unlock()

	defer func() {
		s.connMux.Lock()
		delete(s.connections, connID)
		s.connMux.Unlock()
	}()

	if s.debug {
		log.Printf("Broker: new connection %s", connID)
	}

	for {
		var req BrokerRequest
		if err := conn.Decoder.Decode(&req); err != nil {
			if s.debug {
				log.Printf("Broker: decode error from %s: %v", connID, err)
			}
			return
		}

		conn.LastSeen = time.Now()

		if s.debug {
			log.Printf("Broker: received %s from %s", req.Method, connID)
		}

		resp := s.handleRequest(conn, &req)
		if err := conn.Encoder.Encode(resp); err != nil {
			if s.debug {
				log.Printf("Broker: encode error to %s: %v", connID, err)
			}
			return
		}
	}
}

func (s *Service) handleRequest(conn *Connection, req *BrokerRequest) *BrokerResponse {
	switch req.Method {
	case "connect":
		return s.handleConnect(conn, req)
	case "publish":
		return s.handlePublish(conn, req)
	case "publish_envelope":
		return s.handlePublishEnvelope(conn, req)
	case "subscribe":
		return s.handleSubscribe(conn, req)
	case "send_pipe":
		return s.handleSendPipe(conn, req)
	case "send_pipe_envelope":
		return s.handleSendPipeEnvelope(conn, req)
	case "receive_pipe":
		return s.handleReceivePipe(conn, req)
	default:
		return &BrokerResponse{
			ID: req.ID,
			Error: &BrokerError{
				Code:    -32601,
				Message: fmt.Sprintf("Method not found: %s", req.Method),
			},
		}
	}
}

func (s *Service) handleConnect(conn *Connection, req *BrokerRequest) *BrokerResponse {
	var params struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &BrokerResponse{
			ID:    req.ID,
			Error: &BrokerError{Code: -32602, Message: "Invalid params"},
		}
	}

	conn.AgentID = params.AgentID

	if s.debug {
		log.Printf("Broker: agent %s connected on %s", params.AgentID, conn.ID)
	}

	return &BrokerResponse{
		ID:     req.ID,
		Result: "connected",
	}
}

func (s *Service) handlePublish(conn *Connection, req *BrokerRequest) *BrokerResponse {
	var params struct {
		Topic   string  `json:"topic"`
		Message Message `json:"message"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &BrokerResponse{
			ID:    req.ID,
			Error: &BrokerError{Code: -32602, Message: "Invalid params"},
		}
	}

	params.Message.Timestamp = time.Now()
	params.Message.Target = fmt.Sprintf("pub:%s", params.Topic)

	s.topicsMux.Lock()
	topic, exists := s.topics[params.Topic]
	if !exists {
		topic = &Topic{
			Name:        params.Topic,
			Subscribers: make([]*Connection, 0),
			Messages:    make([]*Message, 0, 100),
			Envelopes:   make([]*envelope.Envelope, 0, 100),
		}
		s.topics[params.Topic] = topic
	}
	s.topicsMux.Unlock()

	topic.mux.Lock()
	// Add message to topic history
	topic.Messages = append(topic.Messages, &params.Message)
	if len(topic.Messages) > 100 {
		topic.Messages = topic.Messages[1:]
	}

	// Send to all subscribers
	for _, subscriber := range topic.Subscribers {
		if subscriber.ID != conn.ID { // Don't echo back to sender
			// Create message with proper target for client routing
			// IMPORTANT: Forward ALL message fields, including Meta and ID
			pubMsg := Message{
				ID:        params.Message.ID,
				Type:      params.Message.Type,
				Target:    fmt.Sprintf("pub:%s", params.Topic),
				Payload:   params.Message.Payload,
				Meta:      params.Message.Meta,      // Critical: preserve metadata!
				Timestamp: params.Message.Timestamp,
			}

			if err := subscriber.Encoder.Encode(pubMsg); err != nil {
				if s.debug {
					log.Printf("Broker: failed to send to subscriber %s: %v", subscriber.ID, err)
				}
			}
		}
	}
	topic.mux.Unlock()

	if s.debug {
		log.Printf("Broker: published to topic %s (%d subscribers)", params.Topic, len(topic.Subscribers))
	}

	return &BrokerResponse{
		ID:     req.ID,
		Result: "published",
	}
}

func (s *Service) handleSubscribe(conn *Connection, req *BrokerRequest) *BrokerResponse {
	var params struct {
		Topic string `json:"topic"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &BrokerResponse{
			ID:    req.ID,
			Error: &BrokerError{Code: -32602, Message: "Invalid params"},
		}
	}

	s.topicsMux.Lock()
	topic, exists := s.topics[params.Topic]
	if !exists {
		topic = &Topic{
			Name:        params.Topic,
			Subscribers: make([]*Connection, 0),
			Messages:    make([]*Message, 0, 100),
			Envelopes:   make([]*envelope.Envelope, 0, 100),
		}
		s.topics[params.Topic] = topic
	}
	s.topicsMux.Unlock()

	topic.mux.Lock()
	// Add subscriber if not already subscribed
	found := false
	for _, sub := range topic.Subscribers {
		if sub.ID == conn.ID {
			found = true
			break
		}
	}
	if !found {
		topic.Subscribers = append(topic.Subscribers, conn)
	}
	topic.mux.Unlock()

	if s.debug {
		log.Printf("Broker: agent %s subscribed to topic %s", conn.AgentID, params.Topic)
	}

	return &BrokerResponse{
		ID:     req.ID,
		Result: "subscribed",
	}
}

func (s *Service) handlePublishEnvelope(conn *Connection, req *BrokerRequest) *BrokerResponse {
	var params struct {
		Topic    string             `json:"topic"`
		Envelope *envelope.Envelope `json:"envelope"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &BrokerResponse{
			ID:    req.ID,
			Error: &BrokerError{Code: -32602, Message: "Invalid params"},
		}
	}

	// Validate envelope
	if err := params.Envelope.Validate(); err != nil {
		return &BrokerResponse{
			ID:    req.ID,
			Error: &BrokerError{Code: -32602, Message: fmt.Sprintf("Invalid envelope: %v", err)},
		}
	}

	// Add hop tracking
	if conn.AgentID != "" {
		params.Envelope.AddHop(conn.AgentID)
	}

	// Update destination to include topic prefix
	if params.Envelope.Destination == "" {
		params.Envelope.Destination = fmt.Sprintf("pub:%s", params.Topic)
	}

	s.topicsMux.Lock()
	topic, exists := s.topics[params.Topic]
	if !exists {
		topic = &Topic{
			Name:        params.Topic,
			Subscribers: make([]*Connection, 0),
			Messages:    make([]*Message, 0, 100),
			Envelopes:   make([]*envelope.Envelope, 0, 100),
		}
		s.topics[params.Topic] = topic
	}
	s.topicsMux.Unlock()

	topic.mux.Lock()
	// Add envelope to topic history
	topic.Envelopes = append(topic.Envelopes, params.Envelope)
	if len(topic.Envelopes) > 100 {
		topic.Envelopes = topic.Envelopes[1:]
	}

	// Send to all subscribers
	for _, subscriber := range topic.Subscribers {
		if subscriber.ID != conn.ID { // Don't echo back to sender
			if err := subscriber.Encoder.Encode(params.Envelope); err != nil {
				if s.debug {
					log.Printf("Broker: failed to send envelope to subscriber %s: %v", subscriber.ID, err)
				}
			}
		}
	}
	topic.mux.Unlock()

	if s.debug {
		log.Printf("Broker: published envelope to topic %s (%d subscribers)", params.Topic, len(topic.Subscribers))
	}

	return &BrokerResponse{
		ID:     req.ID,
		Result: "published",
	}
}

func (s *Service) handleSendPipe(conn *Connection, req *BrokerRequest) *BrokerResponse {
	var params struct {
		Pipe    string  `json:"pipe"`
		Message Message `json:"message"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &BrokerResponse{
			ID:    req.ID,
			Error: &BrokerError{Code: -32602, Message: "Invalid params"},
		}
	}

	params.Message.Timestamp = time.Now()
	params.Message.Target = fmt.Sprintf("pipe:%s", params.Pipe)

	s.pipesMux.Lock()
	pipe, exists := s.pipes[params.Pipe]
	if !exists {
		pipe = &Pipe{
			Name:      params.Pipe,
			Messages:  make(chan *Message, 100),
			Envelopes: make(chan *envelope.Envelope, 100),
		}
		s.pipes[params.Pipe] = pipe
	}
	s.pipesMux.Unlock()

	// Send message to pipe
	select {
	case pipe.Messages <- &params.Message:
		if s.debug {
			log.Printf("Broker: sent message to pipe %s", params.Pipe)
		}
		return &BrokerResponse{
			ID:     req.ID,
			Result: "sent",
		}
	default:
		return &BrokerResponse{
			ID:    req.ID,
			Error: &BrokerError{Code: -32603, Message: "Pipe buffer full"},
		}
	}
}

func (s *Service) handleSendPipeEnvelope(conn *Connection, req *BrokerRequest) *BrokerResponse {
	var params struct {
		Pipe     string             `json:"pipe"`
		Envelope *envelope.Envelope `json:"envelope"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &BrokerResponse{
			ID:    req.ID,
			Error: &BrokerError{Code: -32602, Message: "Invalid params"},
		}
	}

	// Validate envelope
	if err := params.Envelope.Validate(); err != nil {
		return &BrokerResponse{
			ID:    req.ID,
			Error: &BrokerError{Code: -32602, Message: fmt.Sprintf("Invalid envelope: %v", err)},
		}
	}

	// Add hop tracking
	if conn.AgentID != "" {
		params.Envelope.AddHop(conn.AgentID)
	}

	// Update destination to include pipe prefix
	if params.Envelope.Destination == "" {
		params.Envelope.Destination = fmt.Sprintf("pipe:%s", params.Pipe)
	}

	s.pipesMux.Lock()
	pipe, exists := s.pipes[params.Pipe]
	if !exists {
		pipe = &Pipe{
			Name:      params.Pipe,
			Messages:  make(chan *Message, 100),
			Envelopes: make(chan *envelope.Envelope, 100),
		}
		s.pipes[params.Pipe] = pipe
	}
	s.pipesMux.Unlock()

	// Send envelope to pipe
	select {
	case pipe.Envelopes <- params.Envelope:
		if s.debug {
			log.Printf("Broker: sent envelope to pipe %s", params.Pipe)
		}
		return &BrokerResponse{
			ID:     req.ID,
			Result: "sent",
		}
	default:
		return &BrokerResponse{
			ID:    req.ID,
			Error: &BrokerError{Code: -32603, Message: "Pipe buffer full"},
		}
	}
}

func (s *Service) handleReceivePipe(conn *Connection, req *BrokerRequest) *BrokerResponse {
	var params struct {
		Pipe    string `json:"pipe"`
		Timeout int    `json:"timeout_ms,omitempty"` // Timeout in milliseconds
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &BrokerResponse{
			ID:    req.ID,
			Error: &BrokerError{Code: -32602, Message: "Invalid params"},
		}
	}

	s.pipesMux.Lock()
	pipe, exists := s.pipes[params.Pipe]
	if !exists {
		pipe = &Pipe{
			Name:      params.Pipe,
			Messages:  make(chan *Message, 100),
			Envelopes: make(chan *envelope.Envelope, 100),
		}
		s.pipes[params.Pipe] = pipe
	}
	s.pipesMux.Unlock()

	// Set timeout (default 5 seconds)
	timeout := 5000
	if params.Timeout > 0 {
		timeout = params.Timeout
	}

	select {
	case msg := <-pipe.Messages:
		return &BrokerResponse{
			ID:     req.ID,
			Result: msg,
		}
	case env := <-pipe.Envelopes:
		return &BrokerResponse{
			ID:     req.ID,
			Result: env,
		}
	case <-time.After(time.Duration(timeout) * time.Millisecond):
		return &BrokerResponse{
			ID:    req.ID,
			Error: &BrokerError{Code: -32603, Message: "Timeout waiting for message"},
		}
	}
}
