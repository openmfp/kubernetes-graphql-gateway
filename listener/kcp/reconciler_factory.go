package kcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/openmfp/golang-commons/logger"
	gatewayv1alpha1 "github.com/openmfp/kubernetes-graphql-gateway/common/apis/gateway/v1alpha1"
	"github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/apischema"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/clusterpath"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/controller"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/discoveryclient"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/workspacefile"
)

var (
	ErrCreateDiscoveryClient = errors.New("failed to create discovery client")
	ErrCreateIOHandler       = errors.New("failed to create IO Handler")
	ErrCreateRestMapper      = errors.New("failed to create rest mapper")
	ErrGenerateSchema        = errors.New("failed to generate OpenAPI Schema")
	ErrResolveSchema         = errors.New("failed to resolve server JSON schema")
	ErrWriteJSON             = errors.New("failed to write JSON to filesystem")
	ErrCreatePathResolver    = errors.New("failed to create cluster path resolver")
	ErrGetVWConfig           = errors.New("unable to get virtual workspace config, check if your kcp cluster is running")
	ErrCreateHTTPClient      = errors.New("failed to create http client")
)

type CustomReconciler interface {
	Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error)
	SetupWithManager(mgr ctrl.Manager) error
}

type ReconcilerOpts struct {
	*rest.Config
	*runtime.Scheme
	client.Client
	OpenAPIDefinitionsPath string
}

type PreReconcileClusterAccessFunc func(cr *apischema.CRDResolver, io workspacefile.IOHandler, client client.Client, log *logger.Logger) error

// NoOpReconciler is a reconciler that does nothing - used when ClusterAccess is managing target clusters
type NoOpReconciler struct {
	log *logger.Logger
}

func (r *NoOpReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// No-op: ClusterAccess manages target clusters, not the management cluster
	return ctrl.Result{}, nil
}

func (r *NoOpReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// No setup needed for no-op reconciler
	r.log.Info().Msg("ClusterAccess mode: Management cluster CRD reconciler disabled")
	return nil
}

func NewReconciler(appCfg config.Config, opts ReconcilerOpts, restcfg *rest.Config,
	discoveryInterface discovery.DiscoveryInterface,
	preReconcileFunc PreReconcileClusterAccessFunc,
	discoverFactory func(cfg *rest.Config) (*discoveryclient.FactoryProvider, error),
	log *logger.Logger,
) (CustomReconciler, error) {
	if !appCfg.EnableKcp {
		return newClusterAccessReconciler(opts, discoveryInterface, preReconcileFunc, log)
	}

	return newKcpReconciler(opts, restcfg, discoverFactory, log)
}

func newClusterAccessReconciler(
	opts ReconcilerOpts,
	discoveryInterface discovery.DiscoveryInterface,
	preReconcileFunc PreReconcileClusterAccessFunc,
	log *logger.Logger,
) (CustomReconciler, error) {
	ioHandler, err := workspacefile.NewIOHandler(opts.OpenAPIDefinitionsPath)
	if err != nil {
		return nil, errors.Join(ErrCreateIOHandler, err)
	}

	rm, err := restMapperFromConfig(opts.Config)
	if err != nil {
		return nil, err
	}

	schemaResolver := &apischema.CRDResolver{
		DiscoveryInterface: discoveryInterface,
		RESTMapper:         rm,
	}

	if err = preReconcileFunc(schemaResolver, ioHandler, opts.Client, log); err != nil {
		return nil, errors.Join(ErrGenerateSchema, err)
	}

	// Return NoOpReconciler since ClusterAccess manages target clusters, not the management cluster
	return &NoOpReconciler{log: log}, nil
}

func restMapperFromConfig(cfg *rest.Config) (meta.RESTMapper, error) {
	httpClt, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return nil, errors.Join(ErrCreateHTTPClient, err)
	}
	rm, err := apiutil.NewDynamicRESTMapper(cfg, httpClt)
	if err != nil {
		return nil, errors.Join(ErrCreateRestMapper, err)
	}

	return rm, nil
}

func newKcpReconciler(opts ReconcilerOpts, restcfg *rest.Config, newDiscoveryFactoryFunc func(cfg *rest.Config) (*discoveryclient.FactoryProvider, error), log *logger.Logger) (CustomReconciler, error) {
	ioHandler, err := workspacefile.NewIOHandler(opts.OpenAPIDefinitionsPath)
	if err != nil {
		return nil, errors.Join(ErrCreateIOHandler, err)
	}

	pr, err := clusterpath.NewResolver(opts.Config, opts.Scheme)
	if err != nil {
		return nil, errors.Join(ErrCreatePathResolver, err)
	}

	df, err := newDiscoveryFactoryFunc(restcfg)
	if err != nil {
		return nil, errors.Join(ErrCreateDiscoveryClient, err)
	}

	return controller.NewAPIBindingReconciler(
		ioHandler, df, apischema.NewResolver(), pr, log,
	), nil
}

