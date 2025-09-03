# Gox How-To Guide

This comprehensive guide covers building, running, testing, and developing with Gox's distributed agent-based pipeline system.

## Quick Start

### Prerequisites
- Go 1.20 or higher
- Unix-like environment (macOS, Linux, WSL)

### Build and Test
```bash
# Build the entire system
./build.sh

# Run comprehensive test suite
./test.sh

# Quick unit test verification
go test ./test/ -v
```

This creates executables in the `build/` directory:
- `gox` - Main orchestrator with embedded services
- `file_ingester` - File watching and ingestion agent
- `text_transformer` - Text processing agent  
- `file_writer` - File output agent
- `orchestrator` - Standalone orchestrator for advanced use cases

### Run the Demo Pipeline
```bash
# Automated integration test
./test.sh

# Manual operation
./build/gox gox.yaml
```

The automated test demonstrates:
1. **Pipeline Setup**: Creates sample input files
2. **Service Startup**: Starts orchestrator and agents with dependency ordering
3. **Message Flow**: Processes files through pub/sub and pipe patterns
4. **Output Verification**: Shows processed results and validates transformations
5. **Graceful Shutdown**: Cleans up all processes properly

## Manual Operation

### 1. Start the Orchestrator

The orchestrator provides **Support Service** (agent management) and **Broker Service** (message routing):

```bash
./build/gox gox.yaml
```

**Expected Output:**
```
2025/01/15 14:22:33 INFO  Gox orchestrator starting: production-pipeline
2025/01/15 14:22:33 INFO  Support service listening on :9000
2025/01/15 14:22:33 INFO  Broker service listening on :9001 (tcp/json)
2025/01/15 14:22:33 INFO  Loading agent pool from: pool.yaml  
2025/01/15 14:22:33 INFO  Loading processing cells from: cells.yaml
2025/01/15 14:22:33 INFO  Dependency resolution completed: [file-ingester-001, text-transformer-001, file-writer-001]
2025/01/15 14:22:33 INFO  Orchestrator ready - waiting for agents to connect
```

### 2. Agent Deployment Strategies

Gox supports three deployment strategies:

**Call Agents (Embedded Execution):**
```bash
# Defined with operator: "call:path/to/binary" in pool.yaml
# Executed directly by orchestrator - fastest startup
```

**Spawn Agents (Process Isolation):**
```bash  
# Defined with operator: "spawn:path/to/binary" in pool.yaml
# Independent processes managed by orchestrator
```

**Await Agents (External Connection):**
```bash
# Defined with operator: "await" in pool.yaml
# Connect independently from anywhere on the network
./build/text_transformer --agent-id=text-transformer-001 --support=localhost:9000
```

### 3. Create Input Files

```bash
mkdir -p examples/pipeline-demo/input
echo "Hello World! This is test data." > examples/pipeline-demo/input/test1.txt
echo "Processing with Gox distributed agents." > examples/pipeline-demo/input/test2.txt
echo "Third file for pipeline demonstration." > examples/pipeline-demo/input/test3.txt
```

### 4. Monitor Processing

**Watch for processed files:**
```bash
watch -n 1 'ls -la examples/pipeline-demo/output/'
```

**Monitor agent status:**
```bash
# Check agent registrations
curl http://localhost:9000/agents | jq

# View pipeline status  
curl http://localhost:9000/pipeline/status | jq

# Monitor broker connections
curl http://localhost:9001/connections | jq
```

**Expected processed files:**
- `processed_1705331753123456789_test1.txt`
- `processed_1705331753234567890_test2.txt`  
- `processed_1705331753345678901_test3.txt`

## Configuration System

### Main Configuration (gox.yaml)

```yaml
app_name: "production-pipeline"
debug: false

# Support service (agent management)
support:
  port: ":9000"
  debug: true
  max_connections: 500
  timeout: 60

# Broker service (message routing)  
broker:
  port: ":9001"
  protocol: "tcp"
  codec: "json"
  debug: false
  max_message_size: 1048576

# Configuration file locations
basedir: ["."]
pool: ["pool.yaml"]
cells: ["cells.yaml"]

# Timeout settings
await-timeout_seconds: 300
await_support_reboot_seconds: 60
```

