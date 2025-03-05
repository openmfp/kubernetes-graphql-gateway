package manager

import (
	"context"
	"github.com/openmfp/crd-gql-gateway/gateway/manager/mocks"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/openmfp/golang-commons/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/transport"
)

func TestRoundTripper_RoundTrip(t *testing.T) {
	tests := []struct {
		name         string
		token        string
		impersonate  bool
		expectedUser string
	}{
		{
			name:         "success",
			token:        createTestToken(t, jwt.MapClaims{"sub": "test-user"}),
			impersonate:  true,
			expectedUser: "test-user",
		},
		{
			name:        "no_token_in_context",
			impersonate: false,
		},
		{
			name:        "token_present_impersonate_false",
			token:       "valid-token",
			impersonate: false,
		},
		{
			name:        "failed_to_parse_token",
			token:       "invalid-token",
			impersonate: true,
		},
		{
			name:        "user_claim_not_found",
			token:       createTestToken(t, jwt.MapClaims{}), // No user claim
			impersonate: true,
		},
		{
			name:        "user_claim_is_not_a_string",
			token:       createTestToken(t, jwt.MapClaims{"sub": 123}), // User claim is not a string
			impersonate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock RoundTripper using mockery
			mockRoundTripper := &mocks.MockRoundTripper{}

			// Set up the mock to return a successful response
			mockRoundTripper.EXPECT().
				RoundTrip(mock.Anything).
				Return(&http.Response{StatusCode: http.StatusOK}, nil)

			// If impersonation is expected, set up a more specific expectation
			if tt.expectedUser != "" {
				mockRoundTripper.EXPECT().
					RoundTrip(mock.MatchedBy(func(req *http.Request) bool {
						return req.Header.Get(transport.ImpersonateUserHeader) == tt.expectedUser
					})).
					Return(&http.Response{StatusCode: http.StatusOK}, nil)
			}

			log, err := logger.New(logger.DefaultConfig())
			require.NoError(t, err)

			rt := NewRoundTripper(log, mockRoundTripper, "sub", tt.impersonate)

			req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
			if tt.token != "" {
				ctx := context.WithValue(req.Context(), TokenKey{}, tt.token)
				req = req.WithContext(ctx)
			}

			resp, err := rt.RoundTrip(req)
			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			mockRoundTripper.AssertExpectations(t)
		})
	}
}

// Helper function to create a test JWT token
func createTestToken(t *testing.T, claims jwt.MapClaims) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedToken, err := token.SignedString([]byte("test-secret"))
	require.NoError(t, err)
	return signedToken
}
