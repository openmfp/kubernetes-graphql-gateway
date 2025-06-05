package manager

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
	"github.com/openmfp/golang-commons/logger"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/kcp"

	gatewayv1alpha1 "github.com/openmfp/kubernetes-graphql-gateway/common/apis/gateway/v1alpha1"
	appConfig "github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"github.com/openmfp/kubernetes-graphql-gateway/gateway/resolver"
)

type Provider interface {
	Start()
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

type Service struct {
	AppCfg  appConfig.Config
	restCfg *rest.Config

	log *logger.Logger

	// Management cluster client for reading ClusterAccess CRDs
	managementClient client.Client
	// Multiple resolvers, one per target cluster
	resolvers map[string]resolver.Provider

	handlers handlerStore
	watcher  *fsnotify.Watcher
}

func NewManager(log *logger.Logger, cfg *rest.Config, appCfg appConfig.Config) (*Service, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// lets ensure that kcp url points directly to kcp domain
	u, err := url.Parse(cfg.Host)
	if err != nil {
		return nil, err
	}
	cfg.Host = fmt.Sprintf("%s://%s", u.Scheme, u.Host)

	cfg.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return NewRoundTripper(log, rt, appCfg.Gateway.UsernameClaim, appCfg.Gateway.ShouldImpersonate)
	})

	// Create scheme with ClusterAccess CRDs
	scheme := runtime.NewScheme()
	utilruntime.Must(gatewayv1alpha1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))

	// Create management cluster client for reading ClusterAccess CRDs
	managementClient, err := kcp.NewClusterAwareClientWithWatch(cfg, client.Options{
		Scheme: scheme,
	})
	if err != nil {
		return nil, err
	}

	m := &Service{
		AppCfg: appCfg,
		handlers: handlerStore{
			registry: make(map[string]*graphqlHandler),
		},
		log:              log,
		managementClient: managementClient,
		resolvers:        make(map[string]resolver.Provider),
		restCfg:          cfg,
		watcher:          watcher,
	}

	// Initialize target cluster clients and resolvers
	err = m.initializeTargetClusters()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to initialize target clusters, will retry when files change")
	}

	err = m.watcher.Add(appCfg.OpenApiDefinitionsPath)
	if err != nil {
		return nil, err
	}

	files, err := filepath.Glob(filepath.Join(appCfg.OpenApiDefinitionsPath, "*"))
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		filename := filepath.Base(file)
		m.OnFileChanged(filename)
	}

	m.Start()

	return m, nil
}

// initializeTargetClusters reads ClusterAccess CRDs and creates runtime clients for each target cluster
func (s *Service) initializeTargetClusters() error {
	ctx := context.Background()

	s.log.Info().Msg("Initializing target clusters from ClusterAccess resources")

	// List all ClusterAccess resources
	clusterAccessList := &gatewayv1alpha1.ClusterAccessList{}
	if err := s.managementClient.List(ctx, clusterAccessList); err != nil {
		s.log.Error().Err(err).Msg("Failed to list ClusterAccess resources")
		return fmt.Errorf("failed to list ClusterAccess resources: %w", err)
	}

	s.log.Info().Int("count", len(clusterAccessList.Items)).Msg("Found ClusterAccess resources")

	// Clear existing resolvers
	s.resolvers = make(map[string]resolver.Provider)

	// For each ClusterAccess resource, create a runtime client and resolver
	for _, item := range clusterAccessList.Items {
		clusterAccessName := item.GetName()
		s.log.Info().Str("clusterAccess", clusterAccessName).Msg("Processing ClusterAccess resource")

		// Extract target cluster config from ClusterAccess spec
		targetConfig, clusterName, err := s.buildTargetClusterConfig(item)
		if err != nil {
			s.log.Error().Err(err).Str("clusterAccess", clusterAccessName).Msg("Failed to build target cluster config")
			continue
		}

		s.log.Info().Str("clusterAccess", clusterAccessName).Str("host", targetConfig.Host).Str("clusterName", clusterName).Msg("Built target cluster config")

		// Create runtime client for target cluster
		targetClient, err := client.NewWithWatch(targetConfig, client.Options{})
		if err != nil {
			s.log.Error().Err(err).Str("clusterAccess", clusterAccessName).Msg("Failed to create runtime client for target cluster")
			continue
		}

		// Create resolver for target cluster
		targetResolver := resolver.New(s.log, targetClient)
		s.resolvers[clusterName] = targetResolver

		s.log.Info().Str("clusterAccess", clusterAccessName).Str("clusterName", clusterName).Msg("Successfully created runtime client and resolver for target cluster")
	}

	s.log.Info().Int("clusters", len(s.resolvers)).Msg("Completed target cluster initialization")
	return nil
}

