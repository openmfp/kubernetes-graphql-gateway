package manager_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/openmfp/golang-commons/logger/testlogger"

	"github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"github.com/openmfp/kubernetes-graphql-gateway/gateway/manager"
	"github.com/openmfp/kubernetes-graphql-gateway/gateway/manager/mocks"
)

func TestRoundTripper_RoundTrip(t *testing.T) {
	tests := []struct {
		name          string
		token         string
		requestTarget string
		impersonate   bool
		expectedUser  string
		setupMocks    func(adminRT, tokenOnlyRT, unauthorizedRT *mocks.MockRoundTripper)
		err           error
	}{
		{
			name:          "discovery_request",
			requestTarget: "/api/v1",
			setupMocks: func(adminRT, tokenOnlyRT, unauthorizedRT *mocks.MockRoundTripper) {
				adminRT.EXPECT().RoundTrip(mock.Anything).Once().Return(&http.Response{}, nil)
			},
		},
		{
			name:          "no_token_in_context",
			requestTarget: "/api/v1/configMaps",
			setupMocks: func(adminRT, tokenOnlyRT, unauthorizedRT *mocks.MockRoundTripper) {
				unauthorizedRT.EXPECT().RoundTrip(mock.Anything).Once().Return(&http.Response{}, nil)
			},
		},
		{
			name:          "impersonate_false",
			token:         "valid_token",
			requestTarget: "/api/v1/configMaps",
			impersonate:   false,
			setupMocks: func(adminRT, tokenOnlyRT, unauthorizedRT *mocks.MockRoundTripper) {
				tokenOnlyRT.EXPECT().RoundTrip(mock.Anything).Once().Return(&http.Response{}, nil)
			},
		},
		{
			name:          "failed_to_parse_token",
			token:         "not_valid_token",
			requestTarget: "/api/v1/configMaps",
			impersonate:   true,
			setupMocks: func(adminRT, tokenOnlyRT, unauthorizedRT *mocks.MockRoundTripper) {
				unauthorizedRT.EXPECT().RoundTrip(mock.Anything).Once().Return(&http.Response{}, nil)
			},
		},
		{
			name:          "user_claim_not_found",
			token:         createTestToken(t, jwt.MapClaims{}), // no "sub" claim
			requestTarget: "/api/v1/configMaps",
			impersonate:   true,
			setupMocks: func(adminRT, tokenOnlyRT, unauthorizedRT *mocks.MockRoundTripper) {
				unauthorizedRT.EXPECT().RoundTrip(mock.Anything).Once().Return(&http.Response{}, nil)
			},
		},
		{
			name:          "user_claim_is_not_a_string",
			token:         createTestToken(t, jwt.MapClaims{"sub": 123}), // sub is not a string
			requestTarget: "/api/v1/configMaps",
			impersonate:   true,
			setupMocks: func(adminRT, tokenOnlyRT, unauthorizedRT *mocks.MockRoundTripper) {
				unauthorizedRT.EXPECT().RoundTrip(mock.Anything).Once().Return(&http.Response{}, nil)
			},
		},
		{
			name:          "user_claim_is_empty_string",
			token:         createTestToken(t, jwt.MapClaims{"sub": ""}), // sub is empty string
			requestTarget: "/api/v1/configMaps",
			impersonate:   true,
			setupMocks: func(adminRT, tokenOnlyRT, unauthorizedRT *mocks.MockRoundTripper) {
				unauthorizedRT.EXPECT().RoundTrip(mock.Anything).Once().Return(&http.Response{}, nil)
			},
		},
		{
			name:          "impersonation_success",
			token:         createTestToken(t, jwt.MapClaims{"sub": "test-user"}),
			requestTarget: "/api/v1/configMaps",
			impersonate:   true,
			expectedUser:  "test-user",
			setupMocks: func(adminRT, tokenOnlyRT, unauthorizedRT *mocks.MockRoundTripper) {
				tokenOnlyRT.EXPECT().RoundTrip(mock.Anything).Once().Return(&http.Response{}, nil)
			},
		},
	}

	var adminRT, tokenOnlyRT, unauthorizedRT *mocks.MockRoundTripper
	var appCfg config.Config
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adminRT = &mocks.MockRoundTripper{}
			tokenOnlyRT = &mocks.MockRoundTripper{}
			unauthorizedRT = &mocks.MockRoundTripper{}

			if tt.setupMocks != nil {
				tt.setupMocks(adminRT, tokenOnlyRT, unauthorizedRT)
			}

			appCfg.Gateway.ShouldImpersonate = tt.impersonate
			appCfg.Gateway.UsernameClaim = "sub"

			rt := manager.NewRoundTripper(
				testlogger.New().HideLogOutput().Logger,
				appCfg,
				adminRT, tokenOnlyRT, unauthorizedRT,
			)

			req := httptest.NewRequest(http.MethodGet, tt.requestTarget, nil)
			if tt.token != "" {
				ctx := context.WithValue(req.Context(), manager.TokenKey{}, tt.token)
				req = req.WithContext(ctx)
			}

			resp, err := rt.RoundTrip(req)
			if tt.err != nil {
				assert.Error(t, err)
				assert.Nil(t, resp)
			}
		})
	}
}

