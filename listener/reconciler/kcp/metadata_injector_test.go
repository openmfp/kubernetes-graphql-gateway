package kcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmfp/golang-commons/logger/testlogger"
)

func TestInjectKCPClusterMetadata(t *testing.T) {
	log := testlogger.New().HideLogOutput().Logger

	// Create a temporary kubeconfig for testing
	tempDir := t.TempDir()
	kubeconfigPath := filepath.Join(tempDir, "config")

	kubeconfigContent := `
apiVersion: v1
kind: Config
current-context: test-context
contexts:
- name: test-context
  context:
    cluster: test-cluster
    user: test-user
clusters:
- name: test-cluster
  cluster:
    server: https://kcp.api.portal.cc-d1.showroom.apeirora.eu:443
    certificate-authority-data: LS0tLS1CRUdJTi0tLS0t
users:
- name: test-user
  user:
    token: test-token
`

	err := os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0644)
	require.NoError(t, err)

	// Set environment variable
	originalKubeconfig := os.Getenv("KUBECONFIG")
	defer os.Setenv("KUBECONFIG", originalKubeconfig)
	os.Setenv("KUBECONFIG", kubeconfigPath)

	tests := []struct {
		name         string
		schemaJSON   []byte
		clusterPath  string
		expectedHost string
		expectError  bool
	}{
		{
			name: "successful_injection",
			schemaJSON: []byte(`{
				"definitions": {
					"test.resource": {
						"type": "object",
						"properties": {
							"metadata": {
								"type": "object"
							}
						}
					}
				}
			}`),
			clusterPath:  "root:test",
			expectedHost: "https://kcp.api.portal.cc-d1.showroom.apeirora.eu:443",
			expectError:  false,
		},
		{
			name: "invalid_json",
			schemaJSON: []byte(`{
				"definitions": {
					"test.resource": invalid-json
				}
			}`),
			clusterPath: "root:test",
			expectError: true,
		},
	}

	// Add test for host override (virtual workspace)
	t.Run("with_host_override", func(t *testing.T) {
		overrideURL := "https://kcp.api.portal.cc-d1.showroom.apeirora.eu:443/services/contentconfigurations"
		schemaJSON := []byte(`{
			"definitions": {
				"test.resource": {
					"type": "object",
					"properties": {
						"metadata": {
							"type": "object"
						}
					}
				}
			}
		}`)

		result, err := injectKCPClusterMetadata(schemaJSON, "virtual-workspace/custom-ws", log, overrideURL)
		require.NoError(t, err)
		assert.NotNil(t, result)

		// Parse the result to verify metadata injection
		var resultData map[string]interface{}
		err = json.Unmarshal(result, &resultData)
		require.NoError(t, err)

		// Check that metadata was injected with override host
		metadata, exists := resultData["x-cluster-metadata"]
		require.True(t, exists, "x-cluster-metadata should be present")

		metadataMap, ok := metadata.(map[string]interface{})
		require.True(t, ok, "x-cluster-metadata should be a map")

		// Verify override host is used
		host, exists := metadataMap["host"]
		require.True(t, exists, "host should be present")
		assert.Equal(t, overrideURL, host, "host should be the override URL")
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := injectKCPClusterMetadata(tt.schemaJSON, tt.clusterPath, log)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, result)

			// Parse the result to verify metadata injection
			var resultData map[string]interface{}
			err = json.Unmarshal(result, &resultData)
			require.NoError(t, err)

			// Check that metadata was injected
			metadata, exists := resultData["x-cluster-metadata"]
			require.True(t, exists, "x-cluster-metadata should be present")

			metadataMap, ok := metadata.(map[string]interface{})
			require.True(t, ok, "x-cluster-metadata should be a map")

			// Verify host
			host, exists := metadataMap["host"]
			require.True(t, exists, "host should be present")
			assert.Equal(t, tt.expectedHost, host)

			// Verify path
			path, exists := metadataMap["path"]
			require.True(t, exists, "path should be present")
			assert.Equal(t, tt.clusterPath, path)

			// Verify auth
			auth, exists := metadataMap["auth"]
			require.True(t, exists, "auth should be present")

			authMap, ok := auth.(map[string]interface{})
			require.True(t, ok, "auth should be a map")

			authType, exists := authMap["type"]
			require.True(t, exists, "auth type should be present")
			assert.Equal(t, "kubeconfig", authType)

			kubeconfig, exists := authMap["kubeconfig"]
			require.True(t, exists, "kubeconfig should be present")
			assert.NotEmpty(t, kubeconfig, "kubeconfig should not be empty")

			// Verify CA data (if present)
			if ca, exists := metadataMap["ca"]; exists {
				caMap, ok := ca.(map[string]interface{})
				require.True(t, ok, "ca should be a map")

				caData, exists := caMap["data"]
				require.True(t, exists, "ca data should be present")
				assert.NotEmpty(t, caData, "ca data should not be empty")
			}
		})
	}
}

func TestExtractKubeconfigData(t *testing.T) {
	log := testlogger.New().HideLogOutput().Logger

	tests := []struct {
		name          string
		setupEnv      func() (cleanup func())
		expectedHost  string
		expectError   bool
		errorContains string
	}{
		{
			name: "from_env_variable",
			setupEnv: func() func() {
				tempDir := t.TempDir()
				kubeconfigPath := filepath.Join(tempDir, "config")

				kubeconfigContent := `
apiVersion: v1
kind: Config
current-context: test-context
contexts:
- name: test-context
  context:
    cluster: test-cluster
clusters:
- name: test-cluster
  cluster:
    server: https://test.example.com:6443
`

				err := os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0644)
				require.NoError(t, err)

				original := os.Getenv("KUBECONFIG")
				os.Setenv("KUBECONFIG", kubeconfigPath)

				return func() {
					os.Setenv("KUBECONFIG", original)
				}
			},
			expectedHost: "https://test.example.com:6443",
			expectError:  false,
		},
		{
			name: "file_not_found",
			setupEnv: func() func() {
				original := os.Getenv("KUBECONFIG")
				os.Setenv("KUBECONFIG", "/non/existent/path")

				return func() {
					os.Setenv("KUBECONFIG", original)
				}
			},
			expectError:   true,
			errorContains: "kubeconfig file not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup := tt.setupEnv()
			defer cleanup()

			kubeconfigData, host, err := extractKubeconfigData(log)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, kubeconfigData)
			assert.Equal(t, tt.expectedHost, host)
		})
	}
}