### Agent Pool Definition (pool.yaml)

```yaml
pool:
  agent_types:
    # Call agent - embedded execution (fastest)
    - agent_type: "file-ingester"
      binary: "operators/file_ingester/main.go"
      operator: "call"
      capabilities: ["file-ingestion", "directory-watching"]
      description: "Watches directories and ingests files"
    
    # Spawn agent - process isolation (balanced)
    - agent_type: "text-transformer" 
      binary: "operators/text_transformer/main.go"
      operator: "spawn"
      capabilities: ["text-processing", "transformation"]
      description: "Transforms text content"
    
    # Spawn agent - process isolation
    - agent_type: "file-writer"
      binary: "operators/file_writer/main.go"
      operator: "spawn"
      capabilities: ["file-writing", "data-persistence"]
      description: "Writes processed data to files"
```

### Processing Pipeline (cells.yaml)

```yaml
cell:
  id: "document-processing-pipeline"
  description: "File processing with dependency coordination"
  
  # Orchestration settings
  orchestration:
    startup_timeout_seconds: 120
    shutdown_timeout_seconds: 60
    dependency_wait_timeout_seconds: 30
    
  agents:
    # Stage 1: File ingestion (starts first)
    - id: "file-ingester-001"
      agent_type: "file-ingester"
      dependencies: []  # No dependencies - starts immediately
      ingress: "file:examples/pipeline-demo/input/*.{txt,pdf,docx}"
      egress: "pub:raw-documents"
      config:
        watch_interval: 5
        digest_strategy: "delete"  # delete|move|copy|none
        duplicate_detection: true
        
    # Stage 2: Text transformation (waits for ingester)
    - id: "text-transformer-001"  
      agent_type: "text-transformer"
      dependencies: ["file-ingester-001"]
      ingress: "sub:raw-documents"
      egress: "pipe:processed-text"
      config:
        transformation: "uppercase"
        add_metadata: true
        timeout_seconds: 30
        
    # Stage 3: File output (waits for transformer)
    - id: "file-writer-001"
      agent_type: "file-writer" 
      dependencies: ["text-transformer-001"]
      ingress: "pipe:processed-text"
      egress: "file:examples/pipeline-demo/output/processed_{{.timestamp}}.txt"
      config:
        output_format: "text"
        create_directories: true
        backup_existing: false
```

## Communication Patterns

### File Pattern (`file:path/*.ext`)
**Use Case**: Filesystem integration with watching and templating
```yaml
# Input - watch for new files
ingress: "file:input/*.{txt,json,csv}"

# Output - write with timestamp template  
egress: "file:output/result_{{.timestamp}}_{{.filename}}.txt"
```

### Pub/Sub Pattern (`pub:topic` / `sub:topic`)
**Use Case**: One-to-many broadcasting, event-driven processing
```yaml
# Publisher
egress: "pub:file-events"

# Subscriber (multiple agents can subscribe)
ingress: "sub:file-events"
```

### Pipe Pattern (`pipe:channel-name`)
**Use Case**: Point-to-point delivery, sequential processing
```yaml
# Producer  
egress: "pipe:processing-queue"

# Consumer (guaranteed single consumer)
ingress: "pipe:processing-queue"
```

### Complex Flow Example
```yaml
# Multi-stage pipeline with branching
agents:
  - id: "source"
    ingress: "file:input/*.csv" 
    egress: "pub:raw-data"
    
  - id: "validator"
    ingress: "sub:raw-data"
    egress: "pub:validated-data"
    
  - id: "processor-1"
    ingress: "sub:validated-data"
    egress: "pipe:results-1"
    
  - id: "processor-2" 
    ingress: "sub:validated-data"
    egress: "pipe:results-2"
    
  - id: "aggregator"
    dependencies: ["processor-1", "processor-2"]
    ingress: ["pipe:results-1", "pipe:results-2"]
    egress: "file:output/final_{{.date}}.json"
```

