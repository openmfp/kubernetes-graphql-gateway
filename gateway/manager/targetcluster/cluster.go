package targetcluster

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/go-openapi/spec"
	"github.com/graphql-go/graphql"
	"github.com/openmfp/golang-commons/logger"
	"k8s.io/client-go/rest"

	appConfig "github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"github.com/openmfp/kubernetes-graphql-gateway/gateway/manager/handler"
	"github.com/openmfp/kubernetes-graphql-gateway/gateway/resolver"
	"github.com/openmfp/kubernetes-graphql-gateway/gateway/schema"
)

// TargetCluster represents a single target Kubernetes cluster with all its associated resources
type TargetCluster struct {
	name       string
	metadata   *ClusterMetadata
	connection *Connection
	resolver   resolver.Provider
	handler    *handler.GraphQLHandler
	schema     *graphql.Schema
	log        *logger.Logger
	appCfg     appConfig.Config
	lastError  error
}

// ClusterState represents the current state of a cluster connection
type ClusterState int

const (
	StateDisconnected ClusterState = iota
	StateConnecting
	StateConnected
	StateError
)

// NewTargetCluster creates a new TargetCluster from a schema file
func NewTargetCluster(
	name string,
	schemaFilePath string,
	log *logger.Logger,
	appCfg appConfig.Config,
	roundTripperFactory func(*rest.Config) http.RoundTripper,
) (*TargetCluster, error) {
	// Read and parse schema file
	fileData, err := readSchemaFile(schemaFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema file: %w", err)
	}

	if fileData.Metadata == nil || fileData.Metadata.Host == "" {
		return nil, fmt.Errorf("no cluster metadata found in file %s - all schema files must contain x-cluster-metadata", name)
	}

	cluster := &TargetCluster{
		name:     name,
		metadata: fileData.Metadata,
		log:      log,
		appCfg:   appCfg,
	}

	// Establish connection
	if err := cluster.connect(roundTripperFactory); err != nil {
		cluster.lastError = err
		return cluster, fmt.Errorf("failed to establish connection: %w", err)
	}

	// Create GraphQL schema
	if err := cluster.updateSchema(fileData.Definitions); err != nil {
		cluster.lastError = err
		return cluster, fmt.Errorf("failed to create GraphQL schema: %w", err)
	}

	cluster.log.Info().
		Str("cluster", name).
		Str("host", fileData.Metadata.Host).
		Str("endpoint", cluster.GetEndpoint()).
		Msg("Successfully created target cluster")

	// Log endpoint registration prominently
	cluster.log.Info().
		Str("endpoint", cluster.GetEndpoint()).
		Msg("Registered endpoint")

	return cluster, nil
}

// GetName returns the cluster name
func (tc *TargetCluster) GetName() string {
	return tc.name
}

// GetMetadata returns the cluster metadata
func (tc *TargetCluster) GetMetadata() *ClusterMetadata {
	return tc.metadata
}

// GetEndpoint returns the HTTP endpoint for this cluster's GraphQL API
func (tc *TargetCluster) GetEndpoint() string {
	path := tc.metadata.Path
	if path == "" {
		path = tc.name
	}
	return fmt.Sprintf("http://localhost:%s/%s/graphql", tc.appCfg.Gateway.Port, path)
}

// GetHandler returns the GraphQL handler for HTTP requests
func (tc *TargetCluster) GetHandler() *handler.GraphQLHandler {
	return tc.handler
}

// IsHealthy checks if the cluster connection is healthy
func (tc *TargetCluster) IsHealthy() bool {
	return tc.connection != nil && tc.connection.IsHealthy() && tc.lastError == nil
}

// GetState returns the current state of the cluster
func (tc *TargetCluster) GetState() ClusterState {
	if tc.lastError != nil {
		return StateError
	}
	if tc.connection == nil {
		return StateDisconnected
	}
	if tc.IsHealthy() {
		return StateConnected
	}
	return StateConnecting
}

// GetLastError returns the last error encountered
func (tc *TargetCluster) GetLastError() error {
	return tc.lastError
}

// ServeHTTP handles HTTP requests for this cluster
func (tc *TargetCluster) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if tc.handler == nil || tc.handler.Handler == nil {
		http.Error(w, "Cluster not ready", http.StatusServiceUnavailable)
		return
	}
	tc.handler.Handler.ServeHTTP(w, r)
}

// UpdateFromFile updates the cluster configuration from a schema file
func (tc *TargetCluster) UpdateFromFile(schemaFilePath string, roundTripperFactory func(*rest.Config) http.RoundTripper) error {
	// Read and parse schema file
	fileData, err := readSchemaFile(schemaFilePath)
	if err != nil {
		tc.lastError = err
		return fmt.Errorf("failed to read schema file: %w", err)
	}

	// Update metadata
	tc.metadata = fileData.Metadata

	// Reconnect if needed
	if err := tc.connect(roundTripperFactory); err != nil {
		tc.lastError = err
		return fmt.Errorf("failed to reconnect: %w", err)
	}

	// Update schema
	if err := tc.updateSchema(fileData.Definitions); err != nil {
		tc.lastError = err
		return fmt.Errorf("failed to update schema: %w", err)
	}

	tc.lastError = nil
	tc.log.Info().
		Str("cluster", tc.name).
		Str("host", tc.metadata.Host).
		Str("endpoint", tc.GetEndpoint()).
		Msg("Successfully updated target cluster")

	// Log endpoint registration prominently
	tc.log.Info().
		Str("endpoint", tc.GetEndpoint()).
		Msg("Registered endpoint")

	return nil
}

// Close closes the cluster connection and cleans up resources
func (tc *TargetCluster) Close() error {
	tc.connection = nil
	tc.resolver = nil
	tc.handler = nil
	tc.schema = nil
	tc.log.Info().Str("cluster", tc.name).Msg("Closed target cluster")
	return nil
}

// connect establishes connection to the target cluster
func (tc *TargetCluster) connect(roundTripperFactory func(*rest.Config) http.RoundTripper) error {
	tc.log.Info().
		Str("cluster", tc.name).
		Str("host", tc.metadata.Host).
		Str("path", tc.metadata.Path).
		Msg("Connecting to target cluster")

	// Create connection
	connection, err := NewConnection(tc.metadata, roundTripperFactory)
	if err != nil {
		return fmt.Errorf("failed to create connection: %w", err)
	}

	tc.connection = connection

	// Create resolver
	tc.resolver = resolver.New(tc.log, connection.GetClient())

	tc.log.Info().
		Str("cluster", tc.name).
		Str("host", tc.metadata.Host).
		Msg("Successfully connected to target cluster")

	return nil
}

// updateSchema creates or updates the GraphQL schema for this cluster
func (tc *TargetCluster) updateSchema(definitions map[string]interface{}) error {
	// Convert definitions to the expected format
	specDefinitions, err := convertToSpecDefinitions(definitions)
	if err != nil {
		return fmt.Errorf("failed to convert definitions: %w", err)
	}

	// Create GraphQL schema
	schemaGateway, err := schema.New(tc.log, specDefinitions, tc.resolver)
	if err != nil {
		return fmt.Errorf("failed to create GraphQL schema: %w", err)
	}

	tc.schema = schemaGateway.GetSchema()

	// Create HTTP handler through an HTTPServer (to get proper Handler creation with playground/GraphiQL)
	httpServer := handler.NewHTTPServer(tc.log, tc.appCfg)
	tc.handler = httpServer.CreateHandler(tc.schema)

	return nil
}

// readSchemaFile reads a schema file and extracts both OpenAPI definitions and cluster metadata
func readSchemaFile(filePath string) (*FileData, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer file.Close()

	var schemaData map[string]interface{}
	if err := json.NewDecoder(file).Decode(&schemaData); err != nil {
		return nil, fmt.Errorf("failed to decode JSON from file %s: %w", filePath, err)
	}

	// Extract OpenAPI definitions
	var definitions map[string]interface{}
	if defsRaw, exists := schemaData["definitions"]; exists {
		if defs, ok := defsRaw.(map[string]interface{}); ok {
			definitions = defs
		}
	}

	// Extract cluster metadata
	var metadata ClusterMetadata
	if metadataRaw, exists := schemaData["x-cluster-metadata"]; exists {
		metadataBytes, err := json.Marshal(metadataRaw)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal cluster metadata: %w", err)
		}

		if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal cluster metadata: %w", err)
		}
	}

	return &FileData{
		Definitions: definitions,
		Metadata:    &metadata,
	}, nil
}

// convertToSpecDefinitions converts map[string]interface{} to spec.Definitions
func convertToSpecDefinitions(definitions map[string]interface{}) (spec.Definitions, error) {
	if definitions == nil {
		return spec.Definitions{}, nil
	}

	// Marshal and unmarshal to convert types
	defsBytes, err := json.Marshal(definitions)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal definitions: %w", err)
	}

	var specDefs spec.Definitions
	if err := json.Unmarshal(defsBytes, &specDefs); err != nil {
		return nil, fmt.Errorf("failed to unmarshal definitions: %w", err)
	}

	return specDefs, nil
}
