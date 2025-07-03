package targetcluster

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	"github.com/openmfp/golang-commons/logger"
	appConfig "github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"k8s.io/client-go/rest"
)

// RoundTripperFactory creates HTTP round trippers for authentication
type RoundTripperFactory func(*rest.Config) http.RoundTripper

// ClusterRegistry manages multiple target clusters and handles HTTP routing to them
type ClusterRegistry struct {
	mu                  sync.RWMutex
	clusters            map[string]*TargetCluster
	log                 *logger.Logger
	appCfg              appConfig.Config
	roundTripperFactory RoundTripperFactory
}

// NewClusterRegistry creates a new cluster registry
func NewClusterRegistry(
	log *logger.Logger,
	appCfg appConfig.Config,
	roundTripperFactory RoundTripperFactory,
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
	cr.clusters[name] = cluster

	return nil
}

// UpdateCluster updates an existing cluster from a schema file
func (cr *ClusterRegistry) UpdateCluster(schemaFilePath string) error {
	// For simplified implementation, just reload the cluster
	err := cr.RemoveCluster(schemaFilePath)
	if err != nil {
		return err
	}

	return cr.LoadCluster(schemaFilePath)
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

	_, exists := cr.clusters[name]
	if !exists {
		cr.log.Warn().
			Str("cluster", name).
			Msg("Attempted to remove non-existent cluster")
		return nil
	}

	// Remove cluster (no cleanup needed in simplified version)
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

	for name := range cr.clusters {
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

	// No health checking in simplified version - clusters are either working or not loaded

	// Handle GET requests (GraphiQL/Playground) directly
	if r.Method == http.MethodGet {
		cluster.ServeHTTP(w, r)
		return
	}

	// Extract and validate token for non-GET requests
	token := GetToken(r)
	if !cr.handleAuth(w, r, token, cluster) {
		return
	}

	// Set contexts for KCP and authentication
	r = SetContexts(r, clusterName, token, cr.appCfg.EnableKcp)

	// Handle subscription requests
	if r.Header.Get("Accept") == "text/event-stream" {
		// Subscriptions will be handled by the cluster's ServeHTTP method
		cluster.ServeHTTP(w, r)
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
func (cr *ClusterRegistry) handleAuth(w http.ResponseWriter, r *http.Request, token string, cluster *TargetCluster) bool {
	if !cr.appCfg.LocalDevelopment {
		if token == "" {
			http.Error(w, "Authorization header is required", http.StatusUnauthorized)
			return false
		}

		if cr.appCfg.IntrospectionAuthentication {
			if IsIntrospectionQuery(r) {
				valid, err := cr.validateToken(token, cluster)
				if err != nil {
					cr.log.Error().Err(err).Str("cluster", cluster.name).Msg("Error validating token")
					http.Error(w, "Token validation failed", http.StatusUnauthorized)
					return false
				}
				if !valid {
					cr.log.Debug().Str("cluster", cluster.name).Msg("Invalid token for introspection query")
					http.Error(w, "Invalid token", http.StatusUnauthorized)
					return false
				}
			}
		}
	}
	return true
}

// handleCORS handles CORS preflight requests and headers
func (cr *ClusterRegistry) handleCORS(w http.ResponseWriter, r *http.Request) bool {
	if cr.appCfg.Gateway.Cors.Enabled {
		w.Header().Set("Access-Control-Allow-Origin", cr.appCfg.Gateway.Cors.AllowedOrigins)
		w.Header().Set("Access-Control-Allow-Headers", cr.appCfg.Gateway.Cors.AllowedHeaders)

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return true
		}
	}
	return false
}

func (cr *ClusterRegistry) validateToken(token string, cluster *TargetCluster) (bool, error) {
	if cluster == nil {
		return false, errors.New("no cluster provided to validate token")
	}

	cr.log.Debug().Str("cluster", cluster.name).Msg("Validating token for introspection query")

	// Get the cluster's config
	clusterConfig := cluster.GetConfig()
	if clusterConfig == nil {
		return false, fmt.Errorf("cluster %s has no config", cluster.name)
	}

	// Create a new config with the token to validate
	cfg := &rest.Config{
		Host: clusterConfig.Host,
		TLSClientConfig: rest.TLSClientConfig{
			CAFile:   clusterConfig.TLSClientConfig.CAFile,
			CAData:   clusterConfig.TLSClientConfig.CAData,
			Insecure: clusterConfig.TLSClientConfig.Insecure,
		},
		BearerToken: token,
	}

	cr.log.Debug().Str("cluster", cluster.name).Str("host", cfg.Host).Msg("Creating HTTP client for token validation")

	// Create HTTP client for validation
	httpClient, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return false, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	// Make a request to validate the token
	ctx := context.Background()
	versionURL := fmt.Sprintf("%s/version", cfg.Host)
	req, err := http.NewRequestWithContext(ctx, "GET", versionURL, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}

	cr.log.Debug().Str("cluster", cluster.name).Str("url", versionURL).Msg("Making token validation request")

	resp, err := httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to make validation request: %w", err)
	}
	defer resp.Body.Close()

	cr.log.Debug().Str("cluster", cluster.name).Int("status", resp.StatusCode).Msg("Token validation response received")

	// Check response status
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		cr.log.Debug().Str("cluster", cluster.name).Msg("Token validation failed - unauthorized")
		return false, nil
	case http.StatusOK:
		cr.log.Debug().Str("cluster", cluster.name).Msg("Token validation successful")
		return true, nil
	default:
		return false, fmt.Errorf("unexpected status code from /version: %d", resp.StatusCode)
	}
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
