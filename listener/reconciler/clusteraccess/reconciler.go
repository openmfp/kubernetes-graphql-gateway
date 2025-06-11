package clusteraccess

import (
	"context"
	"errors"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/openmfp/golang-commons/logger"
	gatewayv1alpha1 "github.com/openmfp/kubernetes-graphql-gateway/common/apis/gateway/v1alpha1"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/apischema"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/reconciler"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/workspacefile"
)

var (
	ErrCreateIOHandler  = errors.New("failed to create IO Handler")
	ErrCreateRestMapper = errors.New("failed to create rest mapper")
	ErrCreateHTTPClient = errors.New("failed to create http client")
	ErrGenerateSchema   = errors.New("failed to generate OpenAPI Schema")
)

// NoOpReconciler is a reconciler that does nothing - used when ClusterAccess manages target clusters
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

func NewReconciler(
	opts reconciler.ReconcilerOpts,
	discoveryInterface discovery.DiscoveryInterface,
	log *logger.Logger,
) (reconciler.CustomReconciler, error) {
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

	if err = PreReconcileWithClusterAccess(schemaResolver, ioHandler, opts.Client, log); err != nil {
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
		targetConfig, clusterName, err := BuildTargetClusterConfigFromTyped(item, client)
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