func TestIsDiscoveryRequest_AllBuiltinGroups(t *testing.T) {
	tests := []struct {
		method   string
		path     string
		expected bool
	}{
		// Discovery requests
		{http.MethodGet, "/api", true},
		{http.MethodGet, "/api/v1", true},
		{http.MethodGet, "/apis/admissionregistration.k8s.io", true},
		{http.MethodGet, "/apis/apiextensions.k8s.io", true},
		{http.MethodGet, "/apis/apiregistration.k8s.io", true},
		{http.MethodGet, "/apis/apps", true},
		{http.MethodGet, "/apis/authentication.k8s.io", true},
		{http.MethodGet, "/apis/authorization.k8s.io", true},
		{http.MethodGet, "/apis/autoscaling", true},
		{http.MethodGet, "/apis/batch", true},
		{http.MethodGet, "/apis/certificates.k8s.io", true},
		{http.MethodGet, "/apis/coordination.k8s.io", true},
		{http.MethodGet, "/apis/networking.k8s.io", true},
		{http.MethodGet, "/apis/node.k8s.io", true},
		{http.MethodGet, "/apis/policy", true},
		{http.MethodGet, "/apis/rbac.authorization.k8s.io", true},
		{http.MethodGet, "/apis/scheduling.k8s.io", true},
		{http.MethodGet, "/apis/settings.k8s.io", true},
		{http.MethodGet, "/apis/storage.k8s.io", true},
		// CRD discovery
		{http.MethodGet, "/apis/networking.istio.io", true},
		{http.MethodGet, "/apis/security.istio.io", true},
		{http.MethodGet, "/apis/authentication.istio.io", true},
		{http.MethodGet, "/apis/core.openmfp.org/v1alpha1", true},
		// Discovery requests with /clusters
		{http.MethodGet, "/clusters/myworkspace/api", true},
		{http.MethodGet, "/clusters/myworkspace/api/v1", true},
		{http.MethodGet, "/clusters/myworkspace/apis", true},
		{http.MethodGet, "/clusters/myworkspace/apis/apps", true},
		{http.MethodGet, "/clusters/myworkspace/apis/apps/v1", true},
		{http.MethodGet, "/clusters/myworkspace/apis/networking.k8s.io", true},
		{http.MethodGet, "/clusters/myworkspace/apis/networking.k8s.io/v1", true},

		// Non-discovery requests
		{http.MethodPost, "/api", false},
		// for resources within each group
		{http.MethodGet, "/api/v1/pods", false},
		{http.MethodGet, "/apis/admissionregistration.k8s.io/v1/mutatingwebhookconfigurations", false},
		{http.MethodGet, "/apis/apiextensions.k8s.io/v1/customresourcedefinitions", false},
		{http.MethodGet, "/apis/apiregistration.k8s.io/v1/apiservices", false},
		{http.MethodGet, "/apis/apps/v1/deployments", false},
		{http.MethodGet, "/apis/authentication.k8s.io/v1/tokenreviews", false},
		{http.MethodGet, "/apis/authorization.k8s.io/v1/subjectaccessreviews", false},
		{http.MethodGet, "/apis/autoscaling/v1/horizontalpodautoscalers", false},
		{http.MethodGet, "/apis/batch/v1/jobs", false},
		{http.MethodGet, "/apis/certificates.k8s.io/v1/certificatesigningrequests", false},
		{http.MethodGet, "/apis/coordination.k8s.io/v1/leases", false},
		{http.MethodGet, "/apis/networking.k8s.io/v1/networkpolicies", false},
		{http.MethodGet, "/apis/node.k8s.io/v1/runtimeclasses", false},
		{http.MethodGet, "/apis/policy/v1/poddisruptionbudgets", false},
		{http.MethodGet, "/apis/rbac.authorization.k8s.io/v1/roles", false},
		{http.MethodGet, "/apis/scheduling.k8s.io/v1/priorityclasses", false},
		{http.MethodGet, "/apis/settings.k8s.io/v1/podpresets", false},
		{http.MethodGet, "/apis/storage.k8s.io/v1/storageclasses", false},
		// non-discovery CRD resources requests
		{http.MethodGet, "/apis/networking.istio.io/v1alpha3/gateways", false},
		{http.MethodGet, "/apis/security.istio.io/v1beta1/authorizationpolicies", false},
		{http.MethodGet, "/apis/authentication.istio.io/v1alpha1/policies", false},
		{http.MethodGet, "/apis/core.openmfp.org/v1alpha1/accounts", false},
		{http.MethodPost, "/clusters/myworkspace/api", false},
		{http.MethodGet, "/clusters/myworkspace/api/v1/pods", false},
		{http.MethodGet, "/clusters/myworkspace/apis/apps/v1/deployments", false},
		{http.MethodGet, "/clusters/myworkspace/apis/networking.k8s.io/v1/networkpolicies", false},
	}

	for _, tt := range tests {
		t.Run(strings.TrimPrefix(tt.path, "/"), func(t *testing.T) {
			req, _ := http.NewRequest(tt.method, tt.path, nil)
			req.URL.RawPath = "/clusters/myworkspace"
			actual := manager.IsDiscoveryRequestForTest(req)
			if actual != tt.expected {
				t.Errorf("For method %s and path %q, expected %v, got %v", tt.method, tt.path, tt.expected, actual)
			}
		})
	}
}

func createTestToken(t *testing.T, claims jwt.MapClaims) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedToken, err := token.SignedString([]byte("test-secret"))
	require.NoError(t, err)
	return signedToken
}