## Environment Variables

### Global Settings
```bash
# Enable debug logging for all components
export GOX_DEBUG=true

# Override service addresses
export GOX_SUPPORT_PORT=":8000"
export GOX_BROKER_PORT=":8001"

# Override configuration paths
export GOX_CONFIG_PATH="./config/production.yaml"
```

### Agent-Specific Settings  
```bash
# File ingester settings
export GOX_WATCH_DIR="/data/input"
export GOX_WATCH_INTERVAL="10"

# File writer settings
export GOX_OUTPUT_DIR="/data/output"
export GOX_OUTPUT_FORMAT="json"

# Text transformer settings
export GOX_TRANSFORM_TYPE="lowercase"
export GOX_ADD_METADATA="true"
```

### Production Example
```bash
# Production environment setup
export GOX_DEBUG=false
export GOX_SUPPORT_PORT=":9000"
export GOX_BROKER_PORT=":9001" 
export GOX_WATCH_DIR="/mnt/shared/input"
export GOX_OUTPUT_DIR="/mnt/shared/output"

# Start with production config
./build/gox production.yaml
```

## Message Flow Implementation

### Envelope Structure
All messages are wrapped in standardized envelopes:

```go
type Envelope struct {
    // Core identification
    ID            string    `json:"id"`           
    CorrelationID string    `json:"correlation_id,omitempty"`
    
    // Routing information
    Source        string    `json:"source"`       
    Destination   string    `json:"destination"`  
    MessageType   string    `json:"message_type"`
    
    // Timing and sequencing
    Timestamp     time.Time `json:"timestamp"`    
    TTL           int64     `json:"ttl,omitempty"`
    Sequence      int64     `json:"sequence,omitempty"`
    
    // Payload and metadata
    Payload       json.RawMessage       `json:"payload"`
    Headers       map[string]string     `json:"headers,omitempty"`
    Properties    map[string]interface{} `json:"properties,omitempty"`
    
    // Distributed tracing
    TraceID       string    `json:"trace_id,omitempty"`
    SpanID        string    `json:"span_id,omitempty"`
    HopCount      int       `json:"hop_count"`
    Route         []string  `json:"route,omitempty"`
}
```

### Pub/Sub Implementation
```go
// Publisher (File Ingester)
envelope, err := envelope.NewEnvelope(
    "file-ingester-001",      // source
    "raw-documents",          // topic  
    "file-content",           // message type
    fileData,                 // payload
)

brokerClient.Publish("raw-documents", envelope)

// Subscriber (Text Transformer)
msgChan, err := brokerClient.Subscribe("raw-documents")
for envelope := range msgChan {
    var fileData FileContent
    envelope.UnmarshalPayload(&fileData)
    processFile(fileData)
}
```

### Pipe Implementation
```go
// Producer (Text Transformer)
brokerClient.ConnectPipe("processed-text", "producer")

processedEnvelope, _ := envelope.NewEnvelope(
    "text-transformer-001",
    "processed-text", 
    "processed-content",
    transformedData,
)

brokerClient.SendPipe("processed-text", processedEnvelope)

// Consumer (File Writer)
brokerClient.ConnectPipe("processed-text", "consumer")
result, err := brokerClient.ReceivePipe("processed-text", 5000) // 5 second timeout
```

## Agent Configuration API

### Dynamic Configuration Retrieval

The Support Service provides runtime access to agent configurations:

```bash
# Get complete agent configuration
curl -X POST http://localhost:9000/rpc \
  -H "Content-Type: application/json" \
  -d '{
    "id": "1",
    "method": "get_agent_cell_config", 
    "params": {"agent_id": "file-ingester-001"}
  }' | jq
```

**Response:**
```json
{
  "id": "1",
  "result": {
    "id": "file-ingester-demo-001",
    "agent_type": "file-ingester",
    "dependencies": [],
    "ingress": "file:examples/pipeline-demo/input/*.txt",
    "egress": "pub:raw-documents",
    "config": {
      "digest": true,
      "digest_strategy": "delete",
      "watch_interval_seconds": 2
    }
  }
}
```

