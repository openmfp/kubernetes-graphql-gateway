package targetcluster_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openmfp/kubernetes-graphql-gateway/gateway/manager/targetcluster"
)

func TestGetToken(t *testing.T) {
	tests := []struct {
		name          string
		authorization string
		expectedToken string
	}{
		{
			name:          "Bearer token",
			authorization: "Bearer abc123",
			expectedToken: "abc123",
		},
		{
			name:          "bearer token lowercase",
			authorization: "bearer def456",
			expectedToken: "def456",
		},
		{
			name:          "No Bearer prefix",
			authorization: "xyz789",
			expectedToken: "xyz789",
		},
		{
			name:          "Empty authorization",
			authorization: "",
			expectedToken: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}

			token := targetcluster.GetToken(req)
			if token != tt.expectedToken {
				t.Errorf("expected token %q, got %q", tt.expectedToken, token)
			}
		})
	}
}

func TestIsIntrospectionQuery(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected bool
	}{
		{
			name:     "Schema introspection",
			body:     `{"query": "{ __schema { types { name } } }"}`,
			expected: true,
		},
		{
			name:     "Type introspection",
			body:     `{"query": "{ __type(name: \"User\") { name } }"}`,
			expected: true,
		},
		{
			name:     "Normal query",
			body:     `{"query": "{ users { name } }"}`,
			expected: false,
		},
		{
			name:     "Invalid JSON",
			body:     `invalid json`,
			expected: false,
		},
		{
			name:     "Empty body",
			body:     "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			result := targetcluster.IsIntrospectionQuery(req)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}
