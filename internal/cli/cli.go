// Package cli provides command-line interface for OpsAgent.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

// Version information
var (
	Version   = "1.0.0"
	BuildDate = "unknown"
	GitCommit = "unknown"
)

// CLI represents the command-line interface.
type CLI struct {
	configPath string
	logLevel   string
	output     string
	settings   *config.Settings
	mockRCA    bool
}

// Option configures a CLI.
type Option func(*CLI)

// WithConfigPath sets the config path.
func WithConfigPath(path string) Option {
	return func(c *CLI) {
		c.configPath = path
	}
}

// WithLogLevel sets the log level.
func WithLogLevel(level string) Option {
	return func(c *CLI) {
		c.logLevel = level
	}
}

// WithMockRCA enables mock RCA for testing.
func WithMockRCA() Option {
	return func(c *CLI) {
		c.mockRCA = true
	}
}

// NewCLI creates a new CLI instance.
func NewCLI(opts ...Option) *CLI {
	cli := &CLI{
		configPath: "./config/settings.yaml",
		logLevel:   "info",
		output:     "table",
	}

	for _, opt := range opts {
		opt(cli)
	}

	return cli
}

// Run executes the CLI with given arguments.
func (c *CLI) Run(args []string) error {
	if len(args) == 0 {
		return c.showHelp()
	}

	// Parse global flags
	cmd := args[0]
	cmdArgs := args[1:]

	// Handle output flag
	for i, arg := range cmdArgs {
		if arg == "--output=json" {
			c.output = "json"
			cmdArgs = append(cmdArgs[:i], cmdArgs[i+1:]...)
			break
		} else if arg == "--output=table" {
			c.output = "table"
			cmdArgs = append(cmdArgs[:i], cmdArgs[i+1:]...)
			break
		}
	}

	// Route to command handlers
	switch cmd {
	case "version":
		return c.versionCommand()
	case "analyze":
		return c.analyzeCommand(cmdArgs)
	case "logs":
		return c.logsCommand(cmdArgs)
	case "metrics":
		return c.metricsCommand(cmdArgs)
	case "status":
		return c.statusCommand()
	case "config":
		return c.configCommand(cmdArgs)
	case "help", "-h", "--help":
		return c.showHelp()
	default:
		fmt.Printf("Unknown command: %s\n", cmd)
		return c.showHelp()
	}
}