func PreReconcileWithClusterAccess(
	cr *apischema.CRDResolver,
	io workspacefile.IOHandler,
	client client.Client,
	log *logger.Logger,
) error {
	ctx := context.Background()

	log.Info().Msg("starting PreReconcileWithClusterAccess")

	// List all ClusterAccess resources
	clusterAccessList := &gatewayv1alpha1.ClusterAccessList{}

	if err := client.List(ctx, clusterAccessList); err != nil {
		log.Error().Err(err).Msg("failed to list ClusterAccess resources")
		return errors.Join(errors.New("failed to list ClusterAccess resources"), err)
	}

	log.Info().Int("count", len(clusterAccessList.Items)).Msg("found ClusterAccess resources")

	// For each ClusterAccess resource, generate schema for target cluster
	for _, item := range clusterAccessList.Items {
		clusterAccessName := item.GetName()
		log.Info().Str("clusterAccess", clusterAccessName).Msg("processing ClusterAccess resource")

		// Extract target cluster config from ClusterAccess spec
		targetConfig, clusterName, err := buildTargetClusterConfigFromTyped(item, client)
		if err != nil {
			log.Error().Err(err).Str("clusterAccess", clusterAccessName).Msg("failed to build target cluster config")
			continue
		}

		log.Info().Str("clusterAccess", clusterAccessName).Str("host", targetConfig.Host).Str("clusterName", clusterName).Msg("extracted target cluster config")

		// Create discovery client for target cluster
		targetDiscovery, err := discovery.NewDiscoveryClientForConfig(targetConfig)
		if err != nil {
			log.Error().Err(err).Str("clusterAccess", clusterAccessName).Msg("failed to create discovery client for target cluster")
			continue
		}

		log.Info().Str("clusterAccess", clusterAccessName).Msg("created discovery client for target cluster")

		// Create REST mapper for target cluster
		targetRM, err := restMapperFromConfig(targetConfig)
		if err != nil {
			log.Error().Err(err).Str("clusterAccess", clusterAccessName).Msg("failed to create REST mapper for target cluster")
			continue
		}

		log.Info().Str("clusterAccess", clusterAccessName).Msg("created REST mapper for target cluster")

		// Create schema resolver for target cluster
		targetResolver := &apischema.CRDResolver{
			DiscoveryInterface: targetDiscovery,
			RESTMapper:         targetRM,
		}

		log.Info().Str("clusterAccess", clusterAccessName).Msg("attempting to resolve schema for target cluster")

		// Generate schema for target cluster
		JSON, err := targetResolver.Resolve()
		if err != nil {
			log.Error().Err(err).Str("clusterAccess", clusterAccessName).Msg("failed to resolve schema for target cluster")
			continue
		}

		log.Info().Str("clusterAccess", clusterAccessName).Int("schemaSize", len(JSON)).Msg("successfully resolved schema for target cluster")

		// Create the complete schema file with x-cluster-metadata
		schemaWithMetadata, err := injectClusterMetadata(JSON, item, client, log)
		if err != nil {
			log.Error().Err(err).Str("clusterAccess", clusterAccessName).Msg("failed to inject cluster metadata into schema")
			continue
		}

		// Write schema to file using cluster name from path or resource name
		if err := io.Write(schemaWithMetadata, clusterName); err != nil {
			log.Error().Err(err).Str("clusterAccess", clusterAccessName).Str("clusterName", clusterName).Msg("failed to write schema for target cluster")
			continue
		}

		log.Info().Str("clusterAccess", clusterAccessName).Str("clusterName", clusterName).Msg("successfully generated schema for target cluster")
	}

	log.Info().Msg("completed PreReconcileWithClusterAccess")
	return nil
}

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

// extractCAData extracts CA certificate data from secret or configmap references
func extractCAData(ca *gatewayv1alpha1.CAConfig, k8sClient client.Client) ([]byte, error) {
	ctx := context.Background()

	if ca.SecretRef != nil {
		secret := &corev1.Secret{}
		namespace := ca.SecretRef.Namespace
		if namespace == "" {
			namespace = "default" // Use default namespace if not specified
		}

		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      ca.SecretRef.Name,
			Namespace: namespace,
		}, secret)
		if err != nil {
			return nil, errors.Join(errors.New("failed to get CA secret"), err)
		}

		caData, ok := secret.Data[ca.SecretRef.Key]
		if !ok {
			return nil, errors.New("CA key not found in secret")
		}

		return caData, nil
	}

	if ca.ConfigMapRef != nil {
		configMap := &corev1.ConfigMap{}
		namespace := ca.ConfigMapRef.Namespace
		if namespace == "" {
			namespace = "default"
		}

		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      ca.ConfigMapRef.Name,
			Namespace: namespace,
		}, configMap)
		if err != nil {
			return nil, errors.Join(errors.New("failed to get CA configmap"), err)
		}

		caData, ok := configMap.Data[ca.ConfigMapRef.Key]
		if !ok {
			return nil, errors.New("CA key not found in configmap")
		}

		return []byte(caData), nil
	}

	return nil, nil
}

