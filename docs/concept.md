# Gox: Philosophy and Core Concepts

## Philosophy: Zero-Boilerplate Agent Development

Gox revolutionizes distributed processing by eliminating infrastructure complexity through its **Agent Framework**. The core principle: developers should write only **business logic** - all infrastructure concerns are handled automatically.

**The Problem**: Traditional agent systems require 100+ lines of boilerplate per agent for connections, lifecycle management, message handling, and error recovery.

**The Solution**: Gox's Agent Framework reduces this to a single line: `agent.Run(&MyAgent{}, "agent-type")`

### Core Design Principles

#### **1. Framework-Driven Development**
The Agent Framework handles all infrastructure concerns:
- **Lifecycle management** - BaseAgent initialization, state transitions
- **Connection setup** - Support/Broker service connections, retry logic
- **Message processing** - Ingress/egress handling, routing, acknowledgments
- **Signal handling** - Graceful shutdown, cleanup, error recovery

**Result**: Agents contain only business logic - framework provides everything else.

#### **2. Declarative Pipeline Configuration**
```yaml
# Define WHAT agents do, not HOW they do it
agents:
  - id: "processor-001"
    dependencies: ["ingester-001"]  # WHEN to start
    ingress: "sub:raw-data"          # WHAT to consume  
    egress: "pub:processed"          # WHERE to send results
```
The framework automatically handles dependency resolution, startup ordering, and message routing.

#### **3. Self-Contained Infrastructure**
No external dependencies - everything runs in a single `gox` binary:
- **Support Service** - Agent registry and configuration
- **Broker Service** - Message routing and delivery
- **Orchestrator** - Dependency coordination

#### **4. Universal Communication Patterns**
- `file:path/*.ext` - Filesystem integration
- `pub:topic`/`sub:topic` - Event broadcasting
- `pipe:name` - Point-to-point messaging

Agents use these patterns declaratively - no network programming required.

#### **5. Built-in Observability**
Every message includes tracing metadata automatically:
```go
type Envelope struct {
    ID, TraceID, CorrelationID string
    Route []string  // Full message path
    HopCount int    // Number of processing steps
    Timestamp time.Time
    // ... business payload
}
```

## Core Concepts

### **Agent Framework**
The revolutionary infrastructure that eliminates boilerplate:

**Traditional Agent** (120+ lines):
```go
// BaseAgent setup (37 lines)
config := BaseAgentConfig{...}
baseAgent := NewBaseAgent(config)
if err := baseAgent.Start(ctx); err != nil { ... }

// Connection management (25 lines)
ingressConn := parseIngress(...)
egressConn := parseEgress(...)
if err := setupConnections(...); err != nil { ... }

// Signal handling (20 lines) 
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

// Message processing loop (30+ lines)
for { select { case msg := <-msgChan: ... } }

// Custom business logic mixed throughout
```

**Framework Agent** (3 lines):
```go
type MyAgent struct { agent.DefaultAgentRunner }
func (a *MyAgent) ProcessMessage(msg *client.BrokerMessage, base *agent.BaseAgent) (*client.BrokerMessage, error) { /* business logic */ }
func main() { agent.Run(&MyAgent{}, "my-agent") }
```

### **Agent Types vs Instances**
- **Agent Type** (pool.yaml): Reusable template defining capabilities and binary
- **Agent Instance** (cells.yaml): Specific deployment with unique ID and configuration

This separation enables scaling - deploy multiple instances of the same type.

### **Cell (Pipeline Definition)**
A complete processing workflow:
```yaml
cell:
  id: "data-processing-pipeline"
  agents:
    - id: "ingester-001"     # Unique instance
      agent_type: "file-ingester"  # References pool.yaml
      dependencies: []        # Starts first
      ingress: "file:input/*.txt"
      egress: "pub:raw-files"
      
    - id: "processor-001"
      agent_type: "text-transformer"
      dependencies: ["ingester-001"]  # Waits for ingester
      ingress: "sub:raw-files"
      egress: "pipe:processed-data"
```

### **Infrastructure Services**
**Orchestrator**: Dependency resolution and startup coordination
- Topological sorting prevents deadlocks
- Ensures agents start only when dependencies are ready
- Handles graceful shutdown in reverse dependency order

**Support Service** (:9000): Agent registry and configuration
- Agents self-register with capabilities
- Distributes configuration from cells.yaml
- Provides service discovery and health monitoring

**Broker Service** (:9001): Message routing
- Implements pub/sub topics and point-to-point pipes
- JSON-over-TCP with automatic connection management
- Message buffering and delivery guarantees

### **Message Envelope**
Automatically wraps all inter-agent communication:
```go
type Envelope struct {
    ID, TraceID, CorrelationID string    // Tracing
    Source, Destination string           // Routing
    Timestamp time.Time                  // Timing
    Payload json.RawMessage              // Business data
    Route []string                       // Message path
    HopCount int                         // Processing depth
}
```
Agents never handle envelopes directly - framework manages all metadata automatically.

## Design Patterns

### **Framework-Driven Architecture**
The Agent Framework eliminates the need for agents to implement infrastructure patterns:
```go
// Traditional: Agent must implement all patterns
type TraditionalAgent struct {
    baseAgent *BaseAgent
    ingress   IngressHandler
    egress    EgressHandler
    // ... 100+ lines of setup and management
}

// Framework: Agent implements only business logic
type FrameworkAgent struct {
    agent.DefaultAgentRunner  // Inherit all patterns
}
func (a *FrameworkAgent) ProcessMessage(...) { /* business logic only */ }
```

### **Dependency-Driven Orchestration**
Startup order determined by dependencies, not timing:
```yaml
agents:
  - id: "source"        # dependencies: [] → starts first
  - id: "processor-1"   # dependencies: ["source"] → waits for source
  - id: "processor-2"   # dependencies: ["source"] → parallel with processor-1
  - id: "aggregator"    # dependencies: ["processor-1", "processor-2"] → waits for both
```
Enables parallel processing with guaranteed correctness.

### **Reactive Message Flow**
Agents react to messages, framework handles routing:
- Publishers send to topics/pipes without knowing consumers
- Subscribers receive from topics/pipes without knowing producers  
- Framework manages all connection lifecycle and error handling

### **Configuration as Contract**
YAML files define the complete system behavior:
- **pool.yaml**: Agent types and capabilities (development contract)
- **cells.yaml**: Pipeline instances and dependencies (operations contract)
- **gox.yaml**: Infrastructure settings (deployment contract)

## Scalability and Evolution

### **Built for Scale**
- **Stateless agents** scale horizontally automatically
- **Framework handles distribution** - agents can run anywhere
- **Dynamic dependencies** - agents connect as they come online
- **Multiple deployment strategies**: call (embedded), spawn (isolated), await (distributed)

### **Operational Simplicity**
- **Single binary** - no external dependencies
- **YAML configuration** - human-readable contracts
- **Framework abstraction** - agents don't handle infrastructure
- **Built-in testing** - comprehensive integration tests

### **Future Evolution**
The Agent Framework pattern enables easy extension:
- **New agent types** - implement ProcessMessage() interface
- **Custom patterns** - extend communication handlers
- **Enhanced observability** - framework provides hooks
- **Protocol evolution** - envelope abstraction enables changes

---

**The Gox Philosophy**: Complex distributed systems should be **simple to develop**. The Agent Framework achieves this by handling all infrastructure complexity automatically, letting developers focus entirely on business value.

**Result**: `agent.Run(&MyAgent{}, "agent-type")` - that's all the infrastructure code you'll ever write.