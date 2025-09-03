# Gox Architecture Documentation

## Overview: Framework-Driven Architecture

Gox revolutionizes distributed processing through its **Agent Framework** - a breakthrough that eliminates 90% of infrastructure code from agents. The system combines intelligent orchestration with zero-boilerplate agent development.

**Key Innovation**: Agents implement only `ProcessMessage()` business logic - the framework handles all infrastructure automatically.

## Core Architecture Principles

### 1. **Framework-Driven Agents**
The Agent Framework (`internal/agent/framework.go`) eliminates boilerplate:
```go
// Traditional agent: 100+ lines of infrastructure code
// Framework agent: 3 lines total
type MyAgent struct { agent.DefaultAgentRunner }
func (a *MyAgent) ProcessMessage(...) { /* business logic only */ }
func main() { agent.Run(&MyAgent{}, "agent-type") }
```

### 2. **Embedded Infrastructure**
Single `gox` binary contains all services:
- **Orchestrator**: Dependency resolution and coordination
- **Support Service** (:9000): Agent registry and configuration
- **Broker Service** (:9001): Message routing and delivery

### 3. **Declarative Configuration**
YAML files define system behavior:
- **pool.yaml**: Agent types and capabilities
- **cells.yaml**: Pipeline instances and dependencies  
- **gox.yaml**: Infrastructure settings

## System Components

```mermaid
graph TB
    subgraph "Gox Orchestrator"
        ORC[Orchestrator Core]
        SUPP[Support Service<br/>:9000]
        BROKER[Broker Service<br/>:9001]
    end
    
    subgraph "Agent Pool"
        A1[File Ingester<br/>Agent]
        A2[Text Transformer<br/>Agent]
        A3[File Writer<br/>Agent]
        A4[Custom Agent<br/>N...]
    end
    
    subgraph "Configuration"
        CONFIG[gox.yaml<br/>Main Config]
        POOL[pool.yaml<br/>Agent Definitions]
        CELLS[cells.yaml<br/>Pipeline Config]
    end
    
    subgraph "Communication Patterns"
        PUBSUB[Pub/Sub Topics]
        PIPES[Point-to-Point Pipes]
        FILES[File I/O]
    end
    
    ORC --> SUPP
    ORC --> BROKER
    ORC --> CONFIG
    ORC --> POOL
    ORC --> CELLS
    
    SUPP -.-> A1
    SUPP -.-> A2
    SUPP -.-> A3
    SUPP -.-> A4
    
    BROKER --> PUBSUB
    BROKER --> PIPES
    
    A1 --> PUBSUB
    A2 --> PIPES
    A3 --> FILES
    
    A1 -.->|Register| SUPP
    A2 -.->|Register| SUPP
    A3 -.->|Register| SUPP
    A4 -.->|Register| SUPP
    
    A1 -.->|Connect| BROKER
    A2 -.->|Connect| BROKER
    A3 -.->|Connect| BROKER
    A4 -.->|Connect| BROKER
```

### Framework Architecture

#### Agent Framework Core
**Location**: `internal/agent/framework.go`

Handles all infrastructure concerns that agents previously managed manually:
```go
type AgentFramework struct {
    runner    AgentRunner    // Business logic interface
    baseAgent *BaseAgent    // Infrastructure services
    handlers  *ConnectionHandlers  // Message routing
}

// Run() method eliminates 90% of agent boilerplate:
// - BaseAgent initialization (was 37 lines per agent)
// - Connection setup (was 25 lines per agent)
// - Message processing loops (was 30+ lines per agent)  
// - Signal handling (was 20 lines per agent)
// - Graceful shutdown (was 15 lines per agent)
```

#### Infrastructure Services

**Support Service** (:9000) - Agent Control Plane:
```go
// Core APIs agents use
RegisterAgent(registration AgentRegistration) error
GetAgentCellConfig(agentID string) (*AgentCellConfig, error)
ReportStateChange(agentID, state string) error
```

**Broker Service** (:9001) - Message Data Plane:
- **Pub/Sub Topics**: One-to-many broadcasting (`pub:topic`/`sub:topic`)
- **Point-to-Point Pipes**: Guaranteed single-consumer delivery (`pipe:name`)
- **File Integration**: Filesystem watching and templating (`file:path/*.ext`)
- **Protocol**: JSON-over-TCP with automatic connection management

