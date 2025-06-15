package targetcluster

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/go-openapi/spec"
	"github.com/openmfp/golang-commons/logger"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/kcp"

	appConfig "github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"github.com/openmfp/kubernetes-graphql-gateway/gateway/resolver"
	"github.com/openmfp/kubernetes-graphql-gateway/gateway/schema"
)

// TargetCluster represents a single target Kubernetes cluster
type TargetCluster struct {
	name     string
	metadata *ClusterMetadata
	client   client.WithWatch
	handler  *GraphQLHandler
	log      *logger.Logger
}

// NewTargetCluster creates a new TargetCluster from a schema file
func NewTargetCluster(
	name string,
	schemaFilePath string,
	log *logger.Logger,
	appCfg appConfig.Config,
	roundTripperFactory func(*rest.Config) http.RoundTripper,
) (*TargetCluster, error) {
	fileData, err := readSchemaFile(schemaFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema file: %w", err)
	}

	cluster := &TargetCluster{
		name:     name,
		metadata: fileData.Metadata,
		log:      log,
	}

	// Connect to cluster
	if err := cluster.connect(appCfg, roundTripperFactory); err != nil {
		return nil, fmt.Errorf("failed to connect to cluster: %w", err)
	}

	// Create GraphQL schema and handler
	if err := cluster.createHandler(fileData.Definitions, appCfg); err != nil {
		return nil, fmt.Errorf("failed to create GraphQL handler: %w", err)
	}

	log.Info().
		Str("cluster", name).
		Str("endpoint", cluster.GetEndpoint(appCfg)).
		Msg("Successfully created target cluster")

	return cluster, nil
}

// connect establishes connection to the target cluster
func (tc *TargetCluster) connect(appCfg appConfig.Config, roundTripperFactory func(*rest.Config) http.RoundTripper) error {
	var config *rest.Config
	var err error

	if tc.metadata != nil && tc.metadata.Host != "" {
		// Connect to remote cluster using metadata
		config, err = buildConfigFromMetadata(tc.metadata)
		if err != nil {
			return fmt.Errorf("failed to build config from metadata: %w", err)
		}
		tc.log.Info().Str("host", tc.metadata.Host).Msg("Connecting to remote cluster")
	} else if appCfg.LocalDevelopment {
		// Use kubeconfig from environment in development mode
		config, err = clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
		if err != nil {
			return fmt.Errorf("failed to build config from kubeconfig: %w", err)
		}
		tc.log.Info().Msg("Using kubeconfig from environment (development mode)")
	} else {
		return fmt.Errorf("no cluster metadata provided and not in development mode")
	}

	// Apply round tripper
	if roundTripperFactory != nil {
		config.Wrap(func(rt http.RoundTripper) http.RoundTripper {
			return roundTripperFactory(config)
		})
	}

	// Create client
	tc.client, err = kcp.NewClusterAwareClientWithWatch(config, client.Options{})
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	return nil
}

// createHandler creates the GraphQL schema and handler
func (tc *TargetCluster) createHandler(definitions map[string]interface{}, appCfg appConfig.Config) error {
	// Convert definitions to spec format
	specDefs, err := convertToSpecDefinitions(definitions)
	if err != nil {
		return fmt.Errorf("failed to convert definitions: %w", err)
	}

	// Create resolver
	resolverProvider := resolver.New(tc.log, tc.client)

	// Create schema gateway
	schemaGateway, err := schema.New(tc.log, specDefs, resolverProvider)
	if err != nil {
		return fmt.Errorf("failed to create GraphQL schema: %w", err)
	}

	// Create handler
	graphqlServer := NewGraphQLServer(tc.log, appCfg)
	tc.handler = graphqlServer.CreateHandler(schemaGateway.GetSchema())

	return nil
}

// GetName returns the cluster name
func (tc *TargetCluster) GetName() string {
	return tc.name
}

// GetEndpoint returns the HTTP endpoint for this cluster's GraphQL API
func (tc *TargetCluster) GetEndpoint(appCfg appConfig.Config) string {
	path := tc.name
	if tc.metadata != nil && tc.metadata.Path != "" {
		path = tc.metadata.Path
	}
	return fmt.Sprintf("http://localhost:%s/%s/graphql", appCfg.Gateway.Port, path)
}

// ServeHTTP handles HTTP requests for this cluster
func (tc *TargetCluster) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if tc.handler == nil || tc.handler.Handler == nil {
		http.Error(w, "Cluster not ready", http.StatusServiceUnavailable)
		return
	}
	tc.handler.Handler.ServeHTTP(w, r)
}

// readSchemaFile reads and parses a schema file
func readSchemaFile(filePath string) (*FileData, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var fileData FileData
	if err := json.Unmarshal(data, &fileData); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return &fileData, nil
}

// convertToSpecDefinitions converts map definitions to go-openapi spec format
func convertToSpecDefinitions(definitions map[string]interface{}) (spec.Definitions, error) {
	data, err := json.Marshal(definitions)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal definitions: %w", err)
	}

	var specDefs spec.Definitions
	if err := json.Unmarshal(data, &specDefs); err != nil {
		return nil, fmt.Errorf("failed to unmarshal to spec definitions: %w", err)
	}

	return specDefs, nil
}
