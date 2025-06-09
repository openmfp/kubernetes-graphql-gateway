package clusteraccess

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/go-openapi/spec"
	"github.com/openmfp/golang-commons/logger"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appConfig "github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"github.com/openmfp/kubernetes-graphql-gateway/gateway/resolver"
)

// ClusterMetadata represents cluster connection information embedded in schema files
type ClusterMetadata struct {
	Host string        `json:"host"`
	Path string        `json:"path"`
	Auth *AuthMetadata `json:"auth,omitempty"`
	CA   *CAMetadata   `json:"ca,omitempty"`
}

type AuthMetadata struct {
	Type       string `json:"type"`
	Token      string `json:"token,omitempty"`      // base64 encoded
	CertData   string `json:"certData,omitempty"`   // base64 encoded
	KeyData    string `json:"keyData,omitempty"`    // base64 encoded
	Kubeconfig string `json:"kubeconfig,omitempty"` // base64 encoded
}

type CAMetadata struct {
	Data string `json:"data"` // base64 encoded
}

type FileData struct {
	Definitions spec.Definitions
	Metadata    *ClusterMetadata
}

// ClusterClient handles cluster access operations
type ClusterClient struct {
	appCfg          appConfig.Config
	log             *logger.Logger
	resolvers       map[string]resolver.Provider
	newRoundTripper func(*logger.Logger, http.RoundTripper, string, bool) http.RoundTripper
}

// NewClusterClient creates a new cluster access client
func NewClusterClient(appCfg appConfig.Config, log *logger.Logger, resolvers map[string]resolver.Provider, newRoundTripper func(*logger.Logger, http.RoundTripper, string, bool) http.RoundTripper) *ClusterClient {
	return &ClusterClient{
		appCfg:          appCfg,
		log:             log,
		resolvers:       resolvers,
		newRoundTripper: newRoundTripper,
	}
}

// ReadSchemaFile reads a schema file once and extracts both OpenAPI definitions and cluster metadata
func (c *ClusterClient) ReadSchemaFile(filename string) (*FileData, error) {
	filePath := filepath.Join(c.appCfg.OpenApiDefinitionsPath, filename)

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", filename, err)
	}
	defer file.Close()

	var schemaData map[string]interface{}
	if err := json.NewDecoder(file).Decode(&schemaData); err != nil {
		return nil, fmt.Errorf("failed to decode JSON from file %s: %w", filename, err)
	}

	// Extract OpenAPI definitions
	var definitions spec.Definitions
	if defsRaw, exists := schemaData["definitions"]; exists {
		defsBytes, err := json.Marshal(defsRaw)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal definitions: %w", err)
		}
		if err := json.Unmarshal(defsBytes, &definitions); err != nil {
			return nil, fmt.Errorf("failed to unmarshal definitions: %w", err)
		}
	}

	var metadata ClusterMetadata
	if metadataRaw, exists := schemaData["x-cluster-metadata"]; exists {
		metadataBytes, err := json.Marshal(metadataRaw)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal cluster metadata: %w", err)
		}

		if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal cluster metadata: %w", err)
		}
	}

	return &FileData{
		Definitions: definitions,
		Metadata:    &metadata,
	}, nil
}

// CreateResolverFromMetadata creates a resolver based on cluster metadata
func (c *ClusterClient) CreateResolverFromMetadata(filename string, metadata *ClusterMetadata) error {
	c.log.Info().Str("filename", filename).Str("host", metadata.Host).Str("path", metadata.Path).Msg("Creating resolver from file metadata")

	targetConfig, err := c.buildTargetClusterConfigFromMetadata(metadata)
	if err != nil {
		return fmt.Errorf("failed to build target cluster config: %w", err)
	}

	targetClient, err := client.NewWithWatch(targetConfig, client.Options{})
	if err != nil {
		return fmt.Errorf("failed to create runtime client for target cluster: %w", err)
	}

	targetResolver := resolver.New(c.log, targetClient)
	c.resolvers[filename] = targetResolver

	c.log.Info().Str("filename", filename).Str("clusterHost", metadata.Host).Msg("Successfully created resolver from file metadata")
	return nil
}

// buildTargetClusterConfigFromMetadata creates a rest.Config from cluster metadata
func (c *ClusterClient) buildTargetClusterConfigFromMetadata(metadata *ClusterMetadata) (*rest.Config, error) {
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
		config.TLSClientConfig.CAData = caData
		config.TLSClientConfig.Insecure = false
	}

	// Handle authentication configuration
	if metadata.Auth != nil {
		if err := configureAuthenticationFromMetadata(config, metadata.Auth); err != nil {
			return nil, fmt.Errorf("failed to configure authentication: %w", err)
		}
	}

	// Add custom round tripper for impersonation and token handling
	config.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return c.newRoundTripper(c.log, rt, c.appCfg.Gateway.UsernameClaim, c.appCfg.Gateway.ShouldImpersonate)
	})

	return config, nil
}