**Orchestrator Core** - Dependency Coordination:
- Topological sorting for startup order
- Circular dependency detection
- State-based coordination (installed → configured → ready → running)
- Reverse-order graceful shutdown

## Agent Architecture

### Agent Framework Pattern

**Before Framework** (Traditional agents):
```go
// Every agent needed 100+ lines of boilerplate:
func main() {
    // BaseAgent setup (37 lines)
    debug := GetDebugFromEnv()
    supportAddr := GetEnvConfig("SUPPORT_ADDR", "localhost:9000")
    agentConfig := AgentConfig{...}
    baseAgent, err := NewBaseAgent(agentConfig)
    // ... error handling
    
    // Connection management (25 lines)
    ingress := baseAgent.GetConfigString("ingress", "")
    egress := baseAgent.GetConfigString("egress", "")
    handlers, err := NewConnectionHandlers(ingress, egress, baseAgent)
    // ... connection setup
    
    // Message processing (30+ lines)
    msgChan, err := handlers.Connect()
    go func() {
        for {
            select {
            case msg := <-msgChan:
                // Process message...
            case <-ctx.Done():
                return
            }
        }
    }()
    
    // Signal handling (20 lines)
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
    // ... shutdown logic
}
```

**After Framework** (Zero boilerplate):
```go
type MyAgent struct { agent.DefaultAgentRunner }

func (a *MyAgent) ProcessMessage(msg *client.BrokerMessage, base *agent.BaseAgent) (*client.BrokerMessage, error) {
    // Business logic only!
    return processData(msg), nil
}

func main() {
    agent.Run(&MyAgent{}, "my-agent") // Framework handles ALL infrastructure
}
```

### Deployment Strategies

| Strategy | Definition | Use Case | Performance |
|----------|------------|----------|-------------|
| **Call** | `operator: "call:binary"` | Development, embedded execution | Fastest (no process overhead) |
| **Spawn** | `operator: "spawn:binary"` | Production, process isolation | Balanced (managed processes) |
| **Await** | `operator: "await"` | Distributed, external agents | Flexible (network deployment) |

**Framework handles all deployment complexity** - agents work identically regardless of strategy.

## Message Flow Architecture

### Envelope System

All messages are wrapped in standardized envelopes containing:

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
    
    // Content and metadata
    Payload       json.RawMessage       `json:"payload"`
    Headers       map[string]string     `json:"headers,omitempty"`
    Properties    map[string]interface{} `json:"properties,omitempty"`
    
    // Tracing and debugging
    TraceID       string    `json:"trace_id,omitempty"`
    SpanID        string    `json:"span_id,omitempty"`
    HopCount      int       `json:"hop_count,omitempty"`
    Route         []string  `json:"route,omitempty"`
}
```

### Pipeline Flow Patterns

**Basic Linear Pipeline**:
```
Input → Agent A → pub:topic → Agent B → pipe:direct → Agent C → Output
```

**Fan-out/Fan-in Pattern**:
```
Input → Agent A → pub:broadcast
                     ├── Agent B1 → pipe:collect
                     ├── Agent B2 → pipe:collect  
                     └── Agent B3 → pipe:collect
                                      ↓
                                  Agent C → Output
```

**Complex Processing Graph**:
```
File Source → Ingester → pub:raw-data
                           ├── Validator → pub:valid-data → Enricher
                           ├── Transformer → pub:transformed → Aggregator  
                           └── Splitter → pipe:chunks → Processor → Output
```

## Configuration System

### Three-Layer Configuration

**1. Infrastructure** (`gox.yaml`):
```yaml
app_name: "my-pipeline"
debug: true
support:
  port: ":9000"
broker:
  port: ":9001"
  protocol: "tcp"
```

**2. Agent Types** (`pool.yaml`):
```yaml
pool:
  agent_types:
    - agent_type: "file-ingester"
      binary: "operators/file_ingester/main.go"
      operator: "call"  # call|spawn|await
      capabilities: ["file-ingestion"]
      
    - agent_type: "text-transformer"
      binary: "operators/text_transformer/main.go" 
      operator: "spawn"
      capabilities: ["text-processing"]
```

**3. Pipeline Instances** (`cells.yaml`):
```yaml
cell:
  id: "processing-pipeline"
  agents:
    - id: "ingester-001"          # Unique instance ID
      agent_type: "file-ingester" # References pool.yaml
      dependencies: []             # Startup order
      ingress: "file:input/*.txt"  # Data source
      egress: "pub:raw-files"      # Data destination
      config:                      # Instance-specific settings
        watch_interval: 5
        
    - id: "transformer-001"
      agent_type: "text-transformer"
      dependencies: ["ingester-001"]  # Wait for ingester
      ingress: "sub:raw-files"
      egress: "pipe:processed-data"
      config:
        transformation: "uppercase"
