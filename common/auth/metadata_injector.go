package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openmfp/golang-commons/logger"
	gatewayv1alpha1 "github.com/openmfp/kubernetes-graphql-gateway/common/apis/v1alpha1"
)

// MetadataInjectionConfig contains configuration for metadata injection
type MetadataInjectionConfig struct {
	Host         string
	Path         string
	Auth         *gatewayv1alpha1.AuthConfig
	CA           *gatewayv1alpha1.CAConfig
	HostOverride string // For virtual workspaces
}

// InjectClusterMetadata injects cluster metadata into schema JSON
// This unified function handles both KCP and ClusterAccess use cases
func InjectClusterMetadata(schemaJSON []byte, config MetadataInjectionConfig, k8sClient client.Client, log *logger.Logger) ([]byte, error) {
	// Parse the existing schema JSON
	var schemaData map[string]interface{}
	if err := json.Unmarshal(schemaJSON, &schemaData); err != nil {
		return nil, fmt.Errorf("failed to parse schema JSON: %w", err)
	}

	// Determine the host to use
	host := config.Host
	if config.HostOverride != "" {
		host = config.HostOverride
		log.Info().
			Str("originalHost", config.Host).
			Str("overrideHost", host).
			Msg("using host override for virtual workspace")
	} else {
		// For normal workspaces, ensure we use a clean host by stripping any virtual workspace paths
		cleanedHost := stripVirtualWorkspacePath(config.Host)
		if cleanedHost != config.Host {
			host = cleanedHost
			log.Info().
				Str("originalHost", config.Host).
				Str("cleanedHost", host).
				Msg("cleaned virtual workspace path from host for normal workspace")
		}
	}

	// Create cluster metadata
	metadata := map[string]interface{}{
		"host": host,
		"path": config.Path,
	}

	// Extract auth data and potentially CA data
	var kubeconfigCAData []byte
	if config.Auth != nil {
		authMetadata, err := extractAuthDataForMetadata(config.Auth, k8sClient)
		if err != nil {
			log.Warn().Err(err).Msg("failed to extract auth data for metadata")
		} else if authMetadata != nil {
			metadata["auth"] = authMetadata

			// If auth type is kubeconfig, extract CA data from kubeconfig
			if authType, ok := authMetadata["type"].(string); ok && authType == "kubeconfig" {
				if kubeconfigB64, ok := authMetadata["kubeconfig"].(string); ok {
					kubeconfigCAData = extractCAFromKubeconfigB64(kubeconfigB64, log)
				}
			}
		}
	}

	// Add CA data - prefer explicit CA config, fallback to kubeconfig CA
	if config.CA != nil {
		caData, err := ExtractCAData(config.CA, k8sClient)
		if err != nil {
			log.Warn().Err(err).Msg("failed to extract CA data for metadata")
		} else if caData != nil {
			metadata["ca"] = map[string]interface{}{
				"data": base64.StdEncoding.EncodeToString(caData),
			}
		}
	} else if kubeconfigCAData != nil {
		// Use CA data extracted from kubeconfig
		metadata["ca"] = map[string]interface{}{
			"data": base64.StdEncoding.EncodeToString(kubeconfigCAData),
		}
		log.Info().Msg("extracted CA data from kubeconfig")
	}

	// Inject the metadata into the schema
	schemaData["x-cluster-metadata"] = metadata

	// Marshal back to JSON
	modifiedJSON, err := json.Marshal(schemaData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal modified schema: %w", err)
	}

	log.Info().
		Str("host", host).
		Str("path", config.Path).
		Bool("hasCA", kubeconfigCAData != nil || config.CA != nil).
		Msg("successfully injected cluster metadata into schema")

	return modifiedJSON, nil
}

