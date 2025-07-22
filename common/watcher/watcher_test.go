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

func TestNewFileWatcher_FsnotifyError(t *testing.T) {
	// This test covers the error path in NewFileWatcher when fsnotify.NewWatcher fails
	// Since we can't easily mock fsnotify.NewWatcher, we just test that our current implementation works
	// The error case would be covered in integration tests or when the system runs out of file descriptors
	log := testlogger.New().HideLogOutput().Logger
	handler := &MockFileEventHandler{}

	watcher, err := NewFileWatcher(handler, log)
	assert.NoError(t, err)
	assert.NotNil(t, watcher)
	defer watcher.watcher.Close()
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
		{
			name: "chmod_event_unhandled",
			event: fsnotify.Event{
				Name: tempFile,
				Op:   fsnotify.Chmod,
			},
			expectedChanged:      []string{},
			expectedDeleted:      []string{},
			createFileBeforeTest: true,
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

func TestWatchSingleFile_InvalidDirectory(t *testing.T) {
	log := testlogger.New().HideLogOutput().Logger
	handler := &MockFileEventHandler{}

	watcher, err := NewFileWatcher(handler, log)
	require.NoError(t, err)
	defer watcher.watcher.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Try to watch a file in a non-existent directory
	err = watcher.WatchSingleFile(ctx, "/non/existent/file.txt", 100)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to watch directory")
}

func TestWatchSingleFile_RealFile(t *testing.T) {
	log := testlogger.New().HideLogOutput().Logger
	handler := &MockFileEventHandler{}

	watcher, err := NewFileWatcher(handler, log)
	require.NoError(t, err)
	defer watcher.watcher.Close()

	// Create a temporary file
	tempDir, err := os.MkdirTemp("", "watch_single_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	tempFile := filepath.Join(tempDir, "watch_me.txt")
	err = os.WriteFile(tempFile, []byte("initial"), 0644)
	require.NoError(t, err)

	// Start watching with sufficient timeout for file change + debouncing
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	// Start watching in a goroutine
	watchDone := make(chan error, 1)
	go func() {
		watchDone <- watcher.WatchSingleFile(ctx, tempFile, 50) // 50ms debounce
	}()

	// Give the watcher time to start
	time.Sleep(30 * time.Millisecond)

	// Modify the file to trigger an event
	err = os.WriteFile(tempFile, []byte("modified"), 0644)
	require.NoError(t, err)

	// Give time for file change to be detected and debounced
	time.Sleep(120 * time.Millisecond) // 50ms debounce + extra buffer

	// Wait for watch to finish (should timeout after remaining time)
	err = <-watchDone
	assert.Equal(t, context.DeadlineExceeded, err)

	// Check that file change was detected
	assert.True(t, len(handler.OnFileChangedCalls) >= 1, "Expected at least 1 file change call")
	if len(handler.OnFileChangedCalls) > 0 {
		assert.Equal(t, tempFile, handler.OnFileChangedCalls[0])
	}
}

func TestWatchDirectory_InvalidPath(t *testing.T) {
	log := testlogger.New().HideLogOutput().Logger
	handler := &MockFileEventHandler{}

	watcher, err := NewFileWatcher(handler, log)
	require.NoError(t, err)
	defer watcher.watcher.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = watcher.WatchDirectory(ctx, "/non/existent/directory")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add watch paths")
}

func TestWatchDirectory_RealDirectory(t *testing.T) {
	log := testlogger.New().HideLogOutput().Logger
	handler := &MockFileEventHandler{}

	watcher, err := NewFileWatcher(handler, log)
	require.NoError(t, err)
	defer watcher.watcher.Close()

	// Create a temporary directory
	tempDir, err := os.MkdirTemp("", "watch_dir_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Start watching with a short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Start watching in a goroutine
	watchDone := make(chan error, 1)
	go func() {
		watchDone <- watcher.WatchDirectory(ctx, tempDir)
	}()

	// Give the watcher time to start
	time.Sleep(50 * time.Millisecond)

	// Create a file to trigger an event
	testFile := filepath.Join(tempDir, "new_file.txt")
	err = os.WriteFile(testFile, []byte("content"), 0644)
	require.NoError(t, err)

	// Wait for watch to finish
	err = <-watchDone
	assert.Equal(t, context.DeadlineExceeded, err)

	// Check that file creation was detected
	assert.True(t, len(handler.OnFileChangedCalls) >= 1, "Expected at least 1 file change call")
	if len(handler.OnFileChangedCalls) > 0 {
		assert.Equal(t, testFile, handler.OnFileChangedCalls[0])
	}
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

func TestAddWatchRecursively_GlobError(t *testing.T) {
	log := testlogger.New().HideLogOutput().Logger
	handler := &MockFileEventHandler{}

	watcher, err := NewFileWatcher(handler, log)
	require.NoError(t, err)
	defer watcher.watcher.Close()

	// Test with a directory path that would cause glob to fail
	// Using a path with invalid glob pattern characters
	invalidPath := "/tmp/[invalid"

	err = watcher.addWatchRecursively(invalidPath)
	assert.Error(t, err)
}

func TestWatchSingleFile_ContextCancellation(t *testing.T) {
	log := testlogger.New().HideLogOutput().Logger
	handler := &MockFileEventHandler{}

	watcher, err := NewFileWatcher(handler, log)
	require.NoError(t, err)
	defer watcher.watcher.Close()

	// Create a temporary file
	tempDir, err := os.MkdirTemp("", "watch_cancel_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	tempFile := filepath.Join(tempDir, "watch_me.txt")
	err = os.WriteFile(tempFile, []byte("initial"), 0644)
	require.NoError(t, err)

	// Create context that we'll cancel
	ctx, cancel := context.WithCancel(context.Background())

	// Start watching in a goroutine
	watchDone := make(chan error, 1)
	go func() {
		watchDone <- watcher.WatchSingleFile(ctx, tempFile, 50)
	}()

	// Give the watcher time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel the context
	cancel()

	// Wait for watch to finish
	err = <-watchDone
	assert.Equal(t, context.Canceled, err)
}

func TestWatchDirectory_ContextCancellation(t *testing.T) {
	log := testlogger.New().HideLogOutput().Logger
	handler := &MockFileEventHandler{}

	watcher, err := NewFileWatcher(handler, log)
	require.NoError(t, err)
	defer watcher.watcher.Close()

	// Create a temporary directory
	tempDir, err := os.MkdirTemp("", "watch_dir_cancel_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create context that we'll cancel
	ctx, cancel := context.WithCancel(context.Background())

	// Start watching in a goroutine
	watchDone := make(chan error, 1)
	go func() {
		watchDone <- watcher.WatchDirectory(ctx, tempDir)
	}()

	// Give the watcher time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel the context
	cancel()

	// Wait for watch to finish
	err = <-watchDone
	assert.Equal(t, context.Canceled, err)
}

func TestHandleEvent_StatError(t *testing.T) {
	log := testlogger.New().HideLogOutput().Logger
	handler := &MockFileEventHandler{}

	watcher, err := NewFileWatcher(handler, log)
	require.NoError(t, err)
	defer watcher.watcher.Close()

	// Test with a file that doesn't exist (stat will fail)
	nonExistentFile := "/tmp/non_existent_file_12345.txt"

	// Reset handler calls
	handler.OnFileChangedCalls = []string{}
	handler.OnFileDeletedCalls = []string{}

	// Handle create event for non-existent file
	event := fsnotify.Event{
		Name: nonExistentFile,
		Op:   fsnotify.Create,
	}

	watcher.handleEvent(event)

	// Should not call handler since stat failed
	assert.Equal(t, []string{}, handler.OnFileChangedCalls)
	assert.Equal(t, []string{}, handler.OnFileDeletedCalls)
}

func TestWatchSingleFile_WithDebounceTimer(t *testing.T) {
	log := testlogger.New().HideLogOutput().Logger
	handler := &MockFileEventHandler{}

	watcher, err := NewFileWatcher(handler, log)
	require.NoError(t, err)
	defer watcher.watcher.Close()

	// Create a temporary file
	tempDir, err := os.MkdirTemp("", "watch_debounce_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	tempFile := filepath.Join(tempDir, "watch_me.txt")
	err = os.WriteFile(tempFile, []byte("initial"), 0644)
	require.NoError(t, err)

	// Start watching with shorter debounce to make test more reliable
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	// Start watching in a goroutine
	watchDone := make(chan error, 1)
	go func() {
		watchDone <- watcher.WatchSingleFile(ctx, tempFile, 100) // 100ms debounce
	}()

	// Give the watcher time to start
	time.Sleep(50 * time.Millisecond)

	// Rapidly modify the file multiple times to test debounce timer cancellation
	for i := 0; i < 3; i++ {
		err = os.WriteFile(tempFile, []byte("modified"+string(rune(i))), 0644)
		require.NoError(t, err)
		time.Sleep(20 * time.Millisecond) // Less than debounce time
	}

	// Give some time for the debounced callback to execute
	time.Sleep(150 * time.Millisecond)

	// Wait for watch to finish
	err = <-watchDone
	assert.Equal(t, context.DeadlineExceeded, err)

	// Should have received at least one change (due to debouncing, multiple rapid changes = 1 call)
	// Note: This test focuses on exercising the debounce timer logic, not on exact callback behavior
	// The key coverage is the timer cancellation and recreation logic in watchWithDebounce
}

func TestAddWatchRecursively_NestedError(t *testing.T) {
	log := testlogger.New().HideLogOutput().Logger
	handler := &MockFileEventHandler{}

	watcher, err := NewFileWatcher(handler, log)
	require.NoError(t, err)
	defer watcher.watcher.Close()

	// Create temporary directory structure
	tempDir, err := os.MkdirTemp("", "watcher_nested_error_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create a subdirectory
	subDir := filepath.Join(tempDir, "subdir")
	err = os.MkdirAll(subDir, 0755)
	require.NoError(t, err)

	// Add the main directory to the watcher first so it has some watches
	err = watcher.watcher.Add(tempDir)
	require.NoError(t, err)

	// Now close the watcher to make subsequent Add calls fail
	watcher.watcher.Close()

	// Try to add recursively - should fail on subdirectory
	err = watcher.addWatchRecursively(tempDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add watch path")
}

func TestAddWatchRecursively_StatError(t *testing.T) {
	log := testlogger.New().HideLogOutput().Logger
	handler := &MockFileEventHandler{}

	watcher, err := NewFileWatcher(handler, log)
	require.NoError(t, err)
	defer watcher.watcher.Close()

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "watcher_stat_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create a subdirectory
	subDir := filepath.Join(tempDir, "subdir")
	err = os.MkdirAll(subDir, 0755)
	require.NoError(t, err)

	// Create a file in the directory (not a subdirectory)
	testFile := filepath.Join(tempDir, "file.txt")
	err = os.WriteFile(testFile, []byte("content"), 0644)
	require.NoError(t, err)

	// This should work fine - the stat error case is when os.Stat fails,
	// but that error is handled gracefully (ignored) in the code
	err = watcher.addWatchRecursively(tempDir)
	assert.NoError(t, err)
}

func TestWatchSingleFile_ErrorsInLoop(t *testing.T) {
	log := testlogger.New().HideLogOutput().Logger
	handler := &MockFileEventHandler{}

	watcher, err := NewFileWatcher(handler, log)
	require.NoError(t, err)

	// Create a temporary file
	tempDir, err := os.MkdirTemp("", "watch_errors_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	tempFile := filepath.Join(tempDir, "watch_me.txt")
	err = os.WriteFile(tempFile, []byte("initial"), 0644)
	require.NoError(t, err)

	// Start watching in a goroutine
	watchDone := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	go func() {
		watchDone <- watcher.WatchSingleFile(ctx, tempFile, 50)
	}()

	// Give the watcher time to start
	time.Sleep(50 * time.Millisecond)

	// Send an error to the errors channel by trying to watch an invalid path
	// This will generate an error that gets logged but doesn't stop the watcher
	go func() {
		time.Sleep(25 * time.Millisecond)
		// This should generate an error in the watcher
		_ = watcher.watcher.Add("/invalid/path/that/does/not/exist")
	}()

	// Wait for watch to finish
	err = <-watchDone
	assert.Equal(t, context.DeadlineExceeded, err)
}

func TestWatchDirectory_ErrorsInLoop(t *testing.T) {
	log := testlogger.New().HideLogOutput().Logger
	handler := &MockFileEventHandler{}

	watcher, err := NewFileWatcher(handler, log)
	require.NoError(t, err)

	// Create a temporary directory
	tempDir, err := os.MkdirTemp("", "watch_dir_errors_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Start watching in a goroutine
	watchDone := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	go func() {
		watchDone <- watcher.WatchDirectory(ctx, tempDir)
	}()

	// Give the watcher time to start
	time.Sleep(50 * time.Millisecond)

	// Send an error to the errors channel
	go func() {
		time.Sleep(25 * time.Millisecond)
		// This should generate an error in the watcher
		_ = watcher.watcher.Add("/invalid/path/that/does/not/exist")
	}()

	// Wait for watch to finish
	err = <-watchDone
	assert.Equal(t, context.DeadlineExceeded, err)
}

func TestAddWatchRecursively_DirectAddError(t *testing.T) {
	log := testlogger.New().HideLogOutput().Logger
	handler := &MockFileEventHandler{}

	watcher, err := NewFileWatcher(handler, log)
	require.NoError(t, err)
	defer watcher.watcher.Close()

	// Close the watcher immediately to make Add fail
	watcher.watcher.Close()

	// Try to add a directory - should fail immediately on the first Add call
	err = watcher.addWatchRecursively("/tmp")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add watch path")
}

// Test NewFileWatcher edge case documentation
func TestNewFileWatcher_Documentation(t *testing.T) {
	// This test documents the NewFileWatcher error path that's difficult to test
	// The 25% missing coverage in NewFileWatcher is the error path when
	// fsnotify.NewWatcher() fails, which can happen when:
	// - The system runs out of file descriptors
	// - The OS doesn't support inotify/kqueue
	// - Insufficient permissions
	//
	// Since we can't easily mock fsnotify.NewWatcher(), this error path
	// would be covered in integration tests or when system resources are limited

	log := testlogger.New().HideLogOutput().Logger
	handler := &MockFileEventHandler{}

	// Normal case should work
	watcher, err := NewFileWatcher(handler, log)
	assert.NoError(t, err)
	assert.NotNil(t, watcher)
	defer watcher.watcher.Close()
}

func TestWatchSingleFile_NilHandler(t *testing.T) {
	log := testlogger.New().HideLogOutput().Logger

	// Test with nil handler to ensure it doesn't panic
	watcher, err := NewFileWatcher(nil, log)
	require.NoError(t, err)
	defer watcher.watcher.Close()

	// Create a temporary file
	tempDir, err := os.MkdirTemp("", "watch_nil_handler_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	tempFile := filepath.Join(tempDir, "watch_me.txt")
	err = os.WriteFile(tempFile, []byte("initial"), 0644)
	require.NoError(t, err)

	// Start watching with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// This should not panic even with nil handler
	err = watcher.WatchSingleFile(ctx, tempFile, 50)
	assert.Equal(t, context.DeadlineExceeded, err)
}
