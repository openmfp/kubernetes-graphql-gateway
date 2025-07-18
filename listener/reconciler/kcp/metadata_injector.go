package kcp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"

	"github.com/openmfp/golang-commons/logger"
)

// injectKCPClusterMetadata injects kubeconfig metadata from environment into the schema JSON
// If hostOverride is provided, it will be used instead of the host from kubeconfig
func injectKCPClusterMetadata(schemaJSON []byte, clusterPath string, log *logger.Logger, hostOverride ...string) ([]byte, error) {
	// Parse the existing schema JSON
	var schemaData map[string]interface{}
	if err := json.Unmarshal(schemaJSON, &schemaData); err != nil {
		return nil, fmt.Errorf("failed to parse schema JSON: %w", err)
	}

	// Get kubeconfig from the same sources that ctrl.GetConfig() uses
	kubeconfigData, kubeconfigHost, err := extractKubeconfigData(log)
	if err != nil {
		return nil, fmt.Errorf("failed to extract kubeconfig data: %w", err)
	}

	// Determine which host to use
	var host string
	if len(hostOverride) > 0 && hostOverride[0] != "" {
		host = hostOverride[0]
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

	// Create cluster metadata
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

// extractKubeconfigData gets kubeconfig data from the same sources as ctrl.GetConfig()
func extractKubeconfigData(log *logger.Logger) ([]byte, string, error) {
	// Use the same precedence as ctrl.GetConfig():
	// 1. --kubeconfig flag (not applicable in listener context)
	// 2. KUBECONFIG environment variable
	// 3. In-cluster config (not applicable for file extraction)
	// 4. $HOME/.kube/config

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

// stripVirtualWorkspacePath removes virtual workspace paths from a URL to get the base KCP host
// This ensures normal workspaces get clean URLs like /clusters/{workspace} instead of /services/apiexport/.../clusters/{workspace}
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
