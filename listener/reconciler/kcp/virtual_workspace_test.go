package kcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmfp/kubernetes-graphql-gateway/common/config"
)

func TestNewVirtualWorkspaceManager(t *testing.T) {
	appCfg := config.Config{}
	appCfg.Url.VirtualWorkspacePrefix = "virtual-workspace"

	manager := NewVirtualWorkspaceManager(appCfg)

	assert.NotNil(t, manager)
	assert.Equal(t, appCfg, manager.appCfg)
}

func TestVirtualWorkspaceManager_GetWorkspacePath(t *testing.T) {
	tests := []struct {
		name         string
		prefix       string
		workspace    VirtualWorkspace
		expectedPath string
	}{
		{
			name:   "basic_workspace_path",
			prefix: "virtual-workspace",
			workspace: VirtualWorkspace{
				Name: "test-workspace",
				URL:  "https://example.com",
			},
			expectedPath: "virtual-workspace/test-workspace",
		},
		{
			name:   "workspace_with_special_chars",
			prefix: "vw",
			workspace: VirtualWorkspace{
				Name: "test-workspace_123.domain",
				URL:  "https://example.com",
			},
			expectedPath: "vw/test-workspace_123.domain",
		},
		{
			name:   "empty_prefix",
			prefix: "",
			workspace: VirtualWorkspace{
				Name: "test-workspace",
				URL:  "https://example.com",
			},
			expectedPath: "/test-workspace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appCfg := config.Config{}
			appCfg.Url.VirtualWorkspacePrefix = tt.prefix

			manager := NewVirtualWorkspaceManager(appCfg)
			result := manager.GetWorkspacePath(tt.workspace)

			assert.Equal(t, tt.expectedPath, result)
		})
	}
}

func TestCreateVirtualConfig(t *testing.T) {
	tests := []struct {
		name        string
		workspace   VirtualWorkspace
		expectError bool
		errorType   error
	}{
		{
			name: "valid_workspace_without_kubeconfig",
			workspace: VirtualWorkspace{
				Name: "test-workspace",
				URL:  "https://example.com",
			},
			expectError: false,
		},
		{
			name: "empty_url",
			workspace: VirtualWorkspace{
				Name: "test-workspace",
				URL:  "",
			},
			expectError: true,
			errorType:   ErrInvalidVirtualWorkspaceURL,
		},
		{
			name: "invalid_url",
			workspace: VirtualWorkspace{
				Name: "test-workspace",
				URL:  "://invalid-url",
			},
			expectError: true,
			errorType:   ErrParseVirtualWorkspaceURL,
		},
		{
			name: "valid_url_with_port",
			workspace: VirtualWorkspace{
				Name: "test-workspace",
				URL:  "https://example.com:8080",
			},
			expectError: false,
		},
		{
			name: "non_existent_kubeconfig",
			workspace: VirtualWorkspace{
				Name:       "test-workspace",
				URL:        "https://example.com",
				Kubeconfig: "/non/existent/kubeconfig",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := createVirtualConfig(tt.workspace)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, config)
				if tt.errorType != nil {
					assert.ErrorIs(t, err, tt.errorType)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, config)
				assert.Equal(t, tt.workspace.URL+"/clusters/root", config.Host)
				if tt.workspace.Kubeconfig == "" {
					assert.True(t, config.TLSClientConfig.Insecure)
					assert.Equal(t, "kubernetes-graphql-gateway-listener", config.UserAgent)
				}
			}
		})
	}
}

func TestVirtualWorkspaceManager_LoadConfig(t *testing.T) {
	tests := []struct {
		name          string
		configPath    string
		configContent string
		expectError   bool
		expectedCount int
	}{
		{
			name:          "empty_config_path",
			configPath:    "",
			expectError:   false,
			expectedCount: 0,
		},
		{
			name:          "non_existent_file",
			configPath:    "/non/existent/config.yaml",
			expectError:   false,
			expectedCount: 0,
		},
		{
			name:       "valid_config_single_workspace",
			configPath: "test-config.yaml",
			configContent: `
virtualWorkspaces:
  - name: "test-workspace"
    url: "https://example.com"
`,
			expectError:   false,
			expectedCount: 1,
		},
		{
			name:       "valid_config_multiple_workspaces",
			configPath: "test-config.yaml",
			configContent: `
virtualWorkspaces:
  - name: "workspace1"
    url: "https://example.com"
  - name: "workspace2"
    url: "https://example.org"
    kubeconfig: "/path/to/kubeconfig"
`,
			expectError:   false,
			expectedCount: 2,
		},
		{
			name:       "invalid_yaml",
			configPath: "test-config.yaml",
			configContent: `
virtualWorkspaces:
  - name: "test-workspace"
    url: "https://example.com"
  invalid yaml content
`,
			expectError: true,
		},
		{
			name:          "empty_file",
			configPath:    "test-config.yaml",
			configContent: "",
			expectError:   false,
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appCfg := config.Config{}
			manager := NewVirtualWorkspaceManager(appCfg)

			// Create temporary file if content is provided
			var tempFile string
			if tt.configContent != "" {
				tempDir, err := os.MkdirTemp("", "virtual_workspace_test")
				require.NoError(t, err)
				defer os.RemoveAll(tempDir)

				tempFile = filepath.Join(tempDir, "config.yaml")
				err = os.WriteFile(tempFile, []byte(tt.configContent), 0644)
				require.NoError(t, err)

				// Use the temporary file path
				tt.configPath = tempFile
			}

			config, err := manager.LoadConfig(tt.configPath)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, config)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, config)
				assert.Equal(t, tt.expectedCount, len(config.VirtualWorkspaces))

				if tt.expectedCount > 0 {
					assert.NotEmpty(t, config.VirtualWorkspaces[0].Name)
					assert.NotEmpty(t, config.VirtualWorkspaces[0].URL)
				}

				if tt.expectedCount == 2 {
					assert.Equal(t, "workspace1", config.VirtualWorkspaces[0].Name)
					assert.Equal(t, "workspace2", config.VirtualWorkspaces[1].Name)
					assert.Equal(t, "/path/to/kubeconfig", config.VirtualWorkspaces[1].Kubeconfig)
				}
			}
		})
	}
}

func TestNewVirtualWorkspaceReconciler(t *testing.T) {
	appCfg := config.Config{}
	manager := NewVirtualWorkspaceManager(appCfg)

	reconciler := NewVirtualWorkspaceReconciler(manager, nil, nil, nil)

	assert.NotNil(t, reconciler)
	assert.Equal(t, manager, reconciler.virtualWSManager)
	assert.NotNil(t, reconciler.currentWorkspaces)
	assert.Equal(t, 0, len(reconciler.currentWorkspaces))
}
