package kcp

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/openmfp/golang-commons/logger"
)

// ConfigWatcher watches the virtual workspaces configuration file for changes
type ConfigWatcher struct {
	watcher          *fsnotify.Watcher
	virtualWSManager *VirtualWorkspaceManager
	log              *logger.Logger
	mu               sync.Mutex
	isRunning        bool
}

// NewConfigWatcher creates a new config file watcher
func NewConfigWatcher(virtualWSManager *VirtualWorkspaceManager, log *logger.Logger) (*ConfigWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	return &ConfigWatcher{
		watcher:          watcher,
		virtualWSManager: virtualWSManager,
		log:              log,
	}, nil
}

// Start starts watching the configuration file
func (c *ConfigWatcher) Start(ctx context.Context, configPath string, changeHandler func(*VirtualWorkspacesConfig)) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isRunning {
		return fmt.Errorf("config watcher is already running")
	}

	if configPath == "" {
		c.log.Info().Msg("no virtual workspaces config path provided, skipping config watcher")
		return nil
	}

	// Watch the directory containing the config file
	configDir := filepath.Dir(configPath)
	if err := c.watcher.Add(configDir); err != nil {
		return fmt.Errorf("failed to watch config directory %s: %w", configDir, err)
	}

	c.isRunning = true
	c.log.Info().Str("configPath", configPath).Msg("started watching virtual workspaces config file")

	// Load initial configuration
	if err := c.loadAndNotify(configPath, changeHandler); err != nil {
		c.log.Error().Err(err).Msg("failed to load initial virtual workspaces config")
	}

	go c.watchLoop(ctx, configPath, changeHandler)

	return nil
}

// Stop stops the config watcher
func (c *ConfigWatcher) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.isRunning {
		return nil
	}

	c.isRunning = false
	return c.watcher.Close()
}

// watchLoop runs the file watching loop
func (c *ConfigWatcher) watchLoop(ctx context.Context, configPath string, changeHandler func(*VirtualWorkspacesConfig)) {
	debounceTimer := time.NewTimer(0)
	if !debounceTimer.Stop() {
		<-debounceTimer.C
	}

	for {
		select {
		case <-ctx.Done():
			c.log.Info().Msg("stopping virtual workspaces config watcher due to context cancellation")
			return

		case event, ok := <-c.watcher.Events:
			if !ok {
				c.log.Info().Msg("config watcher events channel closed")
				return
			}

			// Check if the event is for our config file
			if filepath.Clean(event.Name) != filepath.Clean(configPath) {
				continue
			}

			if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				c.log.Debug().Str("event", event.String()).Msg("config file changed")

				// Debounce file changes to avoid multiple rapid reloads
				debounceTimer.Reset(500 * time.Millisecond)
			}

		case err, ok := <-c.watcher.Errors:
			if !ok {
				c.log.Info().Msg("config watcher errors channel closed")
				return
			}
			c.log.Error().Err(err).Msg("config watcher error")

		case <-debounceTimer.C:
			if err := c.loadAndNotify(configPath, changeHandler); err != nil {
				c.log.Error().Err(err).Msg("failed to reload virtual workspaces config")
			}
		}
	}
}

// loadAndNotify loads the config and notifies the change handler
func (c *ConfigWatcher) loadAndNotify(configPath string, changeHandler func(*VirtualWorkspacesConfig)) error {
	config, err := c.virtualWSManager.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	c.log.Info().Int("virtualWorkspaces", len(config.VirtualWorkspaces)).Msg("loaded virtual workspaces config")

	changeHandler(config)
	return nil
}