### Pipeline Dependencies API

Query the complete dependency graph:

```bash
curl -X POST http://localhost:9000/rpc \
  -H "Content-Type: application/json" \
  -d '{
    "id": "2", 
    "method": "get_pipeline_dependencies",
    "params": {}
  }' | jq
```

**Response:**
```json
{
  "id": "2",
  "result": {
    "dependencies": {
      "file-ingester-demo-001": [],
      "text-transformer-demo-001": ["file-ingester-demo-001"], 
      "file-writer-demo-001": ["text-transformer-demo-001"]
    },
    "startup_order": [
      "file-ingester-demo-001",
      "text-transformer-demo-001", 
      "file-writer-demo-001"
    ],
    "shutdown_order": [
      "file-writer-demo-001",
      "text-transformer-demo-001",
      "file-ingester-demo-001" 
    ]
  }
}
```

## Troubleshooting

### Connection Issues

**Problem:** Agent cannot connect to Support service
```bash
dial tcp [::1]:9000: connect: connection refused
```

**Solution:**
```bash
# 1. Verify orchestrator is running
ps aux | grep gox

# 2. Check if ports are available
lsof -i :9000 -i :9001

# 3. Test direct connection
nc -zv localhost 9000
nc -zv localhost 9001

# 4. Check firewall settings
sudo ufw status
```

### Dependency Resolution Issues

**Problem:** Agents start in wrong order or hang waiting for dependencies
```bash
WARN  Agent text-transformer-001 waiting for dependencies: [file-ingester-001]
```

**Solution:**
```bash
# 1. Verify dependency configuration in cells.yaml
grep -A 5 -B 5 "dependencies:" cells.yaml

# 2. Check for circular dependencies
./build/gox --validate-config gox.yaml

# 3. Monitor agent registration
curl http://localhost:9000/agents | jq '.[] | {id: .id, state: .state}'

# 4. Enable debug logging
export GOX_DEBUG=true
./build/gox gox.yaml
```

### Message Flow Issues

**Problem:** Messages not flowing between agents
```bash
INFO  Published to topic raw-documents: envelope-123
WARN  No subscribers found for topic: raw-documents
```

**Solution:**
```bash
# 1. Verify communication patterns in cells.yaml
grep -E "(ingress|egress):" cells.yaml

# 2. Check broker connections
curl http://localhost:9001/connections | jq

# 3. Monitor message flow with debug enabled
export GOX_DEBUG=true

# 4. Test broker connectivity directly
nc localhost 9001
```

### Performance Issues

**Problem:** Message processing is slow or backing up
```bash
INFO  Broker queue depth: topic=raw-documents size=1000 
WARN  Agent text-transformer-001 processing time: 5.2s (threshold: 1s)
```

**Solution:**
```bash
# 1. Scale agents horizontally
# Add multiple instances in cells.yaml:
- id: "text-transformer-002"
  agent_type: "text-transformer"
  dependencies: ["file-ingester-001"]
  ingress: "sub:raw-documents"
  egress: "pipe:processed-text"

# 2. Increase message buffer sizes
# In broker configuration:
broker:
  max_message_size: 10485760  # 10MB
  buffer_size: 10000

# 3. Optimize agent processing
# Use batch processing where possible
# Implement async processing patterns
# Add connection pooling for high throughput

# 4. Monitor resource usage
top -p $(pgrep gox)
htop
```

### File Processing Issues

**Problem:** Files not being detected or processed
```bash
INFO  File watcher started: examples/pipeline-demo/input/*.txt
WARN  No files matched pattern in 60 seconds
```

**Solution:**
```bash
# 1. Verify file permissions
ls -la examples/pipeline-demo/input/
chmod 644 examples/pipeline-demo/input/*.txt

# 2. Check file pattern matching
# Test pattern locally:
ls examples/pipeline-demo/input/*.txt

# 3. Verify directory structure
find examples/pipeline-demo -type f -name "*.txt"

# 4. Monitor file events
inotifywait -m examples/pipeline-demo/input/

# 5. Check digest strategy
# If using "delete", ensure files exist before processing
# If using "move", verify target directory exists
```

