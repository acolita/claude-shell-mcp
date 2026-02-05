// claude-shell-mcp is an MCP server providing persistent, interactive shell sessions.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/logging"
	"github.com/acolita/claude-shell-mcp/internal/mcp"
)

// Version information - set at build time.
var (
	Version   = "1.5.5"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

func main() {
	var (
		configPath  string
		mode        string
		showVersion bool
		debug       bool
	)

	flag.StringVar(&configPath, "config", "", "Path to configuration file")
	flag.StringVar(&mode, "mode", "", "Run mode: 'local' or 'ssh' (overrides config)")
	flag.BoolVar(&showVersion, "version", false, "Show version information")
	flag.BoolVar(&debug, "debug", false, "Enable debug mode with verbose PTY logging")
	flag.Parse()

	if showVersion {
		fmt.Printf("claude-shell-mcp version %s\n", Version)
		fmt.Printf("  Build time: %s\n", BuildTime)
		fmt.Printf("  Git commit: %s\n", GitCommit)
		os.Exit(0)
	}

	// Load configuration
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Override mode from command line if provided
	if mode != "" {
		cfg.Mode = mode
	}

	// Enable debug mode if flag is set
	if debug {
		cfg.Logging.Level = "debug"
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid configuration: %v\n", err)
		os.Exit(1)
	}

	// Setup logging
	logging.Setup(cfg.Logging.Level, cfg.Logging.Sanitize)

	slog.Info("starting claude-shell-mcp",
		slog.String("version", Version),
		slog.String("mode", cfg.Mode),
	)

	// Create MCP server
	server := mcp.NewServer(cfg)

	// Set up config hot-reload if config file was provided
	var configWatcher *config.Watcher
	if configPath != "" {
		var watcherErr error
		configWatcher, watcherErr = config.NewWatcher(configPath, func(newCfg *config.Config) {
			// Apply command line overrides to new config
			if mode != "" {
				newCfg.Mode = mode
			}
			if debug {
				newCfg.Logging.Level = "debug"
			}
			server.UpdateConfig(newCfg)
		})
		if watcherErr != nil {
			slog.Warn("config hot-reload disabled",
				slog.String("error", watcherErr.Error()),
			)
		} else {
			slog.Info("config hot-reload enabled",
				slog.String("path", configPath),
			)
		}
	}

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		slog.Info("received shutdown signal")
		if configWatcher != nil {
			configWatcher.Close()
		}
		os.Exit(0)
	}()

	// Run the server
	if err := server.Run(); err != nil {
		slog.Error("server error", slog.String("error", err.Error()))
		if configWatcher != nil {
			configWatcher.Close()
		}
		os.Exit(1)
	}
}
