package targetcluster

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

	// Add custom round tripper if provided
	if roundTripperFactory != nil {
		config.Wrap(func(rt http.RoundTripper) http.RoundTripper {
			return roundTripperFactory(config)
		})
	}

	// Create a scheme with standard Kubernetes types to avoid discovery calls
	scheme := runtime.NewScheme()

	// Add core and apps API groups
	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add core/v1 to scheme: %w", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add apps/v1 to scheme: %w", err)
	}
	if err := metav1.AddMetaToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add meta/v1 to scheme: %w", err)
	}

	// Create a static REST mapper to avoid discovery calls
	restMapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{
		corev1.SchemeGroupVersion,
		appsv1.SchemeGroupVersion,
	})

	// Add core resources
	restMapper.Add(corev1.SchemeGroupVersion.WithKind("Pod"), meta.RESTScopeNamespace)
	restMapper.Add(corev1.SchemeGroupVersion.WithKind("Service"), meta.RESTScopeNamespace)
	restMapper.Add(corev1.SchemeGroupVersion.WithKind("ConfigMap"), meta.RESTScopeNamespace)
	restMapper.Add(corev1.SchemeGroupVersion.WithKind("Secret"), meta.RESTScopeNamespace)
	restMapper.Add(corev1.SchemeGroupVersion.WithKind("Namespace"), meta.RESTScopeRoot)

	// Add apps resources
	restMapper.Add(appsv1.SchemeGroupVersion.WithKind("Deployment"), meta.RESTScopeNamespace)
	restMapper.Add(appsv1.SchemeGroupVersion.WithKind("ReplicaSet"), meta.RESTScopeNamespace)
	restMapper.Add(appsv1.SchemeGroupVersion.WithKind("DaemonSet"), meta.RESTScopeNamespace)
	restMapper.Add(appsv1.SchemeGroupVersion.WithKind("StatefulSet"), meta.RESTScopeNamespace)

	clientWithWatch, err := client.NewWithWatch(config, client.Options{
		Scheme: scheme,
		Mapper: restMapper,
	})
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
	// TODO: Implement health check by making a simple API call
	return c.client != nil
}

// buildConfigFromMetadata creates a rest.Config from cluster metadata
func buildConfigFromMetadata(metadata *ClusterMetadata) (*rest.Config, error) {
	// Ensure URL points directly to the domain (preserve KCP logic)
	u, err := url.Parse(metadata.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to parse host URL: %w", err)
	}
	normalizedHost := fmt.Sprintf("%s://%s", u.Scheme, u.Host)

	config := &rest.Config{
		Host: normalizedHost,
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true,
		},
	}

	// Handle CA configuration
	if metadata.CA != nil {
		caData, err := base64.StdEncoding.DecodeString(metadata.CA.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to decode CA data: %w", err)
		}

		// For development clusters (like kind), keep TLS verification disabled
		// Store CA data for reference but don't enforce verification
		config.TLSClientConfig.CAData = caData
		// Keep Insecure = true to bypass certificate verification issues
	}

	// Set a custom transport that disables TLS verification globally
	config.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	// Handle authentication configuration
	if metadata.Auth != nil {
		if err := configureAuthentication(config, metadata, u); err != nil {
			return nil, fmt.Errorf("failed to configure authentication: %w", err)
		}
	}

	return config, nil
}

// configureAuthentication configures authentication from metadata
func configureAuthentication(config *rest.Config, metadata *ClusterMetadata, u *url.URL) error {
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

	case "kubeconfig":
		if metadata.Auth.Kubeconfig == "" {
			return fmt.Errorf("kubeconfig data is empty")
		}

		kubeconfigData, err := base64.StdEncoding.DecodeString(metadata.Auth.Kubeconfig)
		if err != nil {
			return fmt.Errorf("failed to decode kubeconfig data: %w", err)
		}

		// Use the same approach as the old working implementation
		// Create temporary file and use clientcmd to build complete config
		tmpFile, err := os.CreateTemp("", "kubeconfig-*.yaml")
		if err != nil {
			return fmt.Errorf("failed to create temporary kubeconfig file: %w", err)
		}
		defer os.Remove(tmpFile.Name())

		if _, err := tmpFile.Write(kubeconfigData); err != nil {
			tmpFile.Close()
			return fmt.Errorf("failed to write kubeconfig data: %w", err)
		}
		tmpFile.Close()

		kubeconfigConfig, err := clientcmd.LoadFromFile(tmpFile.Name())
		if err != nil {
			return fmt.Errorf("failed to load kubeconfig: %w", err)
		}

		restConfig, err := clientcmd.NewDefaultClientConfig(*kubeconfigConfig, &clientcmd.ConfigOverrides{}).ClientConfig()
		if err != nil {
			return fmt.Errorf("failed to create rest config from kubeconfig: %w", err)
		}

		// Use the kubeconfig as the base and override only what's needed from metadata
		// This approach matches the old working implementation more closely
		*config = *restConfig

		// Override host if metadata specifies a different one
		normalizedHost := fmt.Sprintf("%s://%s", u.Scheme, u.Host)
		if config.Host != normalizedHost {
			config.Host = normalizedHost
		}

		// Override CA data if metadata specifies it (preserve metadata CA over kubeconfig CA)
		if metadata.CA != nil {
			caData, err := base64.StdEncoding.DecodeString(metadata.CA.Data)
			if err != nil {
				return fmt.Errorf("failed to decode CA data from metadata: %w", err)
			}
			config.TLSClientConfig.CAData = caData
			config.TLSClientConfig.Insecure = false
		}

	default:
		return fmt.Errorf("unsupported authentication type: %s", metadata.Auth.Type)
	}

	return nil
}
