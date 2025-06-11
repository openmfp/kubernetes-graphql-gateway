package targetcluster

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"

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
// Supports only kubeconfig-based authentication for standard Kubernetes clusters
func buildConfigFromMetadata(metadata *ClusterMetadata) (*rest.Config, error) {
	if metadata.Auth == nil || metadata.Auth.Type != "kubeconfig" {
		return nil, fmt.Errorf("only kubeconfig-based authentication is supported for standard clusters")
	}

	if metadata.Auth.Kubeconfig == "" {
		return nil, fmt.Errorf("kubeconfig data is empty")
	}

	// Decode base64 kubeconfig
	kubeconfigData, err := base64.StdEncoding.DecodeString(metadata.Auth.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to decode kubeconfig data: %w", err)
	}

	// Create temporary file for kubeconfig
	tmpFile, err := os.CreateTemp("", "kubeconfig-*.yaml")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary kubeconfig file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(kubeconfigData); err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("failed to write kubeconfig to temporary file: %w", err)
	}
	tmpFile.Close()

	// Build config from kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", tmpFile.Name())
	if err != nil {
		return nil, fmt.Errorf("failed to build config from kubeconfig: %w", err)
	}

	return config, nil
}
