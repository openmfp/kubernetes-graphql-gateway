package targetcluster

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
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
func buildConfigFromMetadata(metadata *ClusterMetadata) (*rest.Config, error) {
	// Handle kubeconfig-based authentication first (most common case)
	if metadata.Auth != nil && metadata.Auth.Type == "kubeconfig" {
		return buildConfigFromKubeconfig(metadata)
	}

	// Fallback to manual config building for other auth types
	return buildConfigManually(metadata)
}

// buildConfigFromKubeconfig creates config directly from kubeconfig (preferred approach)
func buildConfigFromKubeconfig(metadata *ClusterMetadata) (*rest.Config, error) {
	if metadata.Auth.Kubeconfig == "" {
		return nil, fmt.Errorf("kubeconfig data is empty")
	}

	kubeconfigData, err := base64.StdEncoding.DecodeString(metadata.Auth.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to decode kubeconfig data: %w", err)
	}

	// Create temporary file and use clientcmd to build config - same as kubectl
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

	// Use clientcmd to build config - this handles all TLS settings correctly
	config, err := clientcmd.BuildConfigFromFlags("", tmpFile.Name())
	if err != nil {
		return nil, fmt.Errorf("failed to build config from kubeconfig: %w", err)
	}

	// Ensure URL points directly to the domain (preserve KCP logic)
	u, err := url.Parse(config.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to parse host URL: %w", err)
	}
	config.Host = fmt.Sprintf("%s://%s", u.Scheme, u.Host)

	return config, nil
}

// buildConfigManually creates config manually for non-kubeconfig auth types
func buildConfigManually(metadata *ClusterMetadata) (*rest.Config, error) {
	// Ensure URL points directly to the domain
	u, err := url.Parse(metadata.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to parse host URL: %w", err)
	}
	normalizedHost := fmt.Sprintf("%s://%s", u.Scheme, u.Host)

	config := &rest.Config{
		Host: normalizedHost,
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true, // Start with insecure, will be overridden if CA is provided
		},
	}

	// Handle authentication
	if metadata.Auth != nil {
		if err := configureAuthentication(config, metadata); err != nil {
			return nil, fmt.Errorf("failed to configure authentication: %w", err)
		}
	}

	// Handle CA configuration
	if metadata.CA != nil {
		caData, err := base64.StdEncoding.DecodeString(metadata.CA.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to decode CA data: %w", err)
		}

		config.TLSClientConfig.CAData = caData
		config.TLSClientConfig.Insecure = false
	}

	return config, nil
}

// configureAuthentication configures authentication from metadata for manual config
func configureAuthentication(config *rest.Config, metadata *ClusterMetadata) error {
	switch metadata.Auth.Type {
	case "token":
		if metadata.Auth.Token == "" {
			return fmt.Errorf("token data is empty")
		}

		tokenData, err := base64.StdEncoding.DecodeString(metadata.Auth.Token)
		if err != nil {
			return fmt.Errorf("failed to decode token data: %w", err)
		}

		config.BearerToken = string(tokenData)

	case "clientCert":
		if metadata.Auth.CertData == "" || metadata.Auth.KeyData == "" {
			return fmt.Errorf("client certificate data is incomplete")
		}

		certData, err := base64.StdEncoding.DecodeString(metadata.Auth.CertData)
		if err != nil {
			return fmt.Errorf("failed to decode certificate data: %w", err)
		}

		keyData, err := base64.StdEncoding.DecodeString(metadata.Auth.KeyData)
		if err != nil {
			return fmt.Errorf("failed to decode key data: %w", err)
		}

		config.TLSClientConfig.CertData = certData
		config.TLSClientConfig.KeyData = keyData

	default:
		return fmt.Errorf("unsupported authentication type: %s", metadata.Auth.Type)
	}

	return nil
}
