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
	// Load settings from config directory. Try to load full config (including LLM)
	// but fall back to defaults if LLM is not configured — the API server should
	// remain functional for endpoints that don't require LLM (topology, RCA, etc.).
	settings, err := config.Load("./config")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load config from ./config: %v\n", err)
		fmt.Fprintln(os.Stderr, "Using default configuration (chat assistant will not be available)")
		settings = config.Get()
	}

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
		fmt.Println("  POST /api/v1/chat")
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
