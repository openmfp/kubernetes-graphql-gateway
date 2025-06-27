package targetcluster

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/go-openapi/spec"
	"github.com/openmfp/golang-commons/logger"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/kcp"

	appConfig "github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"github.com/openmfp/kubernetes-graphql-gateway/gateway/resolver"
	"github.com/openmfp/kubernetes-graphql-gateway/gateway/schema"
	kcputil "github.com/openmfp/kubernetes-graphql-gateway/listener/reconciler/kcp"
)

// FileData represents the data extracted from a schema file
type FileData struct {
	Definitions     map[string]interface{} `json:"definitions"`
	ClusterMetadata *ClusterMetadata       `json:"x-cluster-metadata,omitempty"`
}

// ClusterMetadata represents the cluster connection metadata stored in schema files
type ClusterMetadata struct {
	Host string        `json:"host"`
	Path string        `json:"path,omitempty"`
	Auth *AuthMetadata `json:"auth,omitempty"`
	CA   *CAMetadata   `json:"ca,omitempty"`
}

// AuthMetadata represents authentication information
type AuthMetadata struct {
	Type       string `json:"type"`
	Token      string `json:"token,omitempty"`
	Kubeconfig string `json:"kubeconfig,omitempty"`
	CertData   string `json:"certData,omitempty"`
	KeyData    string `json:"keyData,omitempty"`
}

// CAMetadata represents CA certificate information
type CAMetadata struct {
	Data string `json:"data"`
}

// TargetCluster represents a single target Kubernetes cluster
type TargetCluster struct {
	name          string
	client        client.WithWatch
	handler       *GraphQLHandler
	graphqlServer *GraphQLServer
	log           *logger.Logger
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
		name: name,
		log:  log,
	}

	// Connect to cluster - use metadata if available, otherwise fall back to standard config
	if err := cluster.connect(appCfg, fileData.ClusterMetadata, roundTripperFactory); err != nil {
		return nil, fmt.Errorf("failed to connect to cluster: %w", err)
	}

	// Create GraphQL schema and handler
	if err := cluster.createHandler(fileData.Definitions, appCfg); err != nil {
		return nil, fmt.Errorf("failed to create GraphQL handler: %w", err)
	}

	log.Info().
		Str("cluster", name).
		Str("endpoint", cluster.GetEndpoint(appCfg)).
		Msg("Registered endpoint")

	return cluster, nil
}

// connect establishes connection to the target cluster
func (tc *TargetCluster) connect(appCfg appConfig.Config, metadata *ClusterMetadata, roundTripperFactory func(*rest.Config) http.RoundTripper) error {
	var config *rest.Config
	var err error

	// In multicluster mode, we MUST have metadata to connect
	if !appCfg.EnableKcp && appCfg.MultiCluster {
		if metadata == nil {
			return fmt.Errorf("multicluster mode requires cluster metadata in schema file")
		}

		tc.log.Info().
			Str("cluster", tc.name).
			Str("host", metadata.Host).
			Msg("Using cluster metadata for connection (multicluster mode)")

		config, err = buildConfigFromMetadata(metadata, tc.log)
		if err != nil {
			return fmt.Errorf("failed to build config from metadata: %w", err)
		}
	} else {
		// Single cluster or KCP mode - use standard config
		tc.log.Info().
			Str("cluster", tc.name).
			Bool("enableKcp", appCfg.EnableKcp).
			Bool("multiCluster", appCfg.MultiCluster).
			Msg("Using standard config for connection (single cluster or KCP mode)")

		config, err = buildKubernetesConfig(appCfg.LocalDevelopment, tc.log)
		if err != nil {
			return fmt.Errorf("failed to build Kubernetes config: %w", err)
		}

		// For KCP mode, modify the config to point to the specific workspace
		if appCfg.EnableKcp {
			config, err = kcputil.ConfigForKCPCluster(tc.name, config)
			if err != nil {
				return fmt.Errorf("failed to configure KCP workspace: %w", err)
			}
		}
	}

	// Apply round tripper
	if roundTripperFactory != nil {
		config.Wrap(func(rt http.RoundTripper) http.RoundTripper {
			return roundTripperFactory(config)
		})
	}

	// Create client - use KCP-aware client only for KCP mode, standard client otherwise
	if appCfg.EnableKcp {
		tc.client, err = kcp.NewClusterAwareClientWithWatch(config, client.Options{})
	} else {
		tc.client, err = client.NewWithWatch(config, client.Options{})
	}
	if err != nil {
		return fmt.Errorf("failed to create cluster client: %w", err)
	}

	return nil
}

// buildConfigFromMetadata creates rest.Config from cluster metadata
func buildConfigFromMetadata(metadata *ClusterMetadata, log *logger.Logger) (*rest.Config, error) {
	config := &rest.Config{
		Host: metadata.Host,
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true, // Start with insecure, will be overridden if CA is provided
		},
	}

	// Handle CA data
	if metadata.CA != nil && metadata.CA.Data != "" {
		caData, err := base64.StdEncoding.DecodeString(metadata.CA.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to decode CA data: %w", err)
		}
		config.TLSClientConfig.CAData = caData
		config.TLSClientConfig.Insecure = false
		log.Debug().Msg("configured CA certificate from metadata")
	}

	// Handle authentication
	if metadata.Auth != nil {
		switch metadata.Auth.Type {
		case "token":
			if metadata.Auth.Token != "" {
				tokenData, err := base64.StdEncoding.DecodeString(metadata.Auth.Token)
				if err != nil {
					return nil, fmt.Errorf("failed to decode token: %w", err)
				}
				config.BearerToken = string(tokenData)
				log.Debug().Msg("configured bearer token authentication from metadata")
			}
		case "kubeconfig":
			if metadata.Auth.Kubeconfig != "" {
				kubeconfigData, err := base64.StdEncoding.DecodeString(metadata.Auth.Kubeconfig)
				if err != nil {
					return nil, fmt.Errorf("failed to decode kubeconfig: %w", err)
				}

				clientConfig, err := clientcmd.NewClientConfigFromBytes(kubeconfigData)
				if err != nil {
					return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
				}

				kubeconfigRestConfig, err := clientConfig.ClientConfig()
				if err != nil {
					return nil, fmt.Errorf("failed to build rest config from kubeconfig: %w", err)
				}

				// Use the auth info from kubeconfig but keep host from metadata
				config.BearerToken = kubeconfigRestConfig.BearerToken
				config.Username = kubeconfigRestConfig.Username
				config.Password = kubeconfigRestConfig.Password
				config.TLSClientConfig.CertData = kubeconfigRestConfig.TLSClientConfig.CertData
				config.TLSClientConfig.KeyData = kubeconfigRestConfig.TLSClientConfig.KeyData

				log.Debug().Msg("configured authentication from kubeconfig metadata")
			}
		case "clientCert":
			if metadata.Auth.CertData != "" && metadata.Auth.KeyData != "" {
				certData, err := base64.StdEncoding.DecodeString(metadata.Auth.CertData)
				if err != nil {
					return nil, fmt.Errorf("failed to decode cert data: %w", err)
				}
				keyData, err := base64.StdEncoding.DecodeString(metadata.Auth.KeyData)
				if err != nil {
					return nil, fmt.Errorf("failed to decode key data: %w", err)
				}
				config.TLSClientConfig.CertData = certData
				config.TLSClientConfig.KeyData = keyData
				log.Debug().Msg("configured client certificate authentication from metadata")
			}
		}
	}

	return config, nil
}

// buildKubernetesConfig creates a rest.Config using standard controller-runtime patterns
func buildKubernetesConfig(localDevelopment bool, log *logger.Logger) (*rest.Config, error) {
	if localDevelopment {
		// Use kubeconfig from environment in development mode
		if kubeconfigPath := os.Getenv("KUBECONFIG"); kubeconfigPath != "" {
			config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
			if err != nil {
				return nil, fmt.Errorf("failed to build config from KUBECONFIG: %w", err)
			}
			log.Info().Str("kubeconfig", kubeconfigPath).Msg("Using kubeconfig from environment (development mode)")
			return config, nil
		}
	}

	// Use ctrl.GetConfigOrDie() for production or development fallback
	config, err := ctrl.GetConfig()
	log.Info().Msg("Using configuration from ctrl.GetConfig()")
	return config, err
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

	// Create and store GraphQL server and handler
	tc.graphqlServer = NewGraphQLServer(tc.log, appCfg)
	tc.handler = tc.graphqlServer.CreateHandler(schemaGateway.GetSchema())

	return nil
}

// GetName returns the cluster name
func (tc *TargetCluster) GetName() string {
	return tc.name
}

// GetEndpoint returns the HTTP endpoint for this cluster's GraphQL API
func (tc *TargetCluster) GetEndpoint(appCfg appConfig.Config) string {
	path := tc.name

	if appCfg.LocalDevelopment {
		return fmt.Sprintf("http://localhost:%s/%s/graphql", appCfg.Gateway.Port, path)
	}

	return fmt.Sprintf("/%s/graphql", path)
}

// ServeHTTP handles HTTP requests for this cluster
func (tc *TargetCluster) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if tc.handler == nil || tc.handler.Handler == nil {
		http.Error(w, "Cluster not ready", http.StatusServiceUnavailable)
		return
	}

	// Handle subscription requests using Server-Sent Events
	if r.Header.Get("Accept") == "text/event-stream" {
		tc.graphqlServer.HandleSubscription(w, r, tc.handler.Schema)
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
