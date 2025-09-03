package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tenzoki/gox/internal/agent"
)

func main() {
	// Get agent configuration from environment/CLI
	agentID := agent.GetAgentID("adapter")
	agentType := agent.GetAgentType("adapter")
	debug := agent.GetDebugFromEnv()
	supportAddress := agent.GetEnvConfig("SUPPORT_ADDRESS", "localhost:9000")

	log.Printf("Starting adapter agent: %s", agentID)

	// Create agent configuration
	config := agent.AgentConfig{
		ID:             agentID,
		AgentType:      agentType,
		Debug:          debug,
		SupportAddress: supportAddress,
		Capabilities: []string{
			"text-transform",
			"format-conversion",
			"json-processing",
			"csv-processing",
			"base64-encoding",
		},
		RebootTimeout: 30 * time.Second,
	}

	// Create adapter agent
	adapterAgent, err := agent.NewAdapterAgent(config)
	if err != nil {
		log.Fatalf("Failed to create adapter agent: %v", err)
	}

	// Setup signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Printf("Received signal: %v, shutting down...", sig)
		cancel()
	}()

	// Start the adapter agent
	if err := adapterAgent.Start(ctx); err != nil {
		log.Printf("Adapter agent stopped with error: %v", err)
	}

	// Cleanup
	if err := adapterAgent.Stop(); err != nil {
		log.Printf("Error during adapter agent cleanup: %v", err)
	}

	log.Printf("Adapter agent %s shut down successfully", agentID)
}