// getResolverForCluster returns the resolver for a specific cluster
func (s *Service) getResolverForCluster(clusterName string) (resolver.Provider, bool) {
	resolver, exists := s.resolvers[clusterName]
	return resolver, exists
}

// buildTargetClusterConfig extracts connection info from ClusterAccess and builds rest.Config
func (s *Service) buildTargetClusterConfig(clusterAccess gatewayv1alpha1.ClusterAccess) (*rest.Config, string, error) {
	spec := clusterAccess.Spec

	// Extract host (required)
	host := spec.Host
	if host == "" {
		return nil, "", fmt.Errorf("host field not found in ClusterAccess spec")
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
		caData, err := s.extractCAData(spec.CA)
		if err != nil {
			return nil, "", fmt.Errorf("failed to extract CA data: %w", err)
		}
		if caData != nil {
			config.TLSClientConfig.CAData = caData
			config.TLSClientConfig.Insecure = false // Use proper TLS verification when CA is provided
		}
	}

	// Handle Auth configuration
	if spec.Auth != nil {
		err := s.configureAuthentication(config, spec.Auth)
		if err != nil {
			return nil, "", fmt.Errorf("failed to configure authentication: %w", err)
		}
	}

	return config, clusterName, nil
}

// extractCAData extracts CA certificate data from secret or configmap references
func (s *Service) extractCAData(ca *gatewayv1alpha1.CAConfig) ([]byte, error) {
	ctx := context.Background()

	if ca.SecretRef != nil {
		secret := &corev1.Secret{}
		namespace := ca.SecretRef.Namespace
		if namespace == "" {
			namespace = "default" // Use default namespace if not specified
		}

		err := s.managementClient.Get(ctx, types.NamespacedName{
			Name:      ca.SecretRef.Name,
			Namespace: namespace,
		}, secret)
		if err != nil {
			return nil, fmt.Errorf("failed to get CA secret: %w", err)
		}

		caData, ok := secret.Data[ca.SecretRef.Key]
		if !ok {
			return nil, fmt.Errorf("CA key not found in secret")
		}

		return caData, nil
	}

	if ca.ConfigMapRef != nil {
		configMap := &corev1.ConfigMap{}
		namespace := ca.ConfigMapRef.Namespace
		if namespace == "" {
			namespace = "default"
		}

		err := s.managementClient.Get(ctx, types.NamespacedName{
			Name:      ca.ConfigMapRef.Name,
			Namespace: namespace,
		}, configMap)
		if err != nil {
			return nil, fmt.Errorf("failed to get CA configmap: %w", err)
		}

		caData, ok := configMap.Data[ca.ConfigMapRef.Key]
		if !ok {
			return nil, fmt.Errorf("CA key not found in configmap")
		}

		return []byte(caData), nil
	}

	return nil, nil
}