// InjectKCPMetadataFromEnv injects KCP metadata using kubeconfig from environment
// This is a convenience function for KCP use cases
func InjectKCPMetadataFromEnv(schemaJSON []byte, clusterPath string, log *logger.Logger, hostOverride ...string) ([]byte, error) {
	// Get kubeconfig from environment (same sources as ctrl.GetConfig())
	kubeconfigData, kubeconfigHost, err := extractKubeconfigFromEnv(log)
	if err != nil {
		return nil, fmt.Errorf("failed to extract kubeconfig data: %w", err)
	}

	// Determine host override
	var override string
	if len(hostOverride) > 0 && hostOverride[0] != "" {
		override = hostOverride[0]
	}

	// Parse the existing schema JSON
	var schemaData map[string]interface{}
	if err := json.Unmarshal(schemaJSON, &schemaData); err != nil {
		return nil, fmt.Errorf("failed to parse schema JSON: %w", err)
	}

	// Determine which host to use
	var host string
	if override != "" {
		host = override
		log.Info().
			Str("clusterPath", clusterPath).
			Str("originalHost", kubeconfigHost).
			Str("overrideHost", host).
			Msg("using host override for virtual workspace")
	} else {
		// For normal workspaces, ensure we use a clean KCP host by stripping any virtual workspace paths
		host = stripVirtualWorkspacePath(kubeconfigHost)
		if host != kubeconfigHost {
			log.Info().
				Str("clusterPath", clusterPath).
				Str("originalHost", kubeconfigHost).
				Str("cleanedHost", host).
				Msg("cleaned virtual workspace path from kubeconfig host for normal workspace")
		}
	}

	// Create cluster metadata with environment kubeconfig
	metadata := map[string]interface{}{
		"host": host,
		"path": clusterPath,
		"auth": map[string]interface{}{
			"type":       "kubeconfig",
			"kubeconfig": base64.StdEncoding.EncodeToString(kubeconfigData),
		},
	}

	// Extract CA data from kubeconfig if available
	caData := extractCAFromKubeconfigData(kubeconfigData, log)
	if caData != nil {
		metadata["ca"] = map[string]interface{}{
			"data": base64.StdEncoding.EncodeToString(caData),
		}
	}

	// Inject the metadata into the schema
	schemaData["x-cluster-metadata"] = metadata

	// Marshal back to JSON
	modifiedJSON, err := json.Marshal(schemaData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal modified schema: %w", err)
	}

	log.Info().
		Str("clusterPath", clusterPath).
		Str("host", host).
		Bool("hasCA", caData != nil).
		Msg("successfully injected KCP cluster metadata into schema")

	return modifiedJSON, nil
}

// extractAuthDataForMetadata extracts auth data from AuthConfig for metadata injection
func extractAuthDataForMetadata(auth *gatewayv1alpha1.AuthConfig, k8sClient client.Client) (map[string]interface{}, error) {
	if auth == nil {
		return nil, nil
	}

	ctx := context.Background()

	if auth.SecretRef != nil {
		secret := &corev1.Secret{}
		namespace := auth.SecretRef.Namespace
		if namespace == "" {
			namespace = "default"
		}

		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      auth.SecretRef.Name,
			Namespace: namespace,
		}, secret)
		if err != nil {
			return nil, fmt.Errorf("failed to get auth secret: %w", err)
		}

		tokenData, ok := secret.Data[auth.SecretRef.Key]
		if !ok {
			return nil, fmt.Errorf("auth key not found in secret")
		}

		return map[string]interface{}{
			"type":  "token",
			"token": base64.StdEncoding.EncodeToString(tokenData),
		}, nil
	}

	if auth.KubeconfigSecretRef != nil {
		secret := &corev1.Secret{}
		namespace := auth.KubeconfigSecretRef.Namespace
		if namespace == "" {
			namespace = "default"
		}

		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      auth.KubeconfigSecretRef.Name,
			Namespace: namespace,
		}, secret)
		if err != nil {
			return nil, fmt.Errorf("failed to get kubeconfig secret: %w", err)
		}

		kubeconfigData, ok := secret.Data["kubeconfig"]
		if !ok {
			return nil, fmt.Errorf("kubeconfig key not found in secret")
		}

		return map[string]interface{}{
			"type":       "kubeconfig",
			"kubeconfig": base64.StdEncoding.EncodeToString(kubeconfigData),
		}, nil
	}

	if auth.ClientCertificateRef != nil {
		secret := &corev1.Secret{}
		namespace := auth.ClientCertificateRef.Namespace
		if namespace == "" {
			namespace = "default"
		}

		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      auth.ClientCertificateRef.Name,
			Namespace: namespace,
		}, secret)
		if err != nil {
			return nil, fmt.Errorf("failed to get client certificate secret: %w", err)
		}

		certData, certOk := secret.Data["tls.crt"]
		keyData, keyOk := secret.Data["tls.key"]

		if !certOk || !keyOk {
			return nil, fmt.Errorf("client certificate or key not found in secret")
		}

		return map[string]interface{}{
			"type":     "clientCert",
			"certData": base64.StdEncoding.EncodeToString(certData),
			"keyData":  base64.StdEncoding.EncodeToString(keyData),
		}, nil
	}

	return nil, nil // No auth configured
}