## Development Guide

### Creating Custom Agents

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/tenzoki/gox/internal/agent"
    "github.com/tenzoki/gox/internal/client"
    "github.com/tenzoki/gox/internal/envelope"
)

func main() {
    // Configure the base agent
    config := agent.BaseAgentConfig{
        AgentID:       "my-custom-agent",
        AgentType:     "custom-processor",
        SupportAddr:   "localhost:9000",
        BrokerAddr:    "localhost:9001", 
        Debug:         true,
    }
    
    // Create base agent with automatic service connections
    baseAgent := agent.NewBaseAgent(config)
    
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    
    // Start the agent
    if err := baseAgent.Start(ctx); err != nil {
        log.Fatalf("Failed to start agent: %v", err)
    }
    
    // Subscribe to input messages
    msgChan, err := baseAgent.BrokerClient.Subscribe("input-topic")
    if err != nil {
        log.Fatalf("Failed to subscribe: %v", err)
    }
    
    // Process messages
    for {
        select {
        case msg := <-msgChan:
            processMessage(msg, baseAgent)
        case <-ctx.Done():
            log.Println("Agent shutting down...")
            return
        }
    }
}

func processMessage(msg client.BrokerMessage, agent *agent.BaseAgent) {
    // Extract envelope
    env, err := envelope.FromJSON(msg.Data)
    if err != nil {
        log.Printf("Failed to parse envelope: %v", err)
        return
    }
    
    // Add hop tracking
    env.AddHop("my-custom-agent")
    
    // Process payload
    var inputData CustomData
    if err := env.UnmarshalPayload(&inputData); err != nil {
        log.Printf("Failed to unmarshal payload: %v", err)
        return
    }
    
    // Custom business logic
    result := processCustomData(inputData)
    
    // Create output envelope
    outputEnv, err := envelope.NewEnvelope(
        "my-custom-agent",
        "output-topic", 
        "processed-data",
        result,
    )
    if err != nil {
        log.Printf("Failed to create output envelope: %v", err)
        return
    }
    
    // Set correlation ID for tracing
    outputEnv.CorrelationID = env.ID
    outputEnv.TraceID = env.TraceID
    
    // Publish result
    outputMsg := client.BrokerMessage{
        Type: "processed-data",
        Data: outputEnv.ToJSON(),
    }
    
    if err := agent.BrokerClient.Publish("output-topic", outputMsg); err != nil {
        log.Printf("Failed to publish result: %v", err)
    }
}

type CustomData struct {
    ID          string    `json:"id"`
    Content     string    `json:"content"`
    ProcessedAt time.Time `json:"processed_at"`
}

func processCustomData(data CustomData) CustomData {
    // Custom processing logic
    data.Content = strings.ToUpper(data.Content)
    data.ProcessedAt = time.Now()
    return data
}
```

### Agent Registration and Configuration

```yaml
# Add to pool.yaml
pool:
  agents:
    - id: "my-custom-agent"
      operator: "await"  # External connection
      capabilities: ["custom-processing", "data-transformation"]

# Add to cells.yaml
cell:
  id: "custom-processing-pipeline"
  agents:
    - id: "my-custom-agent-001"
      agent_type: "my-custom-agent"
      dependencies: ["data-source"]
      ingress: "sub:input-topic"
      egress: "pub:output-topic"
      config:
        batch_size: 100
        timeout_seconds: 30
        custom_setting: "value"
```

### Testing Custom Agents

```bash
# Unit tests
go test ./my-custom-agent/ -v

# Integration test with orchestrator
export GOX_DEBUG=true
./build/gox gox.yaml &
./my-custom-agent &

# Send test messages
echo '{"type": "test", "payload": "test data"}' | \
  nc localhost 9001
```

## Production Deployment

### Docker Deployment

```dockerfile
# Multi-stage build for minimal image size
FROM golang:1.20-alpine AS builder

WORKDIR /app
COPY . .

