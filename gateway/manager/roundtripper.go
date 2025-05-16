package manager

import (
	"io"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/openmfp/golang-commons/logger"
	"k8s.io/client-go/transport"
)

type TokenKey struct{}

type roundTripper struct {
	userClaim    string
	log          *logger.Logger
	unauthorized http.RoundTripper
	impersonate  bool
}

type unauthorizedRoundTripper struct{}

func NewRoundTripper(log *logger.Logger, userNameClaim string, impersonate bool) http.RoundTripper {
	return &roundTripper{
		log:          log,
		unauthorized: &unauthorizedRoundTripper{},
		userClaim:    userNameClaim,
		impersonate:  impersonate,
	}
}

// TODO: unauthorizedRoundTripper.RoundTrip needs to be tested, but first we need to fix token absence issue.

func (u *unauthorizedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusUnauthorized,
		Body:       io.NopCloser(strings.NewReader("unauthorized")),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func (rt *roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	token, ok := req.Context().Value(TokenKey{}).(string)
	if !ok {
		rt.log.Debug().Msg("No token found in context")
		return rt.unauthorized.RoundTrip(req)
	}

	if !rt.impersonate {
		req.Header.Del("Authorization")
		t := transport.NewBearerAuthRoundTripper(token, rt.unauthorized)
		return t.RoundTrip(req)
	}

	claims := jwt.MapClaims{}
	_, _, err := jwt.NewParser().ParseUnverified(token, claims)
	if err != nil {
		rt.log.Error().Err(err).Msg("Failed to parse token")
		return rt.unauthorized.RoundTrip(req)
	}

	userNameRaw, ok := claims[rt.userClaim]
	if !ok {
		rt.log.Debug().Msg("No user claim found in token")
		return rt.unauthorized.RoundTrip(req)
	}

	userName, ok := userNameRaw.(string)
	if !ok {
		rt.log.Debug().Msg("User claim is not a string")
		return rt.unauthorized.RoundTrip(req)
	}

	t := transport.NewImpersonatingRoundTripper(transport.ImpersonationConfig{
		UserName: userName,
	}, rt.unauthorized)

	return t.RoundTrip(req)
}