// configureAuthentication configures authentication for the target cluster
func configureAuthentication(config *rest.Config, auth *gatewayv1alpha1.AuthConfig, k8sClient client.Client) error {
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
			return errors.Join(errors.New("failed to get auth secret"), err)
		}

		token, ok := secret.Data[auth.SecretRef.Key]
		if !ok {
			return errors.New("auth key not found in secret")
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

		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      auth.ClientCertificateRef.Name,
			Namespace: namespace,
		}, secret)
		if err != nil {
			return errors.Join(errors.New("failed to get client certificate secret"), err)
		}

		certData, certOk := secret.Data["tls.crt"]
		keyData, keyOk := secret.Data["tls.key"]
		if !certOk || !keyOk {
			return errors.New("client certificate or key not found in secret (expected tls.crt and tls.key)")
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

		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      auth.KubeconfigSecretRef.Name,
			Namespace: namespace,
		}, secret)
		if err != nil {
			return errors.Join(errors.New("failed to get kubeconfig secret"), err)
		}

		kubeconfigData, ok := secret.Data["kubeconfig"]
		if !ok {
			return errors.New("kubeconfig not found in secret (expected key: kubeconfig)")
		}

		// Load kubeconfig to extract authentication credentials
		tmpFile, err := os.CreateTemp("", "kubeconfig-*.yaml")
		if err != nil {
			return errors.Join(errors.New("failed to create temporary kubeconfig file"), err)
		}
		defer os.Remove(tmpFile.Name())

		if _, err := tmpFile.Write(kubeconfigData); err != nil {
			tmpFile.Close()
			return errors.Join(errors.New("failed to write kubeconfig data"), err)
		}
		tmpFile.Close()

		kubeconfigConfig, err := clientcmd.LoadFromFile(tmpFile.Name())
		if err != nil {
			return errors.Join(errors.New("failed to load kubeconfig"), err)
		}

		restConfig, err := clientcmd.NewDefaultClientConfig(*kubeconfigConfig, &clientcmd.ConfigOverrides{}).ClientConfig()
		if err != nil {
			return errors.Join(errors.New("failed to create rest config from kubeconfig"), err)
		}

		// Copy authentication from kubeconfig but override TLS for development
		config.BearerToken = restConfig.BearerToken
		config.TLSClientConfig.CertData = restConfig.TLSClientConfig.CertData
		config.TLSClientConfig.KeyData = restConfig.TLSClientConfig.KeyData

		// Override the base Insecure setting - turn OFF insecure to enable client certs
		config.TLSClientConfig.Insecure = false
		config.TLSClientConfig.CAData = restConfig.TLSClientConfig.CAData
		config.TLSClientConfig.ServerName = "target-control-plane"
		return nil
	}

	if auth.ServiceAccount != "" {
		// TODO: Implement service account-based authentication
		return errors.New("service account authentication not yet implemented")
	}

	// No authentication configured - this might work for some clusters
	return nil
}