# Build all components
RUN go mod download
RUN go build -o build/gox ./cmd/gox/
RUN go build -o build/file_ingester ./operators/file_ingester/ 
RUN go build -o build/text_transformer ./operators/text_transformer/
RUN go build -o build/file_writer ./operators/file_writer/

FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /root/

# Copy binaries and configuration
COPY --from=builder /app/build/ ./
COPY --from=builder /app/*.yaml ./

# Create directories
RUN mkdir -p /data/input /data/output /data/config

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
  CMD nc -z localhost 9000 && nc -z localhost 9001 || exit 1

# Default command
CMD ["./gox", "gox.yaml"]
```

### Kubernetes Deployment

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: gox-config
data:
  gox.yaml: |
    app_name: "k8s-pipeline"
    debug: false
    support:
      port: ":9000"
      debug: false
    broker:
      port: ":9001" 
      protocol: "tcp"
      codec: "json"
      debug: false
  
  cells.yaml: |
    cell:
      id: "k8s-processing-pipeline"
      agents:
        - id: "file-ingester-001"
          dependencies: []
          ingress: "file:/data/input/*.txt"
          egress: "pub:raw-files"
        - id: "text-transformer-001"
          dependencies: ["file-ingester-001"]
          ingress: "sub:raw-files"
          egress: "pipe:processed-data"
        - id: "file-writer-001"
          dependencies: ["text-transformer-001"]
          ingress: "pipe:processed-data"
          egress: "file:/data/output/result_{{.timestamp}}.txt"

---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: gox-orchestrator
  labels:
    app: gox-orchestrator
spec:
  replicas: 1
  selector:
    matchLabels:
      app: gox-orchestrator
  template:
    metadata:
      labels:
        app: gox-orchestrator
    spec:
      containers:
      - name: gox
        image: gox:latest
        ports:
        - containerPort: 9000
          name: support
        - containerPort: 9001
          name: broker
        env:
        - name: GOX_DEBUG
          value: "false"
        - name: GOX_SUPPORT_PORT
          value: ":9000"
        - name: GOX_BROKER_PORT
          value: ":9001"
        volumeMounts:
        - name: config
          mountPath: /root
        - name: data
          mountPath: /data
        resources:
          requests:
            memory: "128Mi"
            cpu: "100m"
          limits:
            memory: "512Mi"
            cpu: "500m"
        livenessProbe:
          tcpSocket:
            port: 9000
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          tcpSocket:
            port: 9001
          initialDelaySeconds: 10
          periodSeconds: 5
      volumes:
      - name: config
        configMap:
          name: gox-config
      - name: data
        persistentVolumeClaim:
          claimName: gox-data

---
apiVersion: v1
kind: Service
metadata:
  name: gox-orchestrator-service
  labels:
    app: gox-orchestrator
spec:
  selector:
    app: gox-orchestrator
  ports:
  - name: support
    port: 9000
    targetPort: 9000
  - name: broker
    port: 9001
    targetPort: 9001
  type: ClusterIP

---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: gox-data
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
```

### Monitoring and Observability

```yaml
# Prometheus ServiceMonitor
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: gox-metrics
spec:
  selector:
    matchLabels:
      app: gox-orchestrator
  endpoints:
  - port: metrics
    path: /metrics
```

**Add metrics to agents:**
```go
import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

var (
    messagesProcessed = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "gox_messages_processed_total",
            Help: "Total number of messages processed",
        },
        []string{"agent_id", "message_type", "status"},
    )
    
    processingDuration = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "gox_processing_duration_seconds", 
            Help: "Time spent processing messages",
        },
        []string{"agent_id", "message_type"},
    )
)

func processMessage(msg client.BrokerMessage, agentID string) {
    timer := prometheus.NewTimer(processingDuration.WithLabelValues(agentID, msg.Type))
    defer timer.ObserveDuration()
    
    // Process message...
    
    messagesProcessed.WithLabelValues(agentID, msg.Type, "success").Inc()
}
```

This comprehensive how-to guide provides everything needed to build, deploy, and operate Gox-based distributed processing systems in development and production environments.