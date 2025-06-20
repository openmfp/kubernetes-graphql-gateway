package kcp

import (
	"context"
	"errors"

	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	kcpctrl "sigs.k8s.io/controller-runtime/pkg/kcp"

	kcpapis "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	"github.com/openmfp/golang-commons/controller/lifecycle"
	"github.com/openmfp/golang-commons/logger"
	"github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/pkg/apischema"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/pkg/types"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/pkg/workspacefile"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/reconciler/kcp/clusterpath"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/reconciler/kcp/discoveryclient"
)

var (
	ErrCreateIOHandler       = errors.New("failed to create IO Handler")
	ErrCreatePathResolver    = errors.New("failed to create cluster path resolver")
	ErrCreateDiscoveryClient = errors.New("failed to create discovery client")
)

// CreateKCPReconciler creates a KCP reconciler with workspace discovery
func CreateKCPReconciler(
	appCfg config.Config,
	opts types.ReconcilerOpts,
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
	pathResolver, err := clusterpath.NewResolver(opts.Config, opts.Scheme)
	if err != nil {
		return nil, err
	}

	// Create discovery factory
	discoveryFactory, err := discoverFactory(opts.Config)
	if err != nil {
		return nil, err
	}

	return NewReconciler(opts, ioHandler, pathResolver, discoveryFactory, schemaResolver, log)
}

// KCPReconciler handles reconciliation for KCP clusters
type KCPReconciler struct {
	lifecycleManager *lifecycle.LifecycleManager
	opts             types.ReconcilerOpts
	restCfg          *rest.Config
	mgr              ctrl.Manager
	ioHandler        workspacefile.IOHandler
	pathResolver     clusterpath.Resolver
	discoveryFactory discoveryclient.Factory
	schemaResolver   apischema.Resolver
	log              *logger.Logger
}

func NewReconciler(
	opts types.ReconcilerOpts,
	ioHandler workspacefile.IOHandler,
	pathResolver clusterpath.Resolver,
	discoveryFactory discoveryclient.Factory,
	schemaResolver apischema.Resolver,
	log *logger.Logger,
) (types.CustomReconciler, error) {
	// Create KCP-aware manager
	mgr, err := kcpctrl.NewClusterAwareManager(opts.Config, opts.ManagerOpts)
	if err != nil {
		return nil, err
	}

	r := &KCPReconciler{
		opts:             opts,
		restCfg:          opts.Config,
		mgr:              mgr,
		ioHandler:        ioHandler,
		pathResolver:     pathResolver,
		discoveryFactory: discoveryFactory,
		schemaResolver:   schemaResolver,
		log:              log,
	}

	// Create lifecycle manager with subroutines
	r.lifecycleManager = lifecycle.NewLifecycleManager(
		log,
		"kcp-reconciler",
		"kcp-reconciler",
		opts.Client,
		[]lifecycle.Subroutine{
			&processAPIBindingSubroutine{reconciler: r},
		},
	)

	return r, nil
}

func (r *KCPReconciler) GetManager() ctrl.Manager {
	return r.mgr
}

func (r *KCPReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Fetch the APIBinding resource
	apiBinding := &kcpapis.APIBinding{}
	if err := r.opts.Client.Get(ctx, req.NamespacedName, apiBinding); err != nil {
		// If the resource is not found, it might have been deleted
		if client.IgnoreNotFound(err) == nil {
			r.log.Info().Str("apiBinding", req.Name).Msg("APIBinding resource not found, might have been deleted")
			return ctrl.Result{}, nil
		}
		r.log.Error().Err(err).Str("apiBinding", req.Name).Msg("failed to fetch APIBinding resource")
		return ctrl.Result{}, err
	}

	return r.lifecycleManager.Reconcile(ctx, req, apiBinding)
}

func (r *KCPReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kcpapis.APIBinding{}).
		Complete(r)
}