// configureAuthenticationFromMetadata configures authentication from metadata
func configureAuthenticationFromMetadata(config *rest.Config, auth *AuthMetadata) error {
	switch auth.Type {
	case "token":
		if auth.Token == "" {
			return fmt.Errorf("token data is empty")
		}

		tokenData, err := base64.StdEncoding.DecodeString(auth.Token)
		if err != nil {
			return fmt.Errorf("failed to decode token data: %w", err)
		}

		config.BearerToken = string(tokenData)

	case "clientCert":
		if auth.CertData == "" || auth.KeyData == "" {
			return fmt.Errorf("client certificate data is incomplete")
		}

		certData, err := base64.StdEncoding.DecodeString(auth.CertData)
		if err != nil {
			return fmt.Errorf("failed to decode certificate data: %w", err)
		}

		keyData, err := base64.StdEncoding.DecodeString(auth.KeyData)
		if err != nil {
			return fmt.Errorf("failed to decode key data: %w", err)
		}

		config.TLSClientConfig.CertData = certData
		config.TLSClientConfig.KeyData = keyData

	case "kubeconfig":
		if auth.Kubeconfig == "" {
			return fmt.Errorf("kubeconfig data is empty")
		}

		kubeconfigData, err := base64.StdEncoding.DecodeString(auth.Kubeconfig)
		if err != nil {
			return fmt.Errorf("failed to decode kubeconfig data: %w", err)
		}

		// Parse kubeconfig and extract auth info
		clientConfig, err := clientcmd.NewClientConfigFromBytes(kubeconfigData)
		if err != nil {
			return fmt.Errorf("failed to parse kubeconfig: %w", err)
		}

		rawConfig, err := clientConfig.RawConfig()
		if err != nil {
			return fmt.Errorf("failed to get raw kubeconfig: %w", err)
		}

		// Get the current context
		currentContext := rawConfig.CurrentContext
		if currentContext == "" {
			return fmt.Errorf("no current context in kubeconfig")
		}

		context, exists := rawConfig.Contexts[currentContext]
		if !exists {
			return fmt.Errorf("current context '%s' not found in kubeconfig", currentContext)
		}

		// Get auth info for current context
		authInfo, exists := rawConfig.AuthInfos[context.AuthInfo]
		if !exists {
			return fmt.Errorf("auth info '%s' not found in kubeconfig", context.AuthInfo)
		}

		// Extract authentication information
		if err := extractAuthFromKubeconfig(config, authInfo); err != nil {
			return fmt.Errorf("failed to extract auth from kubeconfig: %w", err)
		}

	default:
		return fmt.Errorf("unsupported authentication type: %s", auth.Type)
	}

	return nil
}

// extractAuthFromKubeconfig extracts authentication info from kubeconfig AuthInfo
func extractAuthFromKubeconfig(config *rest.Config, authInfo *api.AuthInfo) error {
	// Handle client certificate auth
	if len(authInfo.ClientCertificateData) > 0 && len(authInfo.ClientKeyData) > 0 {
		config.TLSClientConfig.CertData = authInfo.ClientCertificateData
		config.TLSClientConfig.KeyData = authInfo.ClientKeyData
		return nil
	}

	// Handle bearer token auth
	if authInfo.Token != "" {
		config.BearerToken = authInfo.Token
		return nil
	}

	// Handle token file auth
	if authInfo.TokenFile != "" {
		tokenData, err := os.ReadFile(authInfo.TokenFile)
		if err != nil {
			return fmt.Errorf("failed to read token file '%s': %w", authInfo.TokenFile, err)
		}
		config.BearerToken = string(tokenData)
		return nil
	}

	// Handle client certificate files
	if authInfo.ClientCertificate != "" && authInfo.ClientKey != "" {
		certData, err := os.ReadFile(authInfo.ClientCertificate)
		if err != nil {
			return fmt.Errorf("failed to read client certificate file '%s': %w", authInfo.ClientCertificate, err)
		}
		keyData, err := os.ReadFile(authInfo.ClientKey)
		if err != nil {
			return fmt.Errorf("failed to read client key file '%s': %w", authInfo.ClientKey, err)
		}
		config.TLSClientConfig.CertData = certData
		config.TLSClientConfig.KeyData = keyData
		return nil
	}

	return fmt.Errorf("no supported authentication method found in kubeconfig")
}
