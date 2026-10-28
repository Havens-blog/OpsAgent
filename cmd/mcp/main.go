// MCP Server for OpsAgent
//
// This program runs OpsAgent as an MCP server, exposing its capabilities
// as tools that can be called from Claude Code or other MCP clients.
//
// Usage:
//
//	go run cmd/mcp/main.go --port 8081
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Havens-blog/OpsAgent/internal/agent"
	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/datasource/credentials"
	"github.com/Havens-blog/OpsAgent/internal/datasource/registry"
	"github.com/Havens-blog/OpsAgent/internal/mcp"
	"github.com/Havens-blog/OpsAgent/internal/rca/engine"
	"github.com/Havens-blog/OpsAgent/internal/rca/sources"
)

var (
	port       = flag.Int("port", 8081, "MCP server port")
	configFile = flag.String("config", "config/settings.yaml", "Config file path")
)

func main() {
	flag.Parse()

	// Load configuration
	settings, err := config.Load(*configFile)
	if err != nil {
		log.Printf("Warning: Failed to load config: %v", err)
		// Use Get() which returns settings with defaults
		settings = config.Get()
	}

	// Create credential provider
	credProvider := credentials.NewEnvProvider("OPSAGENT")

	// Create data source registry
	reg := registry.NewRegistry(credProvider)

	// Setup data sources from config
	ctx := context.Background()
	if err := reg.SetupFromConfig(ctx, settings.DataSources); err != nil {
		log.Printf("Warning: Failed to setup datasources: %v", err)
	}

	// Create MCP server
	server := mcp.NewServer(reg)

	// Wire the RCA data sources exactly like the REST API does: build the source
	// set from the environment ONCE and inject the topology, metric and log
	// dimensions together with whether each is real. Without this the tools have
	// no data source and must report that honestly instead of inventing results.
	dsCfg := agent.NewDataSourceConfigFromEnv()
	rcaSources, dsActive := agent.CreateRCASourcesWithFallback(dsCfg)
	server.SetTopologySource(rcaSources.TopologySource, dsActive["topology"])
	server.SetMetricSource(rcaSources.MetricSource, dsActive["metrics"])
	server.SetLogSource(rcaSources.LogSource, dsActive["logs"])
	log.Printf("RCA sources real: metrics=%v logs=%v topology=%v changes=%v",
		dsActive["metrics"], dsActive["logs"], dsActive["topology"], dsActive["changes"])

	// Build the real RCA engine over those same sources (mirrors
	// internal/interfaces/api.go), so opsagent_rca_analyze runs the identical
	// analysis as POST /api/v1/rca/analyze. On failure the engine stays
	// unwired and the tool says so rather than fabricating a report.
	traceSource := rcaSources.TraceSource
	if traceSource == nil {
		traceSource = sources.NewEmptyTraceSource()
	}
	rcaEngine, err := engine.NewEngine(
		engine.WithLogSource(rcaSources.LogSource),
		engine.WithMetricSource(rcaSources.MetricSource),
		engine.WithTraceSource(traceSource),
		engine.WithTopologySource(rcaSources.TopologySource),
		engine.WithChangeSource(rcaSources.ChangeSource),
	)
	if err != nil {
		log.Printf("Warning: RCA engine not wired into MCP (%v); opsagent_rca_analyze will report it honestly", err)
	} else {
		server.SetRCAEngine(rcaEngine)
	}

	// Start server
	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Starting OpsAgent MCP server on %s", addr)
	log.Printf("Available endpoints:")
	log.Printf("  - http://localhost%s/mcp/tools", addr)
	log.Printf("  - http://localhost%s/mcp/call", addr)

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := server.Start(addr); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-sigChan
	log.Println("Shutting down MCP server...")
}
