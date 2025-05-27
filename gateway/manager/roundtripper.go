package manager

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/openmfp/golang-commons/logger"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"

	"github.com/openmfp/kubernetes-graphql-gateway/common/config"
)

type TokenKey struct{}

type roundTripper struct {
	log                                  *logger.Logger
	adminRT, tokenOnlyRT, unauthorizedRT http.RoundTripper
	appCfg                               config.Config
}

type unauthorizedRoundTripper struct{}

func NewRoundTripper(log *logger.Logger, appCfg config.Config, adminRoundTripper, tokenOnlyRT, unauthorizedRT http.RoundTripper) http.RoundTripper {
	return &roundTripper{
		log:            log,
		adminRT:        adminRoundTripper,
		tokenOnlyRT:    tokenOnlyRT,
		unauthorizedRT: unauthorizedRT,
		appCfg:         appCfg,
	}
}

// NewTokenOnlyTransport does not include any admin credentials.
// It is intended to rely solely on token authentication.
func NewTokenOnlyTransport(tlsConfig rest.TLSClientConfig) http.RoundTripper {
	newTlsConfig := &tls.Config{}
	if len(tlsConfig.CAData) > 0 || tlsConfig.CAFile != "" {
		rootCAs := x509.NewCertPool()
		if len(tlsConfig.CAData) > 0 {
			rootCAs.AppendCertsFromPEM(tlsConfig.CAData)
		}
		newTlsConfig.RootCAs = rootCAs
	}

	return &http.Transport{
		TLSClientConfig: newTlsConfig,
	}
}

// NewUnauthorizedRoundTripper returns a RoundTripper that always returns 401 Unauthorized
func NewUnauthorizedRoundTripper() http.RoundTripper {
	return &unauthorizedRoundTripper{}
}

func (rt *roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if rt.appCfg.LocalDevelopment {
		return rt.adminRT.RoundTrip(req)
	}

	// Allow unauthenticated access for Kubernetes API discovery requests
	if isDiscoveryRequest(req) {
		return rt.adminRT.RoundTrip(req)
	}

	token, ok := req.Context().Value(TokenKey{}).(string)
	if !ok || token == "" {
		rt.log.Debug().Msg("No token found in context, denying request")
		return rt.unauthorizedRT.RoundTrip(req)
	}

	if !rt.appCfg.Gateway.ShouldImpersonate {
		return transport.NewBearerAuthRoundTripper(token, rt.tokenOnlyRT).RoundTrip(req)
	}

	claims := jwt.MapClaims{}
	_, _, err := jwt.NewParser().ParseUnverified(token, claims)
	if err != nil {
		rt.log.Error().Err(err).Msg("Failed to parse token for impersonation, denying request")
		return rt.unauthorizedRT.RoundTrip(req)
	}

	userNameRaw, ok := claims[rt.appCfg.Gateway.UsernameClaim]
	if !ok {
		rt.log.Debug().Msg("No user claim found in token for impersonation, denying request")
		return rt.unauthorizedRT.RoundTrip(req)
	}

	userName, ok := userNameRaw.(string)
	if !ok || userName == "" {
		rt.log.Debug().Msg("User claim is not a valid string for impersonation, denying request")
		return rt.unauthorizedRT.RoundTrip(req)
	}

	impersonatingRT := transport.NewImpersonatingRoundTripper(transport.ImpersonationConfig{
		UserName: userName,
	}, rt.tokenOnlyRT)

	return transport.NewBearerAuthRoundTripper(token, impersonatingRT).RoundTrip(req)
}

func (u *unauthorizedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusUnauthorized,
		Request:    req,
		Body:       http.NoBody,
	}, nil
}

func isDiscoveryRequest(req *http.Request) bool {
	if req.Method != http.MethodGet {
		return false
	}

	path := strings.TrimPrefix(req.URL.Path, req.URL.RawPath) // remove /clusters/{workspace} if present
	path = strings.Trim(path, "/")                            // remove leading and trailing slashes
	parts := strings.Split(path, "/")

	switch {
	case len(parts) == 1 && (parts[0] == "api" || parts[0] == "apis"):
		return true // /api or /apis (root groups)
	case len(parts) == 2 && parts[0] == "apis":
		return true // /apis/<group>
	case len(parts) == 2 && parts[0] == "api":
		return true // /api/v1 (core group version)
	case len(parts) == 3 && parts[0] == "apis":
		return true // /apis/<group>/<version>
	default:
		return false
	}
}
