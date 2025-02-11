package controller

import (
	"context"

	"github.com/openmfp/crd-gql-gateway/listener/apischema"
	"github.com/openmfp/crd-gql-gateway/listener/workspacefile"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// CRDReconciler reconciles a CustomResourceDefinition object
type CRDReconciler struct {
	ClusterName string
	client.Client
	*apischema.CRDResolver
	io workspacefile.IOHandler
}

func NewCRDReconciler(name string,
	clt client.Client,
	cr *apischema.CRDResolver,
	io workspacefile.IOHandler,
) *CRDReconciler {
	return &CRDReconciler{
		ClusterName: name,
		Client:      clt,
		CRDResolver: cr,
		io:          io,
	}
}

// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinition,verbs=get;list;watch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinition/status,verbs=get
func (r *CRDReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	logger := log.FromContext(ctx).WithValues("cluster", r.ClusterName).WithName(req.Name)
	logger.Info("starting reconciliation...")

	crd := &apiextensionsv1.CustomResourceDefinition{}
	err := r.Client.Get(ctx, req.NamespacedName, crd)
	if apierrors.IsNotFound(err) {
		logger.Info("resource not found, updating schema...")
		return ctrl.Result{}, r.updateAPISchema()
	}
	if client.IgnoreNotFound(err) != nil {
		logger.Error(err, "failed to get reconciled object")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, r.updateAPISchemaWith(crd)
}

// SetupWithManager sets up the controller with the Manager.
func (r *CRDReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiextensionsv1.CustomResourceDefinition{}).
		Named("CRD").
		Complete(r)
}