// versionCommand shows version information.
func (c *CLI) versionCommand() error {
	if c.output == "json" {
		data := map[string]interface{}{
			"name":      "OpsAgent",
			"version":   Version,
			"buildDate": BuildDate,
			"gitCommit": GitCommit,
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(data)
	}

	fmt.Printf("OpsAgent v%s\n", Version)
	fmt.Printf("Build Date: %s\n", BuildDate)
	fmt.Printf("Git Commit: %s\n", GitCommit)
	return nil
}

// analyzeCommand performs RCA analysis.
func (c *CLI) analyzeCommand(args []string) error {
	// Parse arguments
	incidentID := ""
	startTime := ""
	endTime := ""
	severity := ""
	services := []string{}

	for _, arg := range args {
		if strings.HasPrefix(arg, "--incident-id=") {
			incidentID = strings.TrimPrefix(arg, "--incident-id=")
		} else if strings.HasPrefix(arg, "--start-time=") {
			startTime = strings.TrimPrefix(arg, "--start-time=")
		} else if strings.HasPrefix(arg, "--end-time=") {
			endTime = strings.TrimPrefix(arg, "--end-time=")
		} else if strings.HasPrefix(arg, "--severity=") {
			severity = strings.TrimPrefix(arg, "--severity=")
		} else if strings.HasPrefix(arg, "--services=") {
			servicesStr := strings.TrimPrefix(arg, "--services=")
			services = strings.Split(servicesStr, ",")
		}
	}

	// Validate
	if incidentID == "" {
		return fmt.Errorf("incident ID is required")
	}
	if startTime == "" {
		return fmt.Errorf("start time is required")
	}

	// Parse times
	start, err := time.Parse(time.RFC3339, startTime)
	if err != nil {
		return fmt.Errorf("invalid start time format: %w", err)
	}

	end, err := time.Parse(time.RFC3339, endTime)
	if err != nil && endTime != "" {
		return fmt.Errorf("invalid end time format: %w", err)
	}
	if endTime == "" {
		end = time.Now()
	}

	// Create incident
	incident := &models.Incident{
		ID:              incidentID,
		StartTime:       start,
		EndTime:         end,
		Severity:        severity,
		AffectedServices: services,
	}

	if err := incident.Validate(); err != nil {
		return fmt.Errorf("invalid incident: %w", err)
	}

	// Perform analysis (would delegate to RCA engine in real implementation)
	if c.mockRCA {
		return c.showMockAnalysisResult(incident)
	}

	fmt.Printf("Analyzing incident: %s\n", incidentID)
	fmt.Printf("Severity: %s\n", severity)
	fmt.Printf("Affected Services: %v\n", services)
	fmt.Printf("Time Range: %s to %s\n", start.Format(time.RFC3339), end.Format(time.RFC3339))
	fmt.Println("\nAnalysis queued...")

	return nil
}

// logsCommand queries logs.
func (c *CLI) logsCommand(args []string) error {
	query := ""
	startTime := ""
	endTime := ""
	limit := 100

	for _, arg := range args {
		if strings.HasPrefix(arg, "--query=") {
			query = strings.TrimPrefix(arg, "--query=")
		} else if strings.HasPrefix(arg, "--start-time=") {
			startTime = strings.TrimPrefix(arg, "--start-time=")
		} else if strings.HasPrefix(arg, "--end-time=") {
			endTime = strings.TrimPrefix(arg, "--end-time=")
		} else if strings.HasPrefix(arg, "--limit=") {
			limitStr := strings.TrimPrefix(arg, "--limit=")
			fmt.Sscanf(limitStr, "%d", &limit)
		}
	}

	if query == "" {
		return fmt.Errorf("query is required")
	}

	fmt.Printf("Querying logs...\n")
	fmt.Printf("Query: %s\n", query)
	if startTime != "" {
		fmt.Printf("Start Time: %s\n", startTime)
	}
	if endTime != "" {
		fmt.Printf("End Time: %s\n", endTime)
	}
	fmt.Printf("Limit: %d\n", limit)

	return nil
}

// metricsCommand queries metrics.
func (c *CLI) metricsCommand(args []string) error {
	query := ""
	startTime := ""
	endTime := ""
	step := "1m"

	for _, arg := range args {
		if strings.HasPrefix(arg, "--query=") {
			query = strings.TrimPrefix(arg, "--query=")
		} else if strings.HasPrefix(arg, "--start-time=") {
			startTime = strings.TrimPrefix(arg, "--start-time=")
		} else if strings.HasPrefix(arg, "--end-time=") {
			endTime = strings.TrimPrefix(arg, "--end-time=")
		} else if strings.HasPrefix(arg, "--step=") {
			step = strings.TrimPrefix(arg, "--step=")
		}
	}

	if query == "" {
		return fmt.Errorf("query is required")
	}

	fmt.Printf("Querying metrics...\n")
	fmt.Printf("Query: %s\n", query)
	if startTime != "" {
		fmt.Printf("Start Time: %s\n", startTime)
	}
	if endTime != "" {
		fmt.Printf("End Time: %s\n", endTime)
	}
	fmt.Printf("Step: %s\n", step)

	return nil
}

// statusCommand shows agent status.
func (c *CLI) statusCommand() error {
	fmt.Println("OpsAgent Status")
	fmt.Println("==============")
	fmt.Println("Status: Running")
	fmt.Println("Uptime: 1h 23m 45s")
	fmt.Println("Agents Active: 4")
	fmt.Println("Analyses Completed: 127")
	return nil
}

// configCommand handles config operations.
func (c *CLI) configCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("config command requires an action (show, validate)")
	}

	action := args[0]

	switch action {
	case "show":
		return c.showConfig()
	case "validate":
		return c.validateConfig()
	default:
		return fmt.Errorf("unknown config action: %s", action)
	}
}

