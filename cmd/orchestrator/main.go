package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tenzoki/gox/internal/client"
	"github.com/tenzoki/gox/internal/orchestrator"
)

func main() {
	var (
		supportAddr = flag.String("support", "localhost:9000", "Support service address")
		cellID      = flag.String("cell", "pipeline:file-transform", "Cell ID to orchestrate")
		debug       = flag.Bool("debug", false, "Enable debug logging")
		dryRun      = flag.Bool("dry-run", false, "Show dependency order without starting pipeline")
		timeout     = flag.Duration("timeout", 30*time.Second, "Startup timeout per agent")
	)
	flag.Parse()

	if *debug {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
		log.Printf("Starting orchestrator for cell: %s", *cellID)
	}

	// Create orchestrator configuration
	config := orchestrator.OrchestrationConfig{
		SupportAddress:      *supportAddr,
		Debug:               *debug,
		StartupTimeout:      *timeout,
		MaxRetries:          3,
		RetryDelay:          5 * time.Second,
		HealthCheckInterval: 10 * time.Second,
	}

	// Create orchestrator
	orch, err := orchestrator.NewPipelineOrchestrator(config)
	if err != nil {
		log.Fatalf("Failed to create orchestrator: %v", err)
	}
	defer orch.Shutdown()

	// Load dependencies from support service
	if err := loadDependenciesFromCell(orch, *cellID); err != nil {
		log.Fatalf("Failed to load dependencies: %v", err)
	}

	// Get pipeline status for display
	status := orch.GetPipelineStatus()
	log.Printf("Loaded %d agents with dependencies", len(status))

	// Display dependency information
	displayDependencyInfo(status)

	if *dryRun {
		log.Println("Dry run complete - exiting without starting pipeline")
		return
	}

	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Printf("Received signal %s - initiating graceful shutdown", sig)
		cancel()
	}()

	// Start pipeline
	log.Println("Starting pipeline...")
	if err := orch.StartPipeline(ctx, *timeout); err != nil {
		log.Fatalf("Failed to start pipeline: %v", err)
	}

	log.Println("Pipeline started successfully - monitoring agents...")

	// Monitor pipeline until shutdown signal
	monitorPipeline(ctx, orch)

	// Graceful shutdown
	log.Println("Shutting down pipeline...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := orch.StopPipeline(shutdownCtx, 15*time.Second); err != nil {
		log.Printf("Warning: Pipeline shutdown had errors: %v", err)
	} else {
		log.Println("Pipeline shutdown completed successfully")
	}
}

func loadDependenciesFromCell(orch *orchestrator.PipelineOrchestrator, cellID string) error {
	// Create support client to load cell configuration
	supportClient := client.NewSupportClient("localhost:9000", false)
	if err := supportClient.Connect(); err != nil {
		return err
	}
	defer supportClient.Disconnect()

	// Get dependencies from support service
	dependencies, err := supportClient.GetPipelineDependencies(cellID)
	if err != nil {
		return err
	}

	// Convert to orchestrator format
	orchestratorDeps := make([]orchestrator.DependencyConfig, len(dependencies))
	for i, dep := range dependencies {
		orchestratorDeps[i] = orchestrator.DependencyConfig{
			AgentID:      dep.AgentID,
			Dependencies: dep.Dependencies,
			StartTimeout: "30s",
			Retries:      3,
		}
	}

	return orch.LoadDependencies(orchestratorDeps)
}

func displayDependencyInfo(status map[string]*orchestrator.AgentNode) {
	log.Println("\n=== Dependency Graph ===")

	for agentID, agent := range status {
		if len(agent.Dependencies) == 0 {
			log.Printf("📦 %s (%s) - No dependencies", agentID, agent.AgentType)
		} else {
			log.Printf("📦 %s (%s) - Depends on: %v", agentID, agent.AgentType, agent.Dependencies)
		}

		if len(agent.Dependents) > 0 {
			log.Printf("   └── Required by: %v", agent.Dependents)
		}
	}
	log.Println()
}

func monitorPipeline(ctx context.Context, orch *orchestrator.PipelineOrchestrator) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			status := orch.GetPipelineStatus()
			logPipelineStatus(status)
		}
	}
}

func logPipelineStatus(status map[string]*orchestrator.AgentNode) {
	log.Println("\n=== Pipeline Status ===")

	stateCount := make(map[string]int)
	for agentID, agent := range status {
		stateCount[string(agent.State)]++

		var stateEmoji string
		switch agent.State {
		case "installed":
			stateEmoji = "📦"
		case "configured":
			stateEmoji = "⚙️"
		case "ready":
			stateEmoji = "✅"
		case "running":
			stateEmoji = "🏃"
		case "paused":
			stateEmoji = "⏸️"
		case "stopped":
			stateEmoji = "⏹️"
		case "error":
			stateEmoji = "❌"
		default:
			stateEmoji = "❓"
		}

		timeSinceLastPing := ""
		if !agent.LastPing.IsZero() {
			timeSinceLastPing = " (last ping: " + time.Since(agent.LastPing).Round(time.Second).String() + " ago)"
		}

		log.Printf("%s %s: %s%s", stateEmoji, agentID, agent.State, timeSinceLastPing)
	}

	// Summary
	log.Printf("\nSummary:")
	for state, count := range stateCount {
		log.Printf("  %s: %d", state, count)
	}
	log.Println()
}
