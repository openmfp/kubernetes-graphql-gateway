package watcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/openmfp/golang-commons/logger/testlogger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockFileEventHandler for testing
type MockFileEventHandler struct {
	OnFileChangedCalls []string
	OnFileDeletedCalls []string
}

func (m *MockFileEventHandler) OnFileChanged(filepath string) {
	m.OnFileChangedCalls = append(m.OnFileChangedCalls, filepath)
}

func (m *MockFileEventHandler) OnFileDeleted(filepath string) {
	m.OnFileDeletedCalls = append(m.OnFileDeletedCalls, filepath)
}

func TestNewFileWatcher(t *testing.T) {
	tests := []struct {
		name        string
		handler     FileEventHandler
		expectError bool
	}{
		{
			name:        "valid_handler",
			handler:     &MockFileEventHandler{},
			expectError: false,
		},
		{
			name:        "nil_handler",
			handler:     nil,
			expectError: false, // Should still work with nil handler
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := testlogger.New().HideLogOutput().Logger

			watcher, err := NewFileWatcher(tt.handler, log)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, watcher)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, watcher)
				assert.Equal(t, tt.handler, watcher.handler)
				assert.Equal(t, log, watcher.log)
				assert.NotNil(t, watcher.watcher)
			}
		})
	}
}

func TestIsTargetFileEvent(t *testing.T) {
	log := testlogger.New().HideLogOutput().Logger
	handler := &MockFileEventHandler{}

	watcher, err := NewFileWatcher(handler, log)
	require.NoError(t, err)
	defer watcher.watcher.Close()

	tests := []struct {
		name       string
		event      fsnotify.Event
		targetFile string
		expected   bool
	}{
		{
			name: "write_event_matches_target",
			event: fsnotify.Event{
				Name: "/test/file.txt",
				Op:   fsnotify.Write,
			},
			targetFile: "/test/file.txt",
			expected:   true,
		},
		{
			name: "create_event_matches_target",
			event: fsnotify.Event{
				Name: "/test/file.txt",
				Op:   fsnotify.Create,
			},
			targetFile: "/test/file.txt",
			expected:   true,
		},
		{
			name: "remove_event_not_matching",
			event: fsnotify.Event{
				Name: "/test/file.txt",
				Op:   fsnotify.Remove,
			},
			targetFile: "/test/file.txt",
			expected:   false,
		},
		{
			name: "different_file_not_matching",
			event: fsnotify.Event{
				Name: "/test/other.txt",
				Op:   fsnotify.Write,
			},
			targetFile: "/test/file.txt",
			expected:   false,
		},
		{
			name: "path_normalization_matching",
			event: fsnotify.Event{
				Name: "/test/../test/file.txt",
				Op:   fsnotify.Write,
			},
			targetFile: "/test/file.txt",
			expected:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := watcher.isTargetFileEvent(tt.event, tt.targetFile)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHandleEvent(t *testing.T) {
	log := testlogger.New().HideLogOutput().Logger
	handler := &MockFileEventHandler{}

	watcher, err := NewFileWatcher(handler, log)
	require.NoError(t, err)
	defer watcher.watcher.Close()

	// Create a temporary file for testing
	tempDir, err := os.MkdirTemp("", "watcher_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	tempFile := filepath.Join(tempDir, "test.txt")
	err = os.WriteFile(tempFile, []byte("test"), 0644)
	require.NoError(t, err)

	tests := []struct {
		name                 string
		event                fsnotify.Event
		expectedChanged      []string
		expectedDeleted      []string
		createFileBeforeTest bool
	}{
		{
			name: "create_event_file",
			event: fsnotify.Event{
				Name: tempFile,
				Op:   fsnotify.Create,
			},
			expectedChanged:      []string{tempFile},
			expectedDeleted:      []string{},
			createFileBeforeTest: true,
		},
		{
			name: "write_event_file",
			event: fsnotify.Event{
				Name: tempFile,
				Op:   fsnotify.Write,
			},
			expectedChanged:      []string{tempFile},
			expectedDeleted:      []string{},
			createFileBeforeTest: true,
		},
		{
			name: "remove_event_file",
			event: fsnotify.Event{
				Name: tempFile,
				Op:   fsnotify.Remove,
			},
			expectedChanged:      []string{},
			expectedDeleted:      []string{tempFile},
			createFileBeforeTest: false,
		},
		{
			name: "rename_event_file",
			event: fsnotify.Event{
				Name: tempFile,
				Op:   fsnotify.Rename,
			},
			expectedChanged:      []string{},
			expectedDeleted:      []string{tempFile},
			createFileBeforeTest: false,
		},
		{
			name: "create_event_directory",
			event: fsnotify.Event{
				Name: tempDir + "/newdir",
				Op:   fsnotify.Create,
			},
			expectedChanged:      []string{},
			expectedDeleted:      []string{},
			createFileBeforeTest: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset handler calls
			handler.OnFileChangedCalls = []string{}
			handler.OnFileDeletedCalls = []string{}

			// Create directory for directory test
			if tt.name == "create_event_directory" {
				err := os.MkdirAll(tempDir+"/newdir", 0755)
				require.NoError(t, err)
				defer os.RemoveAll(tempDir + "/newdir")
			}

			// Ensure file exists if needed
			if tt.createFileBeforeTest {
				err := os.WriteFile(tempFile, []byte("test"), 0644)
				require.NoError(t, err)
			}

			watcher.handleEvent(tt.event)

			assert.Equal(t, tt.expectedChanged, handler.OnFileChangedCalls)
			assert.Equal(t, tt.expectedDeleted, handler.OnFileDeletedCalls)
		})
	}
}

func TestWatchSingleFile_EmptyPath(t *testing.T) {
	log := testlogger.New().HideLogOutput().Logger
	handler := &MockFileEventHandler{}

	watcher, err := NewFileWatcher(handler, log)
	require.NoError(t, err)
	defer watcher.watcher.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = watcher.WatchSingleFile(ctx, "", 100)
	assert.Equal(t, context.DeadlineExceeded, err)
}

func TestAddWatchRecursively(t *testing.T) {
	log := testlogger.New().HideLogOutput().Logger
	handler := &MockFileEventHandler{}

	watcher, err := NewFileWatcher(handler, log)
	require.NoError(t, err)
	defer watcher.watcher.Close()

	// Create temporary directory structure
	tempDir, err := os.MkdirTemp("", "watcher_recursive_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create nested directories
	subDir1 := filepath.Join(tempDir, "subdir1")
	subDir2 := filepath.Join(tempDir, "subdir2")
	subSubDir := filepath.Join(subDir1, "subsubdir")

	err = os.MkdirAll(subSubDir, 0755)
	require.NoError(t, err)
	err = os.MkdirAll(subDir2, 0755)
	require.NoError(t, err)

	// Test recursive watching
	err = watcher.addWatchRecursively(tempDir)
	assert.NoError(t, err)

	// Test with non-existent directory
	err = watcher.addWatchRecursively("/non/existent/directory")
	assert.Error(t, err)
}
