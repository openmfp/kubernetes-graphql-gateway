package watcher

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"

	"github.com/openmfp/golang-commons/logger"
	"github.com/openmfp/golang-commons/sentry"
)

var (
	ErrUnknownFileEvent = errors.New("unknown file event")
)

// FileEventHandler handles file system events
type FileEventHandler interface {
	OnFileChanged(filename string)
	OnFileDeleted(filename string)
}

// ClusterRegistryInterface defines the minimal interface needed from ClusterRegistry
type ClusterRegistryInterface interface {
	LoadCluster(schemaFilePath string) error
	UpdateCluster(schemaFilePath string) error
	RemoveCluster(schemaFilePath string) error
}

// FileWatcher handles file watching and delegates to cluster registry
type FileWatcher struct {
	log             *logger.Logger
	watcher         *fsnotify.Watcher
	clusterRegistry ClusterRegistryInterface
	watchPath       string
}

// NewFileWatcher creates a new watcher service
func NewFileWatcher(
	log *logger.Logger,
	clusterRegistry ClusterRegistryInterface,
) (*FileWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	return &FileWatcher{
		log:             log,
		watcher:         watcher,
		clusterRegistry: clusterRegistry,
	}, nil
}

// Initialize sets up the watcher with the given path and processes existing files
func (s *FileWatcher) Initialize(watchPath string) error {
	s.watchPath = watchPath

	// Add path and subdirectories to watcher
	if err := s.addWatchRecursively(watchPath); err != nil {
		return fmt.Errorf("failed to add watch paths: %w", err)
	}

	// Process all files in directory recursively
	if err := s.loadAllFiles(watchPath); err != nil {
		return fmt.Errorf("failed to load files: %w", err)
	}

	// Start watching for file system events
	go s.startWatching()

	return nil
}

// startWatching begins watching for file system events (called from Initialize)
func (s *FileWatcher) startWatching() {
	for {
		select {
		case event, ok := <-s.watcher.Events:
			if !ok {
				return
			}
			s.handleEvent(event)
		case err, ok := <-s.watcher.Errors:
			if !ok {
				return
			}
			s.log.Error().Err(err).Msg("Error watching files")
			sentry.CaptureError(err, nil)
		}
	}
}

// Close closes the file watcher
func (s *FileWatcher) Close() error {
	return s.watcher.Close()
}

// addWatchRecursively adds the directory and all subdirectories to the watcher
func (s *FileWatcher) addWatchRecursively(dir string) error {
	if err := s.watcher.Add(dir); err != nil {
		return fmt.Errorf("failed to add watch path %s: %w", dir, err)
	}

	// Find subdirectories
	entries, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		return fmt.Errorf("failed to glob directory %s: %w", dir, err)
	}

	for _, entry := range entries {
		if dirInfo, err := os.Stat(entry); err == nil && dirInfo.IsDir() {
			if err := s.addWatchRecursively(entry); err != nil {
				return err
			}
		}
	}

	return nil
}

// loadAllFiles loads all files in the directory and subdirectories
func (s *FileWatcher) loadAllFiles(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Load cluster directly using full path
		if err := s.clusterRegistry.LoadCluster(path); err != nil {
			s.log.Error().Err(err).Str("file", path).Msg("Failed to load cluster from file")
			// Continue processing other files instead of failing
		}

		return nil
	})
}

func (s *FileWatcher) handleEvent(event fsnotify.Event) {
	s.log.Info().Str("event", event.String()).Msg("File event")

	filePath := event.Name
	switch event.Op {
	case fsnotify.Create:
		s.OnFileChanged(filePath)
	case fsnotify.Write:
		s.OnFileChanged(filePath)
	case fsnotify.Rename:
		s.OnFileDeleted(filePath)
	case fsnotify.Remove:
		s.OnFileDeleted(filePath)
	default:
		err := ErrUnknownFileEvent
		s.log.Error().Err(err).Str("filepath", filePath).Msg("Unknown file event")
		sentry.CaptureError(sentry.SentryError(err), nil, sentry.Extras{"filepath": filePath, "event": event.String()})
	}
}

func (s *FileWatcher) OnFileChanged(filePath string) {
	// Check if this is actually a file (not a directory)
	if info, err := os.Stat(filePath); err != nil || info.IsDir() {
		return
	}

	// Delegate to cluster registry
	if err := s.clusterRegistry.UpdateCluster(filePath); err != nil {
		s.log.Error().Err(err).Str("path", filePath).Msg("Failed to update cluster")
		sentry.CaptureError(err, sentry.Tags{"filepath": filePath})
		return
	}

	s.log.Info().Str("path", filePath).Msg("Successfully updated cluster from file change")
}

func (s *FileWatcher) OnFileDeleted(filePath string) {
	// Delegate to cluster registry
	if err := s.clusterRegistry.RemoveCluster(filePath); err != nil {
		s.log.Error().Err(err).Str("path", filePath).Msg("Failed to remove cluster")
		sentry.CaptureError(err, sentry.Tags{"filepath": filePath})
		return
	}

	s.log.Info().Str("path", filePath).Msg("Successfully removed cluster from file deletion")
}
