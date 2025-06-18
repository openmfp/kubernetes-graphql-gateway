package targetcluster

import (
	"encoding/base64"
	"fmt"
	"net/http"

	"github.com/go-errors/errors"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/kcp"
)

// Connection handles the Kubernetes client connection for a target cluster
type Connection struct {
	config *rest.Config
	client client.WithWatch
}

// NewConnection creates a new connection from cluster metadata
func NewConnection(metadata *ClusterMetadata, roundTripperFactory func(*rest.Config) http.RoundTripper) (*Connection, error) {
	config, err := buildConfigFromMetadata(metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to build config: %w", err)
	}

	// Use the same pattern as main branch - wrap the round tripper at config level
	if roundTripperFactory != nil {
		config.Wrap(func(rt http.RoundTripper) http.RoundTripper {
			return roundTripperFactory(config)
		})
	}

	// Use the same client creation approach as main branch
	clientWithWatch, err := kcp.NewClusterAwareClientWithWatch(config, client.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	return &Connection{
		config: config,
		client: clientWithWatch,
	}, nil
}

// GetClient returns the Kubernetes client
func (c *Connection) GetClient() client.WithWatch {
	return c.client
}

// GetConfig returns the rest configuration
func (c *Connection) GetConfig() *rest.Config {
	return c.config
}

// IsHealthy checks if the connection is healthy
func (c *Connection) IsHealthy() bool {
	return c.client != nil
}

// buildConfigFromMetadata creates a rest.Config from cluster metadata
// Supports kubeconfig-based, in-cluster, and token-based authentication
func buildConfigFromMetadata(metadata *ClusterMetadata) (*rest.Config, error) {
	if metadata.Auth == nil {
		return nil, errors.New("authentication configuration is required")
	}

	switch metadata.Auth.Type {
	case "kubeconfig":
		if metadata.Auth.Kubeconfig == "" {
			return nil, errors.New("kubeconfig data is empty")
		}

		// Decode base64 kubeconfig
		kubeconfigData, err := base64.StdEncoding.DecodeString(metadata.Auth.Kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to decode kubeconfig data: %w", err)
		}

		// Create client config directly from bytes
		clientConfig, err := clientcmd.NewClientConfigFromBytes(kubeconfigData)
		if err != nil {
			return nil, fmt.Errorf("failed to create client config: %w", err)
		}

		// Get rest config
		config, err := clientConfig.ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to get rest config: %w", err)
		}

		return config, nil

	case "in-cluster":
		config, err := rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
		}
		return config, nil

	case "token":
		if metadata.Auth.Token == "" {
			return nil, fmt.Errorf("token is empty")
		}
		if metadata.Host == "" {
			return nil, fmt.Errorf("host is required for token authentication")
		}

		// Decode base64 token (tokens are stored base64-encoded in metadata)
		tokenData, err := base64.StdEncoding.DecodeString(metadata.Auth.Token)
		if err != nil {
			return nil, fmt.Errorf("failed to decode token data: %w", err)
		}

		config := &rest.Config{
			Host:        metadata.Host,
			BearerToken: string(tokenData),
			TLSClientConfig: rest.TLSClientConfig{
				Insecure: true, // Default to insecure, will be overridden if CA is provided
			},
		}

		// Use CA certificate if provided
		if metadata.CA != nil && metadata.CA.Data != "" {
			caData, err := base64.StdEncoding.DecodeString(metadata.CA.Data)
			if err != nil {
				return nil, fmt.Errorf("failed to decode CA data: %w", err)
			}
			config.TLSClientConfig.CAData = caData
			config.TLSClientConfig.Insecure = false // Use proper TLS verification when CA is provided
		}

		return config, nil

	default:
		return nil, fmt.Errorf("unsupported authentication type: %s", metadata.Auth.Type)
	}
}
