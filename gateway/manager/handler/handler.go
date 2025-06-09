package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/handler"
	"github.com/kcp-dev/logicalcluster/v3"
	"sigs.k8s.io/controller-runtime/pkg/kontext"

	"github.com/openmfp/golang-commons/logger"
	"github.com/openmfp/golang-commons/sentry"

	appConfig "github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"github.com/openmfp/kubernetes-graphql-gateway/gateway/manager/roundtripper"
)

var (
	ErrNoHandlerFound = errors.New("no handler found for workspace")
)

// HandlerStore manages GraphQL handlers for different workspaces
type HandlerStore struct {
	mu       sync.RWMutex
	registry map[string]*GraphQLHandler
}

// NewHandlerStore creates a new handler store
func NewHandlerStore() *HandlerStore {
	return &HandlerStore{
		registry: make(map[string]*GraphQLHandler),
	}
}

// Set stores a handler for a workspace
func (hs *HandlerStore) Set(workspace string, handler *GraphQLHandler) {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	hs.registry[workspace] = handler
}

// Get retrieves a handler for a workspace
func (hs *HandlerStore) Get(workspace string) (*GraphQLHandler, bool) {
	hs.mu.RLock()
	defer hs.mu.RUnlock()
	handler, exists := hs.registry[workspace]
	return handler, exists
}

// Delete removes a handler for a workspace
func (hs *HandlerStore) Delete(workspace string) {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	delete(hs.registry, workspace)
}

// GraphQLHandler wraps a GraphQL schema and HTTP handler
type GraphQLHandler struct {
	Schema  *graphql.Schema
	Handler http.Handler
}

// HTTPServer handles HTTP requests and GraphQL operations
type HTTPServer struct {
	log          *logger.Logger
	AppCfg       appConfig.Config
	handlerStore *HandlerStore
}

// NewHTTPServer creates a new HTTP server
func NewHTTPServer(log *logger.Logger, appCfg appConfig.Config) *HTTPServer {
	return &HTTPServer{
		log:          log,
		AppCfg:       appCfg,
		handlerStore: NewHandlerStore(),
	}
}

// CreateHandler creates a new GraphQL handler from a schema
func (h *HTTPServer) CreateHandler(schema *graphql.Schema) *GraphQLHandler {
	graphqlHandler := handler.New(&handler.Config{
		Schema:     schema,
		Pretty:     h.AppCfg.Gateway.HandlerCfg.Pretty,
		Playground: h.AppCfg.Gateway.HandlerCfg.Playground,
		GraphiQL:   h.AppCfg.Gateway.HandlerCfg.GraphiQL,
	})
	return &GraphQLHandler{
		Schema:  schema,
		Handler: graphqlHandler,
	}
}

// SetHandler implements HandlerRegistry interface
func (h *HTTPServer) SetHandler(filename string, handler interface{}) {
	h.handlerStore.Set(filename, handler.(*GraphQLHandler))
}

// DeleteHandler implements HandlerRegistry interface
func (h *HTTPServer) DeleteHandler(filename string) {
	h.handlerStore.Delete(filename)
}

// ServeHTTP handles HTTP requests
func (h *HTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.handleCORS(w, r) {
		return
	}

	workspace, handler, ok := h.getWorkspaceAndHandler(w, r)
	if !ok {
		return
	}

	if r.Method == http.MethodGet {
		handler.Handler.ServeHTTP(w, r)
		return
	}

	token := getToken(r)

	if !h.handleAuth(w, r, token) {
		return
	}

	r = h.setContexts(r, workspace, token)

	if r.Header.Get("Accept") == "text/event-stream" {
		h.handleSubscription(w, r, handler.Schema)
	} else {
		handler.Handler.ServeHTTP(w, r)
	}
}

func (h *HTTPServer) handleCORS(w http.ResponseWriter, r *http.Request) bool {
	if h.AppCfg.Gateway.Cors.Enabled {
		allowedOrigins := strings.Join(h.AppCfg.Gateway.Cors.AllowedOrigins, ",")
		allowedHeaders := strings.Join(h.AppCfg.Gateway.Cors.AllowedHeaders, ",")
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigins)
		w.Header().Set("Access-Control-Allow-Headers", allowedHeaders)
		// setting cors allowed methods is not needed for this service,
		// as all graphql methods are part of the cors safelisted methods
		// https://fetch.spec.whatwg.org/#cors-safelisted-method

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return true
		}
	}
	return false
}

