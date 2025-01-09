package controller

import (
	"bytes"
	"context"
	"errors"

	//"fmt"
	"io/fs"

	"github.com/openmfp/crd-gql-gateway/kcp-listener/internal/apischema"
	"github.com/openmfp/crd-gql-gateway/kcp-listener/internal/discoveryclient"
	"github.com/openmfp/crd-gql-gateway/kcp-listener/internal/workspacefile"

	//"k8s.io/apimachinery/pkg/fields"

	kcpapis "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	kcptenancy "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"

	//apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// APIBindingReconciler reconciles an APIBinding object
type APIBindingReconciler struct {
	//clt client.Client
	io workspacefile.IOHandler
	df discoveryclient.Factory
	sc apischema.Resolver
}

func NewAPIBindingReconciler(
	//clt client.Client,
	io workspacefile.IOHandler,
	df discoveryclient.Factory,
	sc apischema.Resolver,
) *APIBindingReconciler {
	return &APIBindingReconciler{
		//clt: clt,
		io: io,
		df: df,
		sc: sc,
	}
}

// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions/status,verbs=get
// +kubebuilder:rbac:groups=apis.kcp.io,resources=apibindings,verbs=get;list;watch
// +kubebuilder:rbac:groups=apis.kcp.io,resources=apibindings/status,verbs=get
// +kubebuilder:rbac:groups=tenancy.kcp.io,resources=workspaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=tenancy.kcp.io,resources=workspaces/status,verbs=get
func (r *APIBindingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	// needed when you need to get objects from cache in the cluster-aware mode
	//ctx = kontext.WithCluster(ctx, logicalcluster.Name(req.ClusterName))
	logger := log.FromContext(ctx).WithValues("cluster", req.ClusterName)
	logger.Info("starting reconciliation...")

	dc, err := r.df.ClientForCluster(req.ClusterName)
	if err != nil {
		logger.Error(err, "failed to create discovery client for cluster")
		return ctrl.Result{}, err
	}

	savedJSON, err := r.io.Read(req.ClusterName)
	if errors.Is(err, fs.ErrNotExist) {
		actualJSON, err1 := r.sc.Resolve(dc)
		if err1 != nil {
			logger.Error(err1, "failed to resolve server JSON schema")
			return ctrl.Result{}, err1
		}
		if err = r.io.Write(actualJSON, req.ClusterName); err != nil {
			logger.Error(err, "failed to write JSON to filesystem")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if err != nil {
		logger.Error(err, "failed to read JSON from filesystem")
		return ctrl.Result{}, err
	}

	actualJSON, err := r.sc.Resolve(dc)
	if err != nil {
		logger.Error(err, "failed to resolve server JSON schema")
		return ctrl.Result{}, err
	}
	if !bytes.Equal(actualJSON, savedJSON) {
		err = r.io.Write(actualJSON, req.ClusterName)
		if err != nil {
			logger.Error(err, "failed to write JSON to filesystem")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *APIBindingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kcpapis.APIBinding{}).
		Watches(&kcptenancy.Workspace{},
			handler.EnqueueRequestsFromMapFunc(clusterNameFromWorkspace)).
		Named("apibinding").
		Complete(r)
}