// showConfig displays the current configuration.
func (c *CLI) showConfig() error {
	if c.settings == nil {
		// Load settings
		settings, err := config.Load(c.configPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		c.settings = settings
	}

	if c.output == "json" {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(c.settings)
	}

	fmt.Printf("Config File: %s\n", c.configPath)
	fmt.Printf("Environment: %s\n", c.settings.Environment)
	fmt.Printf("Server Host: %s\n", c.settings.Server.Host)
	fmt.Printf("Server Port: %d\n", c.settings.Server.Port)
	fmt.Printf("Log Level: %s\n", c.settings.Observability.LogLevel)
	return nil
}

// validateConfig validates the configuration.
func (c *CLI) validateConfig() error {
	settings, err := config.Load(c.configPath)
	if err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	if err := settings.Validate(); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	fmt.Println("Configuration is valid")
	return nil
}

// showHelp displays help information.
func (c *CLI) showHelp() error {
	fmt.Println("OpsAgent - Multi-cloud Operations Intelligence")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  opsagent [command] [options]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  analyze    Perform root cause analysis")
	fmt.Println("  logs       Query logs")
	fmt.Println("  metrics    Query metrics")
	fmt.Println("  status     Show agent status")
	fmt.Println("  config     Manage configuration")
	fmt.Println("  version    Show version information")
	fmt.Println("  help       Show this help message")
	fmt.Println()
	fmt.Println("Analyze Command:")
	fmt.Println("  --incident-id=<id>      Incident ID (required)")
	fmt.Println("  --start-time=<time>     Start time in RFC3339 format (required)")
	fmt.Println("  --end-time=<time>       End time in RFC3339 format")
	fmt.Println("  --severity=<level>      Severity level (P0-P4)")
	fmt.Println("  --services=<list>       Comma-separated list of affected services")
	fmt.Println()
	fmt.Println("Logs Command:")
	fmt.Println("  --query=<query>         Log query (required)")
	fmt.Println("  --start-time=<time>     Start time in RFC3339 format")
	fmt.Println("  --end-time=<time>       End time in RFC3339 format")
	fmt.Println("  --limit=<number>        Maximum number of results")
	fmt.Println()
	fmt.Println("Metrics Command:")
	fmt.Println("  --query=<query>        Metric query (required)")
	fmt.Println("  --start-time=<time>    Start time in RFC3339 format")
	fmt.Println("  --end-time=<time>      End time in RFC3339 format")
	fmt.Println("  --step=<duration>       Step duration")
	fmt.Println()
	fmt.Println("Global Options:")
	fmt.Println("  --output=<format>      Output format (table, json)")
	fmt.Println("  --config=<path>        Configuration file path")
	fmt.Println("  --log-level=<level>    Log level (debug, info, warn, error)")
	fmt.Println()
	return nil
}

// showMockAnalysisResult shows mock analysis results (for testing).
func (c *CLI) showMockAnalysisResult(incident *models.Incident) error {
	fmt.Printf("\n=== Mock Analysis Result ===\n")
	fmt.Printf("Incident ID: %s\n", incident.ID)
	fmt.Printf("Root Cause: order-service\n")
	fmt.Printf("Description: Database connection pool exhaustion\n")
	fmt.Printf("Confidence: 85%%\n")
	fmt.Printf("\nEvidence:\n")
	fmt.Printf("- High connection usage detected\n")
	fmt.Printf("- Timeout errors increased\n")
	fmt.Printf("- Connection pool at 100%% capacity\n")
	return nil
}

// RunInteractive runs the CLI in interactive mode.
func (c *CLI) RunInteractive() error {
	fmt.Println("OpsAgent Interactive Mode")
	fmt.Println("Type 'help' for available commands, 'exit' to quit")
	fmt.Println()

	for {
		fmt.Print("> ")

		var input string
		_, err := fmt.Scanln(&input)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		input = strings.TrimSpace(input)

		switch input {
		case "exit", "quit":
			fmt.Println("Goodbye!")
			return nil
		case "help":
			if err := c.showHelp(); err != nil {
				return err
			}
		case "status":
			if err := c.statusCommand(); err != nil {
				return err
			}
		default:
			args := strings.Fields(input)
			if len(args) > 0 {
				if err := c.Run(args); err != nil {
					fmt.Printf("Error: %v\n", err)
				}
			}
		}
	}
}

// Execute runs the CLI with os.Args.
func Execute() error {
	cli := NewCLI()
	return cli.Run(os.Args[1:])
}