// getWorkspaceAndHandler extracts the workspace from the path, finds the handler, and handles errors.
// Returns workspace, handler, and ok (true if found, false if error was handled).
func (h *HTTPServer) getWorkspaceAndHandler(w http.ResponseWriter, r *http.Request) (string, *GraphQLHandler, bool) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 2 {
		h.log.Error().Err(fmt.Errorf("invalid path")).Str("path", r.URL.Path).Msg("Error parsing path")
		http.NotFound(w, r)
		return "", nil, false
	}

	workspace := parts[0]

	handler, ok := h.handlerStore.Get(workspace)
	if !ok {
		h.log.Error().Err(ErrNoHandlerFound).Str("workspace", workspace)
		sentry.CaptureError(ErrNoHandlerFound, sentry.Tags{"workspace": workspace})
		http.NotFound(w, r)
		return "", nil, false
	}

	return workspace, handler, true
}

func getToken(r *http.Request) string {
	token := r.Header.Get("Authorization")
	token = strings.TrimPrefix(token, "Bearer ")
	token = strings.TrimPrefix(token, "bearer ")

	return token
}

func (h *HTTPServer) handleAuth(w http.ResponseWriter, r *http.Request, token string) bool {
	if !h.AppCfg.LocalDevelopment {
		if token == "" {
			http.Error(w, "Authorization header is required", http.StatusUnauthorized)
			return false
		}

		if h.AppCfg.IntrospectionAuthentication {
			if h.isIntrospectionQuery(r) {
				ok, err := h.validateToken(r.Context(), token)
				if err != nil {
					h.log.Error().Err(err).Msg("error validating token with k8s")
					http.Error(w, "error validating token", http.StatusInternalServerError)
					return false
				}

				if !ok {
					http.Error(w, "Provided token is not authorized to access the cluster", http.StatusUnauthorized)
					return false
				}
			}
		}
	}
	return true
}

func (h *HTTPServer) isIntrospectionQuery(r *http.Request) bool {
	var params struct {
		Query string `json:"query"`
	}
	bodyBytes, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err == nil {
		if err = json.Unmarshal(bodyBytes, &params); err == nil {
			if strings.Contains(params.Query, "__schema") || strings.Contains(params.Query, "__type") {
				r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
				return true
			}
		}
	}
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	return false
}

// validateToken uses the /version endpoint for a general authentication check.
// TODO: Implement token validation without relying on a single management cluster config
func (h *HTTPServer) validateToken(ctx context.Context, token string) (bool, error) {
	// For now, accept all tokens since we no longer have a central cluster config for validation
	// This should be implemented to validate against the appropriate cluster based on the request
	return true, nil
}

func (h *HTTPServer) setContexts(r *http.Request, workspace, token string) *http.Request {
	if h.AppCfg.EnableKcp {
		r = r.WithContext(kontext.WithCluster(r.Context(), logicalcluster.Name(workspace)))
	}
	return r.WithContext(context.WithValue(r.Context(), roundtripper.TokenKey{}, token))
}

func (h *HTTPServer) handleSubscription(w http.ResponseWriter, r *http.Request, schema *graphql.Schema) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	var params struct {
		Query         string                 `json:"query"`
		OperationName string                 `json:"operationName"`
		Variables     map[string]interface{} `json:"variables"`
	}

	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, "Error parsing JSON request body", http.StatusBadRequest)
		return
	}

	flusher := http.NewResponseController(w)
	r.Body.Close()

	subscriptionParams := graphql.Params{
		Schema:         *schema,
		RequestString:  params.Query,
		VariableValues: params.Variables,
		OperationName:  params.OperationName,
		Context:        r.Context(),
	}

	subscriptionChannel := graphql.Subscribe(subscriptionParams)
	for res := range subscriptionChannel {
		if res == nil {
			continue
		}

		data, err := json.Marshal(res)
		if err != nil {
			h.log.Error().Err(err).Msg("Error marshalling subscription response")
			continue
		}

		fmt.Fprintf(w, "event: next\ndata: %s\n\n", data)
		flusher.Flush()
	}

	fmt.Fprint(w, "event: complete\n\n")
}