// configureAuthentication configures authentication for the target cluster
func (s *Service) configureAuthentication(config *rest.Config, auth *gatewayv1alpha1.AuthConfig) error {
	ctx := context.Background()

	if auth.SecretRef != nil {
		secret := &corev1.Secret{}
		namespace := auth.SecretRef.Namespace
		if namespace == "" {
			namespace = "default"
		}

		err := s.managementClient.Get(ctx, types.NamespacedName{
			Name:      auth.SecretRef.Name,
			Namespace: namespace,
		}, secret)
		if err != nil {
			return fmt.Errorf("failed to get auth secret: %w", err)
		}

		token, ok := secret.Data[auth.SecretRef.Key]
		if !ok {
			return fmt.Errorf("auth key not found in secret")
		}

		config.BearerToken = string(token)
		return nil
	}

	if auth.ClientCertificateRef != nil {
		secret := &corev1.Secret{}
		namespace := auth.ClientCertificateRef.Namespace
		if namespace == "" {
			namespace = "default"
		}

		err := s.managementClient.Get(ctx, types.NamespacedName{
			Name:      auth.ClientCertificateRef.Name,
			Namespace: namespace,
		}, secret)
		if err != nil {
			return fmt.Errorf("failed to get client certificate secret: %w", err)
		}

		certData, certOk := secret.Data["tls.crt"]
		keyData, keyOk := secret.Data["tls.key"]
		if !certOk || !keyOk {
			return fmt.Errorf("client certificate or key not found in secret (expected tls.crt and tls.key)")
		}

		config.TLSClientConfig.CertData = certData
		config.TLSClientConfig.KeyData = keyData
		return nil
	}

	if auth.KubeconfigSecretRef != nil {
		secret := &corev1.Secret{}
		namespace := auth.KubeconfigSecretRef.Namespace
		if namespace == "" {
			namespace = "default"
		}

		err := s.managementClient.Get(ctx, types.NamespacedName{
			Name:      auth.KubeconfigSecretRef.Name,
			Namespace: namespace,
		}, secret)
		if err != nil {
			return fmt.Errorf("failed to get kubeconfig secret: %w", err)
		}

		kubeconfigData, ok := secret.Data["kubeconfig"]
		if !ok {
			return fmt.Errorf("kubeconfig not found in secret (expected key: kubeconfig)")
		}

		// Create a temporary file with the kubeconfig data
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

		// Use clientcmd to load the kubeconfig
		kubeconfigConfig, err := clientcmd.LoadFromFile(tmpFile.Name())
		if err != nil {
			return fmt.Errorf("failed to load kubeconfig: %w", err)
		}

		// Build rest config from kubeconfig
		restConfig, err := clientcmd.NewDefaultClientConfig(*kubeconfigConfig, &clientcmd.ConfigOverrides{}).ClientConfig()
		if err != nil {
			return fmt.Errorf("failed to create rest config from kubeconfig: %w", err)
		}

		// Copy authentication details from the kubeconfig to our config
		config.Username = restConfig.Username
		config.Password = restConfig.Password
		config.BearerToken = restConfig.BearerToken
		config.BearerTokenFile = restConfig.BearerTokenFile
		config.Impersonate = restConfig.Impersonate
		config.AuthProvider = restConfig.AuthProvider
		config.AuthConfigPersister = restConfig.AuthConfigPersister
		config.ExecProvider = restConfig.ExecProvider
		config.TLSClientConfig.CertFile = restConfig.TLSClientConfig.CertFile
		config.TLSClientConfig.KeyFile = restConfig.TLSClientConfig.KeyFile
		config.TLSClientConfig.CertData = restConfig.TLSClientConfig.CertData
		config.TLSClientConfig.KeyData = restConfig.TLSClientConfig.KeyData

		// Override CA data if it was already set from ClusterAccess CA config
		if config.TLSClientConfig.CAData == nil {
			config.TLSClientConfig.CAData = restConfig.TLSClientConfig.CAData
			config.TLSClientConfig.CAFile = restConfig.TLSClientConfig.CAFile
		}

		// If no CA data is available and original kubeconfig was insecure, keep insecure
		if config.TLSClientConfig.CAData == nil && config.TLSClientConfig.CAFile == "" && restConfig.TLSClientConfig.Insecure {
			config.TLSClientConfig.Insecure = true
		} else if config.TLSClientConfig.CAData != nil || config.TLSClientConfig.CAFile != "" {
			config.TLSClientConfig.Insecure = false
		}

		return nil
	}

	if auth.ServiceAccount != "" {
		return fmt.Errorf("service account authentication not yet implemented")
	}

	return nil
}
