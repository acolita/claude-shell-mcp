// Package config handles configuration parsing for claude-shell-mcp.
package config

import (
	"log/slog"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// Watcher watches a config file for changes and reloads it.
type Watcher struct {
	path     string
	config   *Config
	mu       sync.RWMutex
	watcher  *fsnotify.Watcher
	onChange func(*Config)
	done     chan struct{}
}

// NewWatcher creates a new config watcher.
func NewWatcher(path string, onChange func(*Config)) (*Watcher, error) {
	// Load initial config
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}

	// Create fsnotify watcher
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		path:     path,
		config:   cfg,
		watcher:  fsWatcher,
		onChange: onChange,
		done:     make(chan struct{}),
	}

	// Watch the config file's directory (to handle editors that replace files)
	dir := filepath.Dir(path)
	if err := fsWatcher.Add(dir); err != nil {
		fsWatcher.Close()
		return nil, err
	}

	// Start watching in background
	go w.watch()

	return w, nil
}

// Config returns the current configuration.
func (w *Watcher) Config() *Config {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.config
}

// watch monitors for config file changes.
func (w *Watcher) watch() {
	filename := filepath.Base(w.path)

	for {
		select {
		case <-w.done:
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}

			// Check if this is our config file
			if filepath.Base(event.Name) != filename {
				continue
			}

			// Handle write or create events (editors may create new files)
			if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				w.reload()
			}

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			slog.Warn("config watcher error", slog.String("error", err.Error()))
		}
	}
}

// reload reloads the config from disk.
func (w *Watcher) reload() {
	cfg, err := Load(w.path)
	if err != nil {
		slog.Error("failed to reload config",
			slog.String("path", w.path),
			slog.String("error", err.Error()),
		)
		return
	}

	if err := cfg.Validate(); err != nil {
		slog.Error("invalid config after reload",
			slog.String("error", err.Error()),
		)
		return
	}

	w.mu.Lock()
	w.config = cfg
	w.mu.Unlock()

	slog.Info("config reloaded", slog.String("path", w.path))

	// Notify callback
	if w.onChange != nil {
		w.onChange(cfg)
	}
}

// Close stops watching and cleans up.
func (w *Watcher) Close() error {
	close(w.done)
	return w.watcher.Close()
}
