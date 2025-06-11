package clusteraccess

import (
	"context"
	"errors"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/openmfp/golang-commons/logger"
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
