package targetcluster

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/openmfp/golang-commons/logger"
	appConfig "github.com/openmfp/kubernetes-graphql-gateway/common/config"
)

// ClusterRouter routes HTTP requests to the appropriate target cluster
type ClusterRouter struct {
	registry *ClusterRegistry
	log      *logger.Logger
	appCfg   appConfig.Config
}

// NewClusterRouter creates a new cluster router
func NewClusterRouter(
	registry *ClusterRegistry,
	log *logger.Logger,
	appCfg appConfig.Config,
) *ClusterRouter {
	return &ClusterRouter{
		registry: registry,
		log:      log,
		appCfg:   appCfg,
	}
}

// ServeHTTP routes HTTP requests to the appropriate target cluster
func (cr *ClusterRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Handle CORS
	if cr.handleCORS(w, r) {
		return
	}

	// Extract cluster name from path
	clusterName, ok := cr.extractClusterName(w, r)
	if !ok {
		return
	}

	// Get target cluster
	cluster, exists := cr.registry.GetCluster(clusterName)
	if !exists {
		cr.log.Error().
			Str("cluster", clusterName).
			Str("path", r.URL.Path).
			Msg("Target cluster not found")
		http.NotFound(w, r)
		return
	}

	// Check cluster health
	if !cluster.IsHealthy() {
		cr.log.Error().
			Str("cluster", clusterName).
			Str("state", fmt.Sprintf("%d", cluster.GetState())).
			Err(cluster.GetLastError()).
			Msg("Target cluster is not healthy")
		http.Error(w, "Target cluster unavailable", http.StatusServiceUnavailable)
		return
	}

	// Handle GET requests (GraphiQL/Playground) directly
	if r.Method == http.MethodGet {
		cluster.ServeHTTP(w, r)
		return
	}

	// Extract and validate token for non-GET requests
	token := GetToken(r)
	if !cr.handleAuth(w, r, token) {
		return
	}

	// Set contexts for KCP and authentication
	r = SetContexts(r, clusterName, token, cr.appCfg.EnableKcp)

	// Handle subscription requests
	if r.Header.Get("Accept") == "text/event-stream" {
		graphqlServer := NewGraphQLServer(cr.log, cr.appCfg)
		graphqlServer.HandleSubscription(w, r, cluster.GetHandler().Schema)
		return
	}

	// Route to target cluster
	cr.log.Debug().
		Str("cluster", clusterName).
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("Routing request to target cluster")

	cluster.ServeHTTP(w, r)
}

// handleAuth handles authentication for non-GET requests
func (cr *ClusterRouter) handleAuth(w http.ResponseWriter, r *http.Request, token string) bool {
	if !cr.appCfg.LocalDevelopment {
		if token == "" {
			http.Error(w, "Authorization header is required", http.StatusUnauthorized)
			return false
		}

		if cr.appCfg.IntrospectionAuthentication {
			if IsIntrospectionQuery(r) {
				// For now, accept all tokens since we no longer have a central cluster config for validation
				// TODO: Implement token validation against the appropriate cluster based on the request
				return true
			}
		}
	}
	return true
}

// handleCORS handles CORS preflight requests and headers
func (cr *ClusterRouter) handleCORS(w http.ResponseWriter, r *http.Request) bool {
	if cr.appCfg.Gateway.Cors.Enabled {
		allowedOrigins := strings.Join(cr.appCfg.Gateway.Cors.AllowedOrigins, ",")
		allowedHeaders := strings.Join(cr.appCfg.Gateway.Cors.AllowedHeaders, ",")
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigins)
		w.Header().Set("Access-Control-Allow-Headers", allowedHeaders)

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return true
		}
	}
	return false
}

// extractClusterName extracts the cluster name from the request path
// Expected format: /{clusterName}/graphql
func (cr *ClusterRouter) extractClusterName(w http.ResponseWriter, r *http.Request) (string, bool) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 2 {
		cr.log.Error().
			Str("path", r.URL.Path).
			Msg("Invalid path format, expected /{clusterName}/graphql")
		http.NotFound(w, r)
		return "", false
	}

	clusterName := parts[0]
	if clusterName == "" {
		cr.log.Error().
			Str("path", r.URL.Path).
			Msg("Empty cluster name in path")
		http.NotFound(w, r)
		return "", false
	}

	return clusterName, true
}
