package handler

import (
	"net/http"

	"github.com/openmfp/golang-commons/logger/testlogger"
	appConfig "github.com/openmfp/kubernetes-graphql-gateway/common/config"
)

// NewHTTPServerForTest creates an HTTPServer instance for testing
func NewHTTPServerForTest() *HTTPServer {
	cfg := appConfig.Config{}
	cfg.Gateway.Cors.Enabled = true
	cfg.Gateway.Cors.AllowedOrigins = []string{"*"}
	cfg.Gateway.Cors.AllowedHeaders = []string{"Authorization"}
	cfg.LocalDevelopment = true

	return NewHTTPServer(testlogger.New().HideLogOutput().Logger, cfg)
}

// SetHandlerForTest sets a handler for testing purposes
func (h *HTTPServer) SetHandlerForTest(workspace string, handler http.Handler) {
	h.SetHandler(workspace, &GraphQLHandler{
		Handler: handler,
	})
}