```

**Framework Integration**: Agent Framework automatically merges configuration layers and provides them to agents via `base *agent.BaseAgent` parameter.

## Scalability and Deployment

### Framework-Enabled Scaling
The Agent Framework makes scaling trivial - agents work identically at any scale:

**Single Machine** (Development):
```yaml
# All agents as call operators - fastest startup
agents:
  - id: "processor-001"
    operator: "call:./my_agent"
```

**Multi-Process** (Testing):
```yaml
# Process isolation with spawn operators
agents:
  - id: "processor-001"
    operator: "spawn:./my_agent"
  - id: "processor-002"
    operator: "spawn:./my_agent"
```

**Distributed** (Production):
```bash
# Central orchestrator
./gox orchestrator.yaml

# Remote agents connect automatically
./my_agent --agent-id=processor-001 --support=orchestrator:9000
./my_agent --agent-id=processor-002 --support=orchestrator:9000
```

**Key Insight**: Framework abstracts deployment complexity - same agent code works in all scenarios.

## Reliability and Observability

### Framework-Provided Reliability
The Agent Framework handles all error scenarios:
- **Connection failures**: Automatic retry with exponential backoff
- **Agent crashes**: Orchestrator detects and restarts (spawn/await modes)
- **Message failures**: Built-in acknowledgment and retry logic
- **Graceful shutdown**: Framework coordinates clean termination

### Built-in Observability
Every message automatically includes tracing metadata:
```go
type Envelope struct {
    ID, TraceID, CorrelationID string  // Full tracing
    Route []string                     // Message path
    HopCount int                       // Processing depth
    Timestamp time.Time                // Timing data
}
```

Agents get structured logging for free:
```go
base.LogInfo("Processing message %s", msg.ID)
base.LogDebug("Message metadata: %+v", msg.Meta)
base.LogError("Processing failed: %v", err)
```

## Performance Characteristics

### Framework Performance Benefits
- **Reduced overhead**: Framework eliminates duplicate connection handling across agents
- **Optimized message routing**: Single broker handles all patterns efficiently
- **Memory efficiency**: Shared infrastructure services vs per-agent implementations

### Benchmarks
- **Message routing**: Sub-millisecond for local deployments
- **Agent startup**: Call < 10ms, Spawn < 100ms, Await < 1s
- **Memory per agent**: Framework agents use ~50% less memory than traditional
- **CPU efficiency**: Business logic only - no infrastructure overhead

### Deployment Performance
| Strategy | Startup Time | Memory Usage | CPU Overhead | Network Hops |
|----------|--------------|--------------|--------------|---------------|
| Call     | ~10ms        | Shared       | Minimal      | 0 (embedded)  |
| Spawn    | ~100ms       | Isolated     | Low          | 1 (localhost) |
| Await    | ~1s          | Independent  | Variable     | N (network)   |

## Security Model

### Framework Security Benefits
- **Reduced attack surface**: Agents contain only business logic
- **Centralized security**: All network handling in framework, not agents
- **Process isolation**: Spawn/await strategies provide automatic sandboxing
- **Configuration security**: YAML validation prevents malicious configs

### Security Features
- **Network**: JSON-over-TCP with optional TLS encryption
- **Process isolation**: Spawn and await strategies prevent agent interference
- **Configuration validation**: Framework validates all YAML before deployment
- **Audit logging**: Automatic logging of all agent lifecycle events
- **Resource limits**: Configurable memory and CPU constraints per agent

## Agent Configuration and Lifecycle Management

### Agent Configuration Flow

Gox uses a sophisticated configuration system that separates **agent types** (pool.yaml) from **agent instances** (cells.yaml):

```mermaid
graph TB
    subgraph "Configuration Loading"
        ORCH[Orchestrator Startup]
        LOADPOOL[Load pool.yaml<br/>Agent Type Definitions] 
        LOADCELLS[Load cells.yaml<br/>Pipeline Configurations]
        SUPPORT[Support Service<br/>Configuration Distribution]
    end
    
    subgraph "Agent Registration Flow"
        AGENT[Agent Startup]
        REGISTER[Register with Support]
        GETCONFIG[GetAgentCellConfig API]
        APPLYCONFIG[Apply Configuration]
        TRANSITION[State: installed → configured]
    end
    
    subgraph "Configuration Structure"
        POOLDEF[Agent Type Definition<br/>• Binary path<br/>• Capabilities<br/>• Default operator]
        CELLDEF[Agent Instance Definition<br/>• Unique ID<br/>• Dependencies<br/>• Ingress/Egress<br/>• Custom config]
        MERGED[Merged Agent Config<br/>• Runtime configuration<br/>• Type + Instance data]
    end
    
    ORCH --> LOADPOOL
    ORCH --> LOADCELLS 
    LOADPOOL --> SUPPORT
    LOADCELLS --> SUPPORT
    
    AGENT --> REGISTER
    REGISTER --> GETCONFIG
    GETCONFIG --> SUPPORT
    SUPPORT --> MERGED
    MERGED --> APPLYCONFIG
    APPLYCONFIG --> TRANSITION
    
    POOLDEF --> MERGED
    CELLDEF --> MERGED
    
    style SUPPORT fill:#f3e5f5
    style MERGED fill:#e8f5e8
    style TRANSITION fill:#c8e6c9
