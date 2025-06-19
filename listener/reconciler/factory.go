package reconciler

import (
	"errors"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/openmfp/golang-commons/logger"
	"github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/apischema"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/clusterpath"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/discoveryclient"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/reconciler/clusteraccess"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/reconciler/kcp"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/reconciler/standard"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/reconciler/types"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/workspacefile"
)

// CreateKCPReconciler creates a KCP reconciler with workspace discovery
func CreateKCPReconciler(
	appCfg config.Config,
	opts types.ReconcilerOpts,
	restCfg *rest.Config,
	discoverFactory func(cfg *rest.Config) (*discoveryclient.FactoryProvider, error),
	log *logger.Logger,
) (types.CustomReconciler, error) {
	log.Info().Msg("Using KCP reconciler with workspace discovery")

	// Create IO handler
	ioHandler, err := workspacefile.NewIOHandler(appCfg.OpenApiDefinitionsPath)
	if err != nil {
		return nil, err
	}

	// Create schema resolver
	schemaResolver := apischema.NewResolver()

	// Create cluster path resolver
	pathResolver, err := clusterpath.NewResolver(restCfg, opts.Scheme)
	if err != nil {
		return nil, err
	}

	// Create discovery factory
	discoveryFactory, err := discoverFactory(restCfg)
	if err != nil {
		return nil, err
	}

	return kcp.NewReconciler(opts, restCfg, ioHandler, pathResolver, discoveryFactory, schemaResolver, log)
}

// CreateMultiClusterReconciler creates a multi-cluster reconciler using ClusterAccess CRDs
func CreateMultiClusterReconciler(
	appCfg config.Config,
	opts types.ReconcilerOpts,
	restCfg *rest.Config,
	log *logger.Logger,
) (types.CustomReconciler, error) {
	log.Info().Msg("Using multi-cluster reconciler")

	// Check if ClusterAccess CRD is available
	caStatus, err := clusteraccess.CheckClusterAccessCRDStatus(opts.Client, log)
	if err != nil {
		if errors.Is(err, clusteraccess.ErrCRDNotRegistered) {
			log.Error().Msg("Multi-cluster mode enabled but ClusterAccess CRD not registered")
			return nil, errors.New("multi-cluster mode enabled but ClusterAccess CRD not registered")
		}
		log.Error().Err(err).Msg("Multi-cluster mode enabled but failed to check ClusterAccess CRD status")
		return nil, err
	}

	if caStatus != clusteraccess.CRDRegistered {
		log.Error().Msg("Multi-cluster mode enabled but ClusterAccess CRD not available")
		return nil, errors.New("multi-cluster mode enabled but ClusterAccess CRD not available")
	}

	// Create IO handler
	ioHandler, err := workspacefile.NewIOHandler(appCfg.OpenApiDefinitionsPath)
	if err != nil {
		return nil, err
	}

	// Create schema resolver
	schemaResolver := apischema.NewResolver()

	log.Info().Msg("ClusterAccess CRD registered, creating ClusterAccess reconciler")
	return clusteraccess.NewReconciler(opts, restCfg, ioHandler, schemaResolver, log)
}

// CreateSingleClusterReconciler creates a standard single-cluster reconciler
func CreateSingleClusterReconciler(
	appCfg config.Config,
	opts types.ReconcilerOpts,
	restCfg *rest.Config,
	discoveryInterface discovery.DiscoveryInterface,
	log *logger.Logger,
) (types.CustomReconciler, error) {
	log.Info().Msg("Using standard reconciler for single-cluster mode")

	// Create IO handler
	ioHandler, err := workspacefile.NewIOHandler(appCfg.OpenApiDefinitionsPath)
	if err != nil {
		return nil, err
	}

	// Create schema resolver
	schemaResolver := apischema.NewResolver()

	// Create REST mapper
	httpClient, err := rest.HTTPClientFor(restCfg)
	if err != nil {
		return nil, err
	}
	restMapper, err := apiutil.NewDynamicRESTMapper(restCfg, httpClient)
	if err != nil {
		return nil, err
	}

	return standard.NewReconciler(opts, restCfg, ioHandler, schemaResolver, discoveryInterface, restMapper, log)
}
