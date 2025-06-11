package targetcluster

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	"github.com/openmfp/golang-commons/logger"
	appConfig "github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"k8s.io/client-go/rest"
)

// ClusterRegistry manages multiple target clusters and handles HTTP routing to them
type ClusterRegistry struct {
	mu                  sync.RWMutex
	clusters            map[string]*TargetCluster
	log                 *logger.Logger
	appCfg              appConfig.Config
	roundTripperFactory func(*rest.Config) http.RoundTripper
}

// NewClusterRegistry creates a new cluster registry
func NewClusterRegistry(
	log *logger.Logger,
	appCfg appConfig.Config,
	roundTripperFactory func(*rest.Config) http.RoundTripper,
) *ClusterRegistry {
	return &ClusterRegistry{
		clusters:            make(map[string]*TargetCluster),
		log:                 log,
		appCfg:              appCfg,
		roundTripperFactory: roundTripperFactory,
	}
}

// LoadCluster loads a target cluster from a schema file
func (cr *ClusterRegistry) LoadCluster(schemaFilePath string) error {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	// Extract cluster name from filename
	name := strings.TrimSuffix(filepath.Base(schemaFilePath), filepath.Ext(schemaFilePath))

	cr.log.Info().
		Str("cluster", name).
		Str("file", schemaFilePath).
		Msg("Loading target cluster")

	// Create or update cluster
	cluster, err := NewTargetCluster(name, schemaFilePath, cr.log, cr.appCfg, cr.roundTripperFactory)
	if err != nil {
		return fmt.Errorf("failed to create target cluster %s: %w", name, err)
	}

	// Store cluster
	if existingCluster, exists := cr.clusters[name]; exists {
		// Close existing cluster
		existingCluster.Close()
	}

	cr.clusters[name] = cluster

	cr.log.Info().
		Str("cluster", name).
		Str("endpoint", cluster.GetEndpoint()).
		Msg("Successfully loaded target cluster")

	return nil
}

// UpdateCluster updates an existing cluster from a schema file
func (cr *ClusterRegistry) UpdateCluster(schemaFilePath string) error {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	// Extract cluster name from filename
	name := strings.TrimSuffix(filepath.Base(schemaFilePath), filepath.Ext(schemaFilePath))

	cr.log.Info().
		Str("cluster", name).
		Str("file", schemaFilePath).
		Msg("Updating target cluster")

	cluster, exists := cr.clusters[name]
	if !exists {
		// If cluster doesn't exist, load it
		cr.mu.Unlock()
		return cr.LoadCluster(schemaFilePath)
	}

	// Update existing cluster
	if err := cluster.UpdateFromFile(schemaFilePath, cr.roundTripperFactory); err != nil {
		return fmt.Errorf("failed to update target cluster %s: %w", name, err)
	}

	cr.log.Info().
		Str("cluster", name).
		Str("endpoint", cluster.GetEndpoint()).
		Msg("Successfully updated target cluster")

	return nil
}

// RemoveCluster removes a cluster by schema file path
func (cr *ClusterRegistry) RemoveCluster(schemaFilePath string) error {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	// Extract cluster name from filename
	name := strings.TrimSuffix(filepath.Base(schemaFilePath), filepath.Ext(schemaFilePath))

	cr.log.Info().
		Str("cluster", name).
		Str("file", schemaFilePath).
		Msg("Removing target cluster")

	cluster, exists := cr.clusters[name]
	if !exists {
		cr.log.Warn().
			Str("cluster", name).
			Msg("Attempted to remove non-existent cluster")
		return nil
	}

	// Close and remove cluster
	cluster.Close()
	delete(cr.clusters, name)

	cr.log.Info().
		Str("cluster", name).
		Msg("Successfully removed target cluster")

	return nil
}

// GetCluster returns a cluster by name
func (cr *ClusterRegistry) GetCluster(name string) (*TargetCluster, bool) {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	cluster, exists := cr.clusters[name]
	return cluster, exists
}

// Close closes all clusters and cleans up the registry
func (cr *ClusterRegistry) Close() error {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	for name, cluster := range cr.clusters {
		cluster.Close()
		cr.log.Info().Str("cluster", name).Msg("Closed cluster during registry shutdown")
	}

	cr.clusters = make(map[string]*TargetCluster)
	cr.log.Info().Msg("Closed cluster registry")
	return nil
}

// ServeHTTP routes HTTP requests to the appropriate target cluster
func (cr *ClusterRegistry) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	cluster, exists := cr.GetCluster(clusterName)
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
func (cr *ClusterRegistry) handleAuth(w http.ResponseWriter, r *http.Request, token string) bool {
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
func (cr *ClusterRegistry) handleCORS(w http.ResponseWriter, r *http.Request) bool {
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
func (cr *ClusterRegistry) extractClusterName(w http.ResponseWriter, r *http.Request) (string, bool) {
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