// extractKubeconfigFromEnv gets kubeconfig data from the same sources as ctrl.GetConfig()
func extractKubeconfigFromEnv(log *logger.Logger) ([]byte, string, error) {
	var kubeconfigPath string

	// Check KUBECONFIG environment variable first
	if kubeconfig := os.Getenv("KUBECONFIG"); kubeconfig != "" {
		kubeconfigPath = kubeconfig
		log.Debug().Str("source", "KUBECONFIG env var").Str("path", kubeconfigPath).Msg("using kubeconfig from environment variable")
	} else {
		// Fall back to default kubeconfig location
		if home, err := os.UserHomeDir(); err == nil {
			kubeconfigPath = home + "/.kube/config"
			log.Debug().Str("source", "default location").Str("path", kubeconfigPath).Msg("using default kubeconfig location")
		} else {
			return nil, "", fmt.Errorf("failed to determine kubeconfig location: %w", err)
		}
	}

	// Check if file exists
	if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
		return nil, "", fmt.Errorf("kubeconfig file not found: %s", kubeconfigPath)
	}

	// Read kubeconfig file
	kubeconfigData, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read kubeconfig file %s: %w", kubeconfigPath, err)
	}

	// Parse kubeconfig to extract server URL
	config, err := clientcmd.Load(kubeconfigData)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	// Get current context and cluster server URL
	host, err := extractServerURL(config)
	if err != nil {
		return nil, "", fmt.Errorf("failed to extract server URL from kubeconfig: %w", err)
	}

	return kubeconfigData, host, nil
}

// extractServerURL extracts the server URL from kubeconfig
func extractServerURL(config *api.Config) (string, error) {
	if config.CurrentContext == "" {
		return "", fmt.Errorf("no current context in kubeconfig")
	}

	context, exists := config.Contexts[config.CurrentContext]
	if !exists {
		return "", fmt.Errorf("current context %s not found in kubeconfig", config.CurrentContext)
	}

	cluster, exists := config.Clusters[context.Cluster]
	if !exists {
		return "", fmt.Errorf("cluster %s not found in kubeconfig", context.Cluster)
	}

	if cluster.Server == "" {
		return "", fmt.Errorf("no server URL found in cluster configuration")
	}

	return cluster.Server, nil
}

// stripVirtualWorkspacePath removes virtual workspace paths from a URL to get the base KCP host
func stripVirtualWorkspacePath(hostURL string) string {
	parsedURL, err := url.Parse(hostURL)
	if err != nil {
		// If we can't parse the URL, return it as-is
		return hostURL
	}

	// Check if the path contains a virtual workspace pattern: /services/apiexport/...
	if strings.HasPrefix(parsedURL.Path, "/services/apiexport/") {
		// Strip the virtual workspace path to get the base KCP host
		parsedURL.Path = ""
		return parsedURL.String()
	}

	// If it's not a virtual workspace URL, return as-is
	return hostURL
}

// extractCAFromKubeconfigData extracts CA certificate data from raw kubeconfig bytes
func extractCAFromKubeconfigData(kubeconfigData []byte, log *logger.Logger) []byte {
	config, err := clientcmd.Load(kubeconfigData)
	if err != nil {
		log.Warn().Err(err).Msg("failed to parse kubeconfig for CA extraction")
		return nil
	}

	if config.CurrentContext == "" {
		log.Warn().Msg("no current context in kubeconfig for CA extraction")
		return nil
	}

	context, exists := config.Contexts[config.CurrentContext]
	if !exists {
		log.Warn().Str("context", config.CurrentContext).Msg("current context not found in kubeconfig for CA extraction")
		return nil
	}

	cluster, exists := config.Clusters[context.Cluster]
	if !exists {
		log.Warn().Str("cluster", context.Cluster).Msg("cluster not found in kubeconfig for CA extraction")
		return nil
	}

	if len(cluster.CertificateAuthorityData) > 0 {
		return cluster.CertificateAuthorityData
	}

	log.Debug().Msg("no CA data found in kubeconfig")
	return nil
}

// extractCAFromKubeconfigB64 extracts CA certificate data from base64-encoded kubeconfig
func extractCAFromKubeconfigB64(kubeconfigB64 string, log *logger.Logger) []byte {
	kubeconfigData, err := base64.StdEncoding.DecodeString(kubeconfigB64)
	if err != nil {
		log.Warn().Err(err).Msg("failed to decode kubeconfig for CA extraction")
		return nil
	}

	return extractCAFromKubeconfigData(kubeconfigData, log)
}
