// Package main is the entry point for the OpsAgent API server.
// It serves the REST API consumed by the Web console (health, topology,
// changes, agents, RCA analysis).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/interfaces"
)

func main() {
	// Load settings. config.Get() initializes with defaults and does not
	// require an API key — the API server itself doesn't call the LLM,
	// it only exposes data and forwards analysis requests.
	settings := config.Get()

	server, err := interfaces.NewAPIServer(settings)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create API server: %v\n", err)
		os.Exit(1)
	}

	// Handle graceful shutdown on SIGINT / SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		fmt.Printf("OpsAgent API server listening on %s:%d\n",
			settings.Server.Host, settings.Server.Port)
		fmt.Println("Endpoints:")
		fmt.Println("  GET  /api/v1/health")
		fmt.Println("  GET  /api/v1/topology")
		fmt.Println("  GET  /api/v1/changes")
		fmt.Println("  GET  /api/v1/agents")
		fmt.Println("  POST /api/v1/rca/analyze")
		if err := server.Start(); err != nil {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		fmt.Println("\nShutting down API server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		fmt.Println("API server stopped.")
	case err := <-errCh:
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
