package agent

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// AgentState represents the lifecycle state of an agent
type AgentState string

const (
	StateInstalled  AgentState = "installed"  // Agent connected to Support, registered
	StateConfigured AgentState = "configured" // Agent received cell-specific configuration
	StateReady      AgentState = "ready"      // Agent initialized, subscribed, waiting for start
	StateRunning    AgentState = "running"    // Agent actively processing messages
	StatePaused     AgentState = "paused"     // Agent suspended, not processing new messages
	StateStopped    AgentState = "stopped"    // Agent cleanly shut down
	StateError      AgentState = "error"      // Agent in error state, needs intervention
)

// StateTransitions defines valid state transitions
var StateTransitions = map[AgentState][]AgentState{
	StateInstalled:  {StateConfigured, StateError},
	StateConfigured: {StateReady, StateError},
	StateReady:      {StateRunning, StateStopped, StateError},
	StateRunning:    {StatePaused, StateStopped, StateError},
	StatePaused:     {StateRunning, StateStopped, StateError},
	StateStopped:    {},                              // Terminal state
	StateError:      {StateConfigured, StateStopped}, // Can recover or stop
}

// LifecycleManager handles agent state transitions
type LifecycleManager struct {
	agentID       string
	currentState  AgentState
	stateMutex    sync.RWMutex
	stateChanged  chan StateChange
	listeners     []func(StateChange)
	supportNotify func(string, string) error // Callback to notify support service
}

// StateChange represents a state transition event
type StateChange struct {
	AgentID   string     `json:"agent_id"`
	FromState AgentState `json:"from_state"`
	ToState   AgentState `json:"to_state"`
	Timestamp time.Time  `json:"timestamp"`
	Reason    string     `json:"reason,omitempty"`
}

// NewLifecycleManager creates a new lifecycle manager
func NewLifecycleManager(agentID string, supportNotify func(string, string) error) *LifecycleManager {
	return &LifecycleManager{
		agentID:       agentID,
		currentState:  StateInstalled, // Initial state
		stateChanged:  make(chan StateChange, 10),
		listeners:     make([]func(StateChange), 0),
		supportNotify: supportNotify,
	}
}

// GetState returns the current agent state (thread-safe)
func (lm *LifecycleManager) GetState() AgentState {
	lm.stateMutex.RLock()
	defer lm.stateMutex.RUnlock()
	return lm.currentState
}

// SetState transitions to a new state with validation
func (lm *LifecycleManager) SetState(newState AgentState, reason string) error {
	lm.stateMutex.Lock()
	oldState := lm.currentState

	// Validate transition
	validTransitions, exists := StateTransitions[oldState]
	if !exists {
		lm.stateMutex.Unlock()
		return fmt.Errorf("invalid current state: %s", oldState)
	}

	valid := false
	for _, validState := range validTransitions {
		if validState == newState {
			valid = true
			break
		}
	}

	if !valid {
		lm.stateMutex.Unlock()
		return fmt.Errorf("invalid state transition: %s → %s", oldState, newState)
	}

	// Perform transition
	lm.currentState = newState
	lm.stateMutex.Unlock()

	// Create state change event
	change := StateChange{
		AgentID:   lm.agentID,
		FromState: oldState,
		ToState:   newState,
		Timestamp: time.Now(),
		Reason:    reason,
	}

	// Notify support service
	if lm.supportNotify != nil {
		if err := lm.supportNotify(lm.agentID, string(newState)); err != nil {
			// Log error but don't fail the state transition
			fmt.Printf("Warning: failed to notify support of state change: %v\n", err)
		}
	}

	// Notify local listeners
	for _, listener := range lm.listeners {
		go listener(change) // Non-blocking
	}

	// Send to channel (non-blocking)
	select {
	case lm.stateChanged <- change:
	default:
		// Channel full, skip (we have other notification methods)
	}

	return nil
}

// WaitForState blocks until the agent reaches the specified state or timeout
func (lm *LifecycleManager) WaitForState(targetState AgentState, timeout time.Duration) error {
	// Check if already in target state
	if lm.GetState() == targetState {
		return nil
	}

	// Wait for state change
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for state %s (current: %s)", targetState, lm.GetState())
		case change := <-lm.stateChanged:
			if change.ToState == targetState {
				return nil
			}
			if change.ToState == StateError {
				return fmt.Errorf("agent entered error state while waiting for %s", targetState)
			}
		}
	}
}

// AddStateListener adds a callback for state changes
func (lm *LifecycleManager) AddStateListener(listener func(StateChange)) {
	lm.stateMutex.Lock()
	defer lm.stateMutex.Unlock()
	lm.listeners = append(lm.listeners, listener)
}

// GetStateHistory returns recent state changes from the channel
func (lm *LifecycleManager) GetStateHistory() []StateChange {
	changes := make([]StateChange, 0)

	// Drain the channel without blocking
	for {
		select {
		case change := <-lm.stateChanged:
			changes = append(changes, change)
		default:
			return changes
		}
	}
}

// CanTransitionTo checks if a state transition is valid
func (lm *LifecycleManager) CanTransitionTo(targetState AgentState) bool {
	currentState := lm.GetState()
	validTransitions, exists := StateTransitions[currentState]
	if !exists {
		return false
	}

	for _, validState := range validTransitions {
		if validState == targetState {
			return true
		}
	}
	return false
}
