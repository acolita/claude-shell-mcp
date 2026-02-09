// claude-shell-mcp is an MCP server providing persistent, interactive shell sessions.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/acolita/claude-shell-mcp/internal/adapters/realdialog"
	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/logging"
	"github.com/acolita/claude-shell-mcp/internal/mcp"
)

// Version information - set at build time.
var (
	Version   = "1.10.0"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

func main() {
	var (
		configPath  string
		showVersion bool
		debug       bool
		formMode    bool
	)

	flag.StringVar(&configPath, "config", "", "Path to configuration file")
	flag.BoolVar(&showVersion, "version", false, "Show version information")
	flag.BoolVar(&debug, "debug", false, "Enable debug mode with verbose PTY logging")
	flag.BoolVar(&formMode, "form", false, "Run as TUI form helper (internal use)")
	flag.Parse()

	if formMode {
		runFormHelper()
	}

	if showVersion {
		printVersion()
	}

	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	cfg := loadConfig(configPath, debug)

	logging.Setup(cfg.Logging.Level, cfg.Logging.Sanitize)
	slog.Info("starting claude-shell-mcp", slog.String("version", Version))

	server := mcp.NewServer(cfg, mcp.WithConfigPath(configPath))
	watcher := setupConfigWatcher(configPath, debug, server)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		slog.Info("received shutdown signal")
		closeWatcher(watcher)
		os.Exit(0)
	}()

	if err := server.Run(); err != nil {
		slog.Error("server error", slog.String("error", err.Error()))
		closeWatcher(watcher)
		os.Exit(1)
	}
}

// runFormHelper runs the TUI form mode and exits. This is spawned by the
// MCP server's dialog provider in a separate terminal window.
func runFormHelper() {
	if err := realdialog.RunFormHelper(); err != nil {
		if formFile := os.Getenv("CLAUDE_SHELL_FORM_FILE"); formFile != "" {
			os.WriteFile(formFile+".done", []byte(err.Error()), 0600)
		}
		fmt.Fprintf(os.Stderr, "form error: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func printVersion() {
	fmt.Printf("claude-shell-mcp version %s\n", Version)
	fmt.Printf("  Build time: %s\n", BuildTime)
	fmt.Printf("  Git commit: %s\n", GitCommit)
	os.Exit(0)
}

func loadConfig(path string, debug bool) *config.Config {
	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}
	if debug {
		cfg.Logging.Level = "debug"
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid configuration: %v\n", err)
		os.Exit(1)
	}
	return cfg
}

func setupConfigWatcher(configPath string, debug bool, server *mcp.Server) *config.Watcher {
	if configPath == "" {
		return nil
	}
	watcher, err := config.NewWatcher(configPath, func(newCfg *config.Config) {
		if debug {
			newCfg.Logging.Level = "debug"
		}
		server.UpdateConfig(newCfg)
	})
	if err != nil {
		slog.Warn("config hot-reload disabled", slog.String("error", err.Error()))
		return nil
	}
	slog.Info("config hot-reload enabled", slog.String("path", configPath))
	return watcher
}

func closeWatcher(w *config.Watcher) {
	if w != nil {
		w.Close()
	}
}
