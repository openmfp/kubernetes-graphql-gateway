package manager_test

import (
	"context"
	"net/http"
	"net/http/httptest"
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
			requestTarget: manager.K8S_API_V1_PATH,
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

func createTestToken(t *testing.T, claims jwt.MapClaims) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedToken, err := token.SignedString([]byte("test-secret"))
	require.NoError(t, err)
	return signedToken
}
