package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kcp-dev/logicalcluster/v3"
	"github.com/openmfp/kubernetes-graphql-gateway/gateway/manager/handler"
	"github.com/openmfp/kubernetes-graphql-gateway/gateway/manager/roundtripper"
	"sigs.k8s.io/controller-runtime/pkg/kontext"
)

func newHTTPServerForTest() *handler.HTTPServer {
	server := handler.NewHTTPServerForTest()
	// Add a test handler
	server.SetHandlerForTest("testws", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	return server
}

func TestHTTPServer_CORSPreflight(t *testing.T) {
	server := newHTTPServerForTest()
	req := httptest.NewRequest(http.MethodOptions, "/testws/graphql", nil)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for CORS preflight, got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Error("CORS headers not set")
	}
}

func TestHTTPServer_InvalidWorkspace(t *testing.T) {
	server := newHTTPServerForTest()
	req := httptest.NewRequest(http.MethodGet, "/invalidws/graphql", nil)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for invalid workspace, got %d", w.Code)
	}
}

func TestHTTPServer_AuthRequired_NoToken(t *testing.T) {
	server := newHTTPServerForTest()
	// Disable local development to enforce auth
	server.AppCfg.LocalDevelopment = false
	req := httptest.NewRequest(http.MethodPost, "/testws/graphql", nil)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for missing token, got %d", w.Code)
	}
}

func TestHTTPServer_CheckClusterNameInRequest(t *testing.T) {
	server := newHTTPServerForTest()
	server.AppCfg.EnableKcp = true
	server.AppCfg.LocalDevelopment = true

	var capturedCtx context.Context
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
	})
	server.SetHandlerForTest("testws", testHandler)

	req := httptest.NewRequest(http.MethodPost, "/testws/graphql", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer test-token")

	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	cluster, ok := kontext.ClusterFrom(capturedCtx)
	if !ok || cluster != logicalcluster.Name("testws") {
		t.Errorf("expected workspace 'testws' in context, got %v (found: %t)", cluster, ok)
	}

	token, ok := capturedCtx.Value(roundtripper.TokenKey{}).(string)
	if !ok || token != "test-token" {
		t.Errorf("expected token 'test-token' in context, got %v (found: %t)", token, ok)
	}
}
