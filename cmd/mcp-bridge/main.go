package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/jamesprial/mcp-universal-bridge/internal/bridge"
	"github.com/jamesprial/mcp-universal-bridge/internal/config"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	// Build variables (set via ldflags)
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

var (
	serverURL   string
	configFile  string
	logLevel    string
	metricsPort int
	debug       bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "mcp-bridge",
		Short: "Universal MCP Bridge - Connect Claude Code to any MCP server",
		Long: `A high-performance bridge that connects Claude Code (via stdio) to any MCP server
using HTTP, SSE, or WebSocket transports. Written in Go for optimal performance.`,
		RunE: run,
	}
	
	// Add flags
	rootCmd.Flags().StringVarP(&serverURL, "server", "s", "", "MCP server URL")
	rootCmd.Flags().StringVarP(&configFile, "config", "c", "", "Configuration file path")
	rootCmd.Flags().StringVarP(&logLevel, "log-level", "l", "info", "Log level (debug, info, warn, error)")
	rootCmd.Flags().IntVarP(&metricsPort, "metrics-port", "m", 0, "Prometheus metrics port (0 to disable)")
	rootCmd.Flags().BoolVarP(&debug, "debug", "d", false, "Enable debug mode")
	
	// Add subcommands
	rootCmd.AddCommand(versionCmd())
	rootCmd.AddCommand(testCmd())
	
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	// Setup logging
	setupLogging()
	
	// Load configuration
	cfg, err := config.Load(configFile, serverURL)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	
	// Override with flags
	if metricsPort > 0 {
		cfg.Monitoring.MetricsPort = metricsPort
	}
	if debug {
		cfg.Monitoring.LogLevel = "debug"
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}
	
	log.Info().
		Str("version", Version).
		Str("server", cfg.Server.URL).
		Str("transport", cfg.Server.Transport.String()).
		Msg("Starting MCP Universal Bridge")
	
	// Create bridge
	b, err := bridge.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to create bridge: %w", err)
	}
	
	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	go func() {
		sig := <-sigChan
		log.Info().Str("signal", sig.String()).Msg("Shutting down gracefully")
		cancel()
		
		// Give it time to clean up
		<-sigChan
		log.Warn().Msg("Force shutdown")
		os.Exit(1)
	}()
	
	// Start bridge
	if err := b.Start(ctx); err != nil {
		return fmt.Errorf("bridge failed: %w", err)
	}
	
	// Clean shutdown
	return b.Close()
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("MCP Universal Bridge\n")
			fmt.Printf("  Version:    %s\n", Version)
			fmt.Printf("  Build Time: %s\n", BuildTime)
			fmt.Printf("  Git Commit: %s\n", GitCommit)
			fmt.Printf("  Go Version: %s\n", runtime.Version())
			fmt.Printf("  OS/Arch:    %s/%s\n", runtime.GOOS, runtime.GOARCH)
		},
	}
}

func testCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Test connectivity to MCP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			setupLogging()
			
			if serverURL == "" {
				return fmt.Errorf("server URL is required")
			}
			
			log.Info().Str("server", serverURL).Msg("Testing connectivity")
			
			// Load minimal config
			cfg := config.DefaultConfig()
			cfg.Server.URL = serverURL
			
			// Try to create session
			b, err := bridge.New(cfg)
			if err != nil {
				return fmt.Errorf("connection test failed: %w", err)
			}
			defer b.Close()
			
			log.Info().Msg("✅ Connection successful")
			return nil
		},
	}
	
	cmd.Flags().StringVarP(&serverURL, "server", "s", "", "MCP server URL (required)")
	_ = cmd.MarkFlagRequired("server")
	
	return cmd
}

func setupLogging() {
	// Configure zerolog
	if debug || logLevel == "debug" {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else if logLevel == "warn" {
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	} else if logLevel == "error" {
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
	
	// Use console writer for better readability
	if os.Getenv("MCP_LOG_FORMAT") != "json" {
		log.Logger = log.Output(zerolog.ConsoleWriter{
			Out:        os.Stderr,
			TimeFormat: "15:04:05",
		})
	} else {
		// JSON format for production
		zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs
	}
	
	// Add default fields
	log.Logger = log.With().
		Str("service", "mcp-bridge").
		Str("version", Version).
		Logger()
}