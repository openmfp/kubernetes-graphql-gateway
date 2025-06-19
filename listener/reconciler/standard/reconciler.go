package standard

import (
	"context"
	"errors"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/openmfp/golang-commons/controller/lifecycle"
	"github.com/openmfp/golang-commons/logger"
	"github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/apischema"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/reconciler/types"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/workspacefile"
)

const (
	kubernetesClusterName = "kubernetes" // Used as schema file name for standard k8s cluster
)

var (
	ErrCreateIOHandler  = errors.New("failed to create IO Handler")
	ErrCreateRESTMapper = errors.New("failed to create REST mapper")
	ErrCreateHTTPClient = errors.New("failed to create HTTP client")
	ErrGenerateSchema   = errors.New("failed to generate schema")
	ErrResolveSchema    = errors.New("failed to resolve server JSON schema")
	ErrReadJSON         = errors.New("failed to read JSON from filesystem")
	ErrWriteJSON        = errors.New("failed to write JSON to filesystem")
)

// CreateSingleClusterReconciler creates a standard single-cluster reconciler
func CreateSingleClusterReconciler(
	appCfg config.Config,
	opts types.ReconcilerOpts,
	restCfg *rest.Config,
	mgrOpts ctrl.Options,
	log *logger.Logger,
) (types.CustomReconciler, error) {
	log.Info().Msg("Using standard reconciler for single-cluster mode")

	// Create discovery client
	discoveryInterface, err := discovery.NewDiscoveryClientForConfig(restCfg)
	if err != nil {
		return nil, err
	}

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

	return NewReconciler(opts, restCfg, mgrOpts, ioHandler, schemaResolver, discoveryInterface, restMapper, log)
}

// StandardReconciler handles reconciliation for standard non-KCP clusters
type StandardReconciler struct {
	lifecycleManager *lifecycle.LifecycleManager
	opts             types.ReconcilerOpts
	restCfg          *rest.Config
	mgr              ctrl.Manager
	ioHandler        workspacefile.IOHandler
	schemaResolver   apischema.Resolver
	discoveryClient  discovery.DiscoveryInterface
	restMapper       meta.RESTMapper
	log              *logger.Logger
}

func NewReconciler(
	opts types.ReconcilerOpts,
	restCfg *rest.Config,
	mgrOpts ctrl.Options,
	ioHandler workspacefile.IOHandler,
	schemaResolver apischema.Resolver,
	discoveryClient discovery.DiscoveryInterface,
	restMapper meta.RESTMapper,
	log *logger.Logger,
) (types.CustomReconciler, error) {
	// Create standard manager
	mgr, err := ctrl.NewManager(restCfg, mgrOpts)
	if err != nil {
		return nil, err
	}

	r := &StandardReconciler{
		opts:            opts,
		restCfg:         restCfg,
		mgr:             mgr,
		ioHandler:       ioHandler,
		schemaResolver:  schemaResolver,
		discoveryClient: discoveryClient,
		restMapper:      restMapper,
		log:             log,
	}

	// Create lifecycle manager with subroutines
	r.lifecycleManager = lifecycle.NewLifecycleManager(
		log,
		"standard-reconciler",
		"standard-reconciler",
		opts.Client,
		[]lifecycle.Subroutine{
			&generateSchemaSubroutine{reconciler: r},
			&processClusterAccessSubroutine{reconciler: r},
		},
	)

	return r, nil
}

func (r *StandardReconciler) GetManager() ctrl.Manager {
	return r.mgr
}

func (r *StandardReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Fetch the CRD resource that triggered this reconciliation
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := r.opts.Client.Get(ctx, req.NamespacedName, crd); err != nil {
		r.log.Error().Err(err).Str("name", req.Name).Msg("failed to get CRD resource")
		// Continue with lifecycle manager even if CRD is not found (might be deleted)
		return r.lifecycleManager.Reconcile(ctx, req, nil)
	}

	return r.lifecycleManager.Reconcile(ctx, req, crd)
}

func (r *StandardReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiextensionsv1.CustomResourceDefinition{}).
		Named("standard-reconciler").
		Complete(r)
}
