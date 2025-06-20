package targetcluster

import (
	"fmt"
	"net/http"
	"os"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/kcp"
)

// Connection handles the Kubernetes client connection for a target cluster
type Connection struct {
	config *rest.Config
	client client.WithWatch
}

// NewConnection creates a new connection using standard controller-runtime patterns
func NewConnection(clusterName string, roundTripperFactory func(*rest.Config) http.RoundTripper) (*Connection, error) {
	var config *rest.Config
	var err error

	// Try different config sources in order of preference
	if kubeconfigPath := os.Getenv("KUBECONFIG"); kubeconfigPath != "" {
		// Use KUBECONFIG environment variable if set
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to build config from KUBECONFIG: %w", err)
		}
	} else {
		// Fall back to ctrl.GetConfigOrDie() pattern
		config = ctrl.GetConfigOrDie() 
	}

	// Apply round tripper wrapper if provided
	if roundTripperFactory != nil {
		config.Wrap(func(rt http.RoundTripper) http.RoundTripper {
			return roundTripperFactory(config)
		})
	}

	// Create cluster-aware client using standard pattern
	clientWithWatch, err := kcp.NewClusterAwareClientWithWatch(config, client.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to create cluster-aware client: %w", err)
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