func injectClusterMetadata(schemaJSON []byte, clusterAccess gatewayv1alpha1.ClusterAccess, k8sClient client.Client, log *logger.Logger) ([]byte, error) {
	// Parse the existing schema JSON
	var schemaData map[string]interface{}
	if err := json.Unmarshal(schemaJSON, &schemaData); err != nil {
		return nil, fmt.Errorf("failed to parse schema JSON: %w", err)
	}

	// Create cluster metadata
	metadata := map[string]interface{}{
		"host": clusterAccess.Spec.Host,
	}

	// Add path if specified
	if clusterAccess.Spec.Path != "" {
		metadata["path"] = clusterAccess.Spec.Path
	} else {
		metadata["path"] = clusterAccess.GetName()
	}

	// Extract auth data and potentially CA data from kubeconfig
	var kubeconfigCAData []byte
	if clusterAccess.Spec.Auth != nil {
		authMetadata, err := extractAuthDataForMetadata(clusterAccess.Spec.Auth, k8sClient)
		if err != nil {
			log.Warn().Err(err).Str("clusterAccess", clusterAccess.GetName()).Msg("failed to extract auth data for metadata")
		} else if authMetadata != nil {
			metadata["auth"] = authMetadata

			// If auth type is kubeconfig, extract CA data from kubeconfig
			if authType, ok := authMetadata["type"].(string); ok && authType == "kubeconfig" {
				if kubeconfigB64, ok := authMetadata["kubeconfig"].(string); ok {
					kubeconfigCAData = extractCAFromKubeconfig(kubeconfigB64, log)
				}
			}
		}
	}

	// Add CA data - prefer explicit CA config, fallback to kubeconfig CA
	if clusterAccess.Spec.CA != nil {
		caData, err := extractCADataForMetadata(clusterAccess.Spec.CA, k8sClient)
		if err != nil {
			log.Warn().Err(err).Str("clusterAccess", clusterAccess.GetName()).Msg("failed to extract CA data for metadata")
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
		log.Info().Str("clusterAccess", clusterAccess.GetName()).Msg("extracted CA data from kubeconfig")
	}

	// Inject the metadata into the schema
	schemaData["x-cluster-metadata"] = metadata

	// Marshal back to JSON
	modifiedJSON, err := json.Marshal(schemaData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal modified schema: %w", err)
	}

	log.Info().
		Str("clusterAccess", clusterAccess.GetName()).
		Str("host", clusterAccess.Spec.Host).
		Msg("successfully injected cluster metadata into schema")

	return modifiedJSON, nil
}

func extractCADataForMetadata(ca *gatewayv1alpha1.CAConfig, k8sClient client.Client) ([]byte, error) {
	ctx := context.Background()

	if ca.SecretRef != nil {
		secret := &corev1.Secret{}
		namespace := ca.SecretRef.Namespace
		if namespace == "" {
			namespace = "default"
		}

		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      ca.SecretRef.Name,
			Namespace: namespace,
		}, secret)
		if err != nil {
			return nil, err
		}

		caData, ok := secret.Data[ca.SecretRef.Key]
		if !ok {
			return nil, errors.New("CA key not found in secret")
		}

		return caData, nil
	}

	if ca.ConfigMapRef != nil {
		configMap := &corev1.ConfigMap{}
		namespace := ca.ConfigMapRef.Namespace
		if namespace == "" {
			namespace = "default"
		}

		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      ca.ConfigMapRef.Name,
			Namespace: namespace,
		}, configMap)
		if err != nil {
			return nil, err
		}

		caData, ok := configMap.Data[ca.ConfigMapRef.Key]
		if !ok {
			return nil, errors.New("CA key not found in configmap")
		}

		return []byte(caData), nil
	}

	return nil, nil
}

func extractAuthDataForMetadata(auth *gatewayv1alpha1.AuthConfig, k8sClient client.Client) (map[string]interface{}, error) {
	ctx := context.Background()

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
			return nil, err
		}

		kubeconfigData, ok := secret.Data["kubeconfig"]
		if !ok {
			return nil, errors.New("kubeconfig not found in secret")
		}

		return map[string]interface{}{
			"type":       "kubeconfig",
			"kubeconfig": base64.StdEncoding.EncodeToString(kubeconfigData),
		}, nil
	}

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
			return nil, err
		}

		token, ok := secret.Data[auth.SecretRef.Key]
		if !ok {
			return nil, errors.New("auth key not found in secret")
		}

		return map[string]interface{}{
			"type":  "token",
			"token": base64.StdEncoding.EncodeToString(token),
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
			return nil, err
		}

		certData, certOk := secret.Data["tls.crt"]
		keyData, keyOk := secret.Data["tls.key"]
		if !certOk || !keyOk {
			return nil, errors.New("client certificate or key not found in secret")
		}

		return map[string]interface{}{
			"type":     "client-cert",
			"certData": base64.StdEncoding.EncodeToString(certData),
			"keyData":  base64.StdEncoding.EncodeToString(keyData),
		}, nil
	}

	return nil, nil
}

func extractCAFromKubeconfig(kubeconfigB64 string, log *logger.Logger) []byte {
	kubeconfigData, err := base64.StdEncoding.DecodeString(kubeconfigB64)
	if err != nil {
		log.Error().Err(err).Msg("failed to decode kubeconfig")
		return nil
	}

	// Load kubeconfig to extract CA data
	tmpFile, err := os.CreateTemp("", "kubeconfig-*.yaml")
	if err != nil {
		log.Error().Err(err).Msg("failed to create temporary kubeconfig file")
		return nil
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(kubeconfigData); err != nil {
		tmpFile.Close()
		log.Error().Err(err).Msg("failed to write kubeconfig data")
		return nil
	}
	tmpFile.Close()

	kubeconfigConfig, err := clientcmd.LoadFromFile(tmpFile.Name())
	if err != nil {
		log.Error().Err(err).Msg("failed to load kubeconfig")
		return nil
	}

	restConfig, err := clientcmd.NewDefaultClientConfig(*kubeconfigConfig, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		log.Error().Err(err).Msg("failed to create rest config from kubeconfig")
		return nil
	}

	return restConfig.TLSClientConfig.CAData
}
