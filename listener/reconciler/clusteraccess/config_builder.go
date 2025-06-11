package clusteraccess

import (
	"errors"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gatewayv1alpha1 "github.com/openmfp/kubernetes-graphql-gateway/common/apis/gateway/v1alpha1"
)

// buildTargetClusterConfigFromTyped extracts connection info from ClusterAccess and builds rest.Config
func buildTargetClusterConfigFromTyped(clusterAccess gatewayv1alpha1.ClusterAccess, k8sClient client.Client) (*rest.Config, string, error) {
	spec := clusterAccess.Spec

	// Extract host (required)
	host := spec.Host
	if host == "" {
		return nil, "", errors.New("host field not found in ClusterAccess spec")
	}

	// Extract cluster name (path field or resource name)
	clusterName := clusterAccess.GetName()
	if spec.Path != "" {
		clusterName = spec.Path
	}

	config := &rest.Config{
		Host: host,
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true, // Start with insecure, will be overridden if CA is provided
		},
	}

	// Handle CA configuration first
	if spec.CA != nil {
		caData, err := extractCAData(spec.CA, k8sClient)
		if err != nil {
			return nil, "", errors.Join(errors.New("failed to extract CA data"), err)
		}
		if caData != nil {
			config.TLSClientConfig.CAData = caData
			config.TLSClientConfig.Insecure = false // Use proper TLS verification when CA is provided
		}
	}

	// Handle Auth configuration
	if spec.Auth != nil {
		err := configureAuthentication(config, spec.Auth, k8sClient)
		if err != nil {
			return nil, "", errors.Join(errors.New("failed to configure authentication"), err)
		}
	}

	return config, clusterName, nil
}
