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

// ClusterRegistry manages multiple target clusters
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

// GetAllClusters returns all clusters
func (cr *ClusterRegistry) GetAllClusters() map[string]*TargetCluster {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	// Create a copy to avoid concurrent access issues
	result := make(map[string]*TargetCluster)
	for name, cluster := range cr.clusters {
		result[name] = cluster
	}
	return result
}

// GetHealthyClusters returns only healthy clusters
func (cr *ClusterRegistry) GetHealthyClusters() map[string]*TargetCluster {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	result := make(map[string]*TargetCluster)
	for name, cluster := range cr.clusters {
		if cluster.IsHealthy() {
			result[name] = cluster
		}
	}
	return result
}

// GetClusterStats returns statistics about the registry
func (cr *ClusterRegistry) GetClusterStats() ClusterStats {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	stats := ClusterStats{
		Total: len(cr.clusters),
	}

	for _, cluster := range cr.clusters {
		switch cluster.GetState() {
		case StateConnected:
			stats.Connected++
		case StateConnecting:
			stats.Connecting++
		case StateDisconnected:
			stats.Disconnected++
		case StateError:
			stats.Error++
		}
	}

	return stats
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

// ClusterStats represents statistics about the cluster registry
type ClusterStats struct {
	Total        int `json:"total"`
	Connected    int `json:"connected"`
	Connecting   int `json:"connecting"`
	Disconnected int `json:"disconnected"`
	Error        int `json:"error"`
}
