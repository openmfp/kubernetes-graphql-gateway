package clusteraccess

import (
	"context"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openmfp/golang-commons/controller/lifecycle"
	"github.com/openmfp/golang-commons/logger"
	gatewayv1alpha1 "github.com/openmfp/kubernetes-graphql-gateway/common/apis/v1alpha1"
	"github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/apischema"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/reconciler/types"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/workspacefile"
)

var (
	ErrCreateIOHandler  = errors.New("failed to create IO Handler")
	ErrCreateRESTMapper = errors.New("failed to create REST mapper")
	ErrCreateHTTPClient = errors.New("failed to create HTTP client")
	ErrGenerateSchema   = errors.New("failed to generate schema")
	ErrCRDNotRegistered = errors.New("ClusterAccess CRD not registered")
	ErrCRDCheckFailed   = errors.New("failed to check ClusterAccess CRD status")
)

// CRDStatus represents the status of ClusterAccess CRD
type CRDStatus int

const (
	CRDNotRegistered CRDStatus = iota
	CRDRegistered
)

// CreateMultiClusterReconciler creates a multi-cluster reconciler using ClusterAccess CRDs
func CreateMultiClusterReconciler(
	appCfg config.Config,
	opts types.ReconcilerOpts,
	restCfg *rest.Config,
	log *logger.Logger,
) (types.CustomReconciler, error) {
	log.Info().Msg("Using multi-cluster reconciler")

	// Check if ClusterAccess CRD is available
	caStatus, err := CheckClusterAccessCRDStatus(opts.Client, log)
	if err != nil {
		if errors.Is(err, ErrCRDNotRegistered) {
			log.Error().Msg("Multi-cluster mode enabled but ClusterAccess CRD not registered")
			return nil, errors.New("multi-cluster mode enabled but ClusterAccess CRD not registered")
		}
		log.Error().Err(err).Msg("Multi-cluster mode enabled but failed to check ClusterAccess CRD status")
		return nil, err
	}

	if caStatus != CRDRegistered {
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
	return NewReconciler(opts, restCfg, ioHandler, schemaResolver, log)
}

// CheckClusterAccessCRDStatus checks the availability and usage of ClusterAccess CRD
func CheckClusterAccessCRDStatus(k8sClient client.Client, log *logger.Logger) (CRDStatus, error) {
	ctx := context.Background()
	clusterAccessList := &gatewayv1alpha1.ClusterAccessList{}

	err := k8sClient.List(ctx, clusterAccessList)
	if err != nil {
		if meta.IsNoMatchError(err) || errors.Is(err, &meta.NoResourceMatchError{}) {
			log.Info().Err(err).Msg("ClusterAccess CRD not registered")
			return CRDNotRegistered, ErrCRDNotRegistered
		}
		log.Error().Err(err).Msg("Error checking ClusterAccess CRD status")
		return CRDNotRegistered, fmt.Errorf("%w: %v", ErrCRDCheckFailed, err)
	}

	log.Info().Int("count", len(clusterAccessList.Items)).Msg("ClusterAccess CRD registered")
	return CRDRegistered, nil
}

// ClusterAccessReconciler handles reconciliation for ClusterAccess resources
type ClusterAccessReconciler struct {
	lifecycleManager *lifecycle.LifecycleManager
	opts             types.ReconcilerOpts
	restCfg          *rest.Config
	ioHandler        workspacefile.IOHandler
	schemaResolver   apischema.Resolver
	log              *logger.Logger
}

func NewReconciler(
	opts types.ReconcilerOpts,
	restCfg *rest.Config,
	ioHandler workspacefile.IOHandler,
	schemaResolver apischema.Resolver,
	log *logger.Logger,
) (types.CustomReconciler, error) {
	r := &ClusterAccessReconciler{
		opts:           opts,
		restCfg:        restCfg,
		ioHandler:      ioHandler,
		schemaResolver: schemaResolver,
		log:            log,
	}

	// Create lifecycle manager with subroutines
	r.lifecycleManager = lifecycle.NewLifecycleManager(
		log,
		"cluster-access-reconciler",
		"cluster-access-reconciler",
		opts.Client,
		[]lifecycle.Subroutine{
			&generateSchemaSubroutine{reconciler: r},
		},
	)

	return r, nil
}

func (r *ClusterAccessReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Fetch the ClusterAccess resource
	clusterAccess := &gatewayv1alpha1.ClusterAccess{}
	if err := r.opts.Client.Get(ctx, req.NamespacedName, clusterAccess); err != nil {
		if client.IgnoreNotFound(err) == nil {
			r.log.Info().Str("clusterAccess", req.Name).Msg("ClusterAccess resource not found, might have been deleted")
			return ctrl.Result{}, nil
		}
		r.log.Error().Err(err).Str("clusterAccess", req.Name).Msg("failed to fetch ClusterAccess resource")
		return ctrl.Result{}, err
	}

	return r.lifecycleManager.Reconcile(ctx, req, clusterAccess)
}

func (r *ClusterAccessReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1alpha1.ClusterAccess{}).
		Complete(r)
}