```

### Agent Lifecycle States

Each agent progresses through well-defined states coordinated by the Support service:

| State | Description | API Methods Available |
|-------|-------------|---------------------|
| **Installed** | Agent connected to Support, registered | `RegisterAgent`, `GetBroker` |
| **Configured** | Cell configuration loaded from Support | `GetAgentCellConfig`, `ReportStateChange` |
| **Ready** | Dependencies satisfied, ready to process | `WaitForState`, `GetPipelineDependencies` |
| **Running** | Actively processing messages | All APIs available |
| **Paused** | Temporarily suspended | State reporting only |
| **Stopped** | Gracefully shut down | Re-registration required |
| **Error** | Fault detected, intervention needed | `ReportStateChange`, recovery APIs |

### Support Service Configuration APIs

The Support Service provides comprehensive APIs for agent configuration management:

```go
// Core configuration retrieval
type AgentCellConfig struct {
    ID           string                 `json:"id"`           // Agent instance ID
    AgentType    string                 `json:"agent_type"`   // Type from pool.yaml
    Ingress      string                 `json:"ingress"`      // Input specification  
    Egress       string                 `json:"egress"`       // Output specification
    Dependencies []string               `json:"dependencies"` // Dependency list
    Config       map[string]interface{} `json:"config"`       // Custom configuration
}

// Pipeline dependency information  
type DependencyInfo struct {
    AgentID      string   `json:"agent_id"`
    Dependencies []string `json:"dependencies"`
    AgentType    string   `json:"agent_type"`
}

// Orchestration configuration
type OrchestrationConfig struct {
    StartupTimeout  string `json:"startup_timeout"`
    ShutdownTimeout string `json:"shutdown_timeout"`
    MaxRetries      int    `json:"max_retries"`
    RetryDelay      string `json:"retry_delay"`
}
```

### Configuration Resolution Process

1. **Pool Loading**: Orchestrator reads `pool.yaml` and registers agent types with Support service
2. **Cell Loading**: Orchestrator reads `cells.yaml` and stores pipeline configurations  
3. **Agent Registration**: Agent connects and registers with its type and capabilities
4. **Configuration Request**: Agent calls `GetAgentCellConfig(agentID)` 
5. **Configuration Lookup**: Support service finds matching cell configuration
6. **Configuration Merge**: Type defaults merged with instance-specific settings
7. **State Transition**: Agent moves from `installed` → `configured`

### Dynamic Configuration Updates

The Support service supports runtime configuration updates:

```bash
# Update agent configuration
curl -X POST http://localhost:9000/rpc \
  -d '{
    "method": "update_agent_config",
    "params": {
      "agent_id": "text-transformer-demo-001",
      "config": {
        "transformation": "lowercase",
        "batch_size": 50
      }
    }
  }'
```

Agents can subscribe to configuration updates and apply changes without restart.

## Summary

Gox's architecture centers on the revolutionary **Agent Framework** that eliminates infrastructure complexity:

**For Developers**: Write only business logic - framework handles everything else
**For Operations**: Simple YAML configuration defines entire system behavior  
**For Scale**: Same agent code works from single-machine to distributed deployment

**The Result**: Complex distributed processing systems become as simple to develop as single-threaded applications, while retaining all benefits of distributed architecture.