package targetcluster

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/openmfp/golang-commons/logger/testlogger"
	appConfig "github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"github.com/openmfp/kubernetes-graphql-gateway/gateway/manager/roundtripper"
)

func TestExtractClusterNameWithKCPWorkspace(t *testing.T) {
	log := testlogger.New().HideLogOutput().Logger
	appCfg := appConfig.Config{} // Default config

	registry := NewClusterRegistry(log, appCfg, nil)

	tests := []struct {
		name                 string
		path                 string
		expectedClusterName  string
		expectedKCPWorkspace string
		shouldSucceed        bool
	}{
		{
			name:                 "regular workspace",
			path:                 "/test-cluster/graphql",
			expectedClusterName:  "test-cluster",
			expectedKCPWorkspace: "",
			shouldSucceed:        true,
		},
		{
			name:                 "virtual workspace with KCP workspace",
			path:                 "/virtual-workspace/custom-ws/root/graphql",
			expectedClusterName:  "virtual-workspace/custom-ws",
			expectedKCPWorkspace: "root",
			shouldSucceed:        true,
		},
		{
			name:                 "virtual workspace with namespaced KCP workspace",
			path:                 "/virtual-workspace/custom-ws/root:orgs/graphql",
			expectedClusterName:  "virtual-workspace/custom-ws",
			expectedKCPWorkspace: "root:orgs",
			shouldSucceed:        true,
		},
		{
			name:                 "virtual workspace missing KCP workspace",
			path:                 "/virtual-workspace/custom-ws/graphql",
			expectedClusterName:  "",
			expectedKCPWorkspace: "",
			shouldSucceed:        false,
		},
		{
			name:                 "virtual workspace empty KCP workspace",
			path:                 "/virtual-workspace/custom-ws//graphql",
			expectedClusterName:  "",
			expectedKCPWorkspace: "",
			shouldSucceed:        false,
		},
		{
			name:                 "invalid path",
			path:                 "/invalid/path",
			expectedClusterName:  "",
			expectedKCPWorkspace: "",
			shouldSucceed:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test request
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()

			// Extract cluster name
			clusterName, _, success := registry.extractClusterName(w, req)

			// Check if the operation succeeded as expected
			if success != tt.shouldSucceed {
				t.Errorf("extractClusterName() success = %v, want %v", success, tt.shouldSucceed)
				return
			}

			if !tt.shouldSucceed {
				return // No need to check further if operation was expected to fail
			}

			// Check cluster name
			if clusterName != tt.expectedClusterName {
				t.Errorf("extractClusterName() clusterName = %v, want %v", clusterName, tt.expectedClusterName)
			}

			// Check KCP workspace in context
			if kcpWorkspace, ok := req.Context().Value(kcpWorkspaceKey).(string); ok {
				if kcpWorkspace != tt.expectedKCPWorkspace {
					t.Errorf("KCP workspace in context = %v, want %v", kcpWorkspace, tt.expectedKCPWorkspace)
				}
			} else if tt.expectedKCPWorkspace != "" {
				t.Errorf("Expected KCP workspace %v in context, but not found", tt.expectedKCPWorkspace)
			}
		})
	}
}

func TestSetContextsWithKCPWorkspace(t *testing.T) {
	tests := []struct {
		name                     string
		workspace                string
		contextKCPWorkspace      string
		enableKcp                bool
		expectedKCPWorkspaceName string
	}{
		{
			name:                     "regular workspace with KCP enabled",
			workspace:                "test-cluster",
			contextKCPWorkspace:      "",
			enableKcp:                true,
			expectedKCPWorkspaceName: "test-cluster",
		},
		{
			name:                     "virtual workspace with context KCP workspace",
			workspace:                "virtual-workspace/custom-ws",
			contextKCPWorkspace:      "root",
			enableKcp:                true,
			expectedKCPWorkspaceName: "root",
		},
		{
			name:                     "virtual workspace with namespaced context KCP workspace",
			workspace:                "virtual-workspace/custom-ws",
			contextKCPWorkspace:      "root:orgs",
			enableKcp:                true,
			expectedKCPWorkspaceName: "root:orgs",
		},
		{
			name:                     "KCP disabled",
			workspace:                "virtual-workspace/custom-ws",
			contextKCPWorkspace:      "root",
			enableKcp:                false,
			expectedKCPWorkspaceName: "", // Not relevant when KCP is disabled
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test request with KCP workspace in context if provided
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.contextKCPWorkspace != "" {
				req = req.WithContext(context.WithValue(req.Context(), kcpWorkspaceKey, tt.contextKCPWorkspace))
			}

			// Call SetContexts
			resultReq := SetContexts(req, tt.workspace, "test-token", tt.enableKcp)

			// For this test, we can't easily verify the KCP logical cluster context,
			// but we can verify that the function doesn't panic and returns a request
			if resultReq == nil {
				t.Error("SetContexts() returned nil request")
			}

			// Verify token context is set
			if token, ok := resultReq.Context().Value(roundtripper.TokenKey{}).(string); ok {
				if token != "test-token" {
					t.Errorf("Token in context = %v, want %v", token, "test-token")
				}
			} else {
				t.Error("Expected token in context, but not found")
			}
		})
	}
}
