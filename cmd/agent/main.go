// Package main is the entry point for the OpsAgent coordinator service.
// It initializes the multi-agent system (coordinator + specialized agents)
// and keeps them ready to receive tasks via the API or alert webhooks.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Havens-blog/OpsAgent/internal/agent"
	"github.com/Havens-blog/OpsAgent/internal/config"
)

func main() {
	settings, err := config.Load("./config")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		fmt.Fprintln(os.Stderr, "Hint: set ANTHROPIC_API_KEY (or LLM_API_KEY) and ensure config/settings.yaml exists.")
		os.Exit(1)
	}

	coordinator, err := agent.NewCoordinatorAgent(settings)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create coordinator agent: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Printf("OpsAgent coordinator '%s' initialized and idle.\n", coordinator.GetConfig().Name)
	fmt.Println("Trigger analysis via the API server (cmd/api) or Alertmanager webhooks.")
	<-ctx.Done()

	fmt.Println("\nShutting down coordinator...")
	_ = coordinator.Shutdown(context.Background())
	fmt.Println("Coordinator stopped.")
}
