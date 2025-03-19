package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"io/fs"

	"github.com/openmfp/kubernetes-graphql-gateway/listener/apischema"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/clusterpath"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/discoveryclient"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/workspacefile"

	kcpapis "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	kcptenancy "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// APIBindingReconciler reconciles both APIBinding and Workspace objects
type APIBindingReconciler struct {
	io *workspacefile.IOHandler
	df *discoveryclient.Factory
	sc apischema.Resolver
	pr *clusterpath.Resolver
}

func NewAPIBindingReconciler(
	io *workspacefile.IOHandler,
	df *discoveryclient.Factory,
	sc apischema.Resolver,
	pr *clusterpath.Resolver,
) *APIBindingReconciler {
	return &APIBindingReconciler{
		io: io,
		df: df,
		sc: sc,
		pr: pr,
	}
}

func (r *APIBindingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("cluster", req.ClusterName, "name", req.NamespacedName.Name)
	logger.Info("starting reconciliation...")

	// Ignore system workspaces (e.g., system:shard)
	if strings.HasPrefix(req.ClusterName, "system") {
		return ctrl.Result{}, nil
	}

	clusterClt, err := r.pr.ClientForCluster(req.ClusterName)
	if err != nil {
		logger.Error(err, "failed to get cluster client")
		return ctrl.Result{}, err
	}

	clusterPath, err := clusterpath.PathForCluster(req.ClusterName, req.NamespacedName.Name, clusterClt)
	if err != nil {
		logger.Error(err, "failed to get cluster path")
		return ctrl.Result{}, err
	}
	logger.Info("resolved cluster path", "path", clusterPath)

	dc, err := r.df.ClientForCluster(clusterPath)
	if err != nil {
		logger.Error(err, "failed to create discovery client for cluster")
		return ctrl.Result{}, err
	}

	rm, err := r.df.RestMapperForCluster(clusterPath)
	if err != nil {
		logger.Error(err, "failed to create rest mapper for cluster")
		return ctrl.Result{}, err
	}

	savedJSON, err := r.io.Read(clusterPath)
	if errors.Is(err, fs.ErrNotExist) {
		actualJSON, err1 := r.sc.Resolve(dc, rm)
		if err1 != nil {
			logger.Error(err1, "failed to resolve server JSON schema")
			return ctrl.Result{}, err1
		}
		if err := r.io.Write(actualJSON, clusterPath); err != nil {
			logger.Error(err, "failed to write JSON to filesystem")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if err != nil {
		logger.Error(err, "failed to read JSON from filesystem")
		return ctrl.Result{}, err
	}

	actualJSON, err := r.sc.Resolve(dc, rm)
	if err != nil {
		logger.Error(err, "failed to resolve server JSON schema")
		return ctrl.Result{}, err
	}
	if !bytes.Equal(actualJSON, savedJSON) {
		if err := r.io.Write(actualJSON, clusterPath); err != nil {
			logger.Error(err, "failed to write JSON to filesystem")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
// managerType specifies whether this is for "apibinding" (APIBinding) or "workspace" (Workspace).
func (r *APIBindingReconciler) SetupWithManager(mgr ctrl.Manager, managerType string) error {
	switch managerType {
	case "apibinding":
		return ctrl.NewControllerManagedBy(mgr).
			For(&kcpapis.APIBinding{}).
			Named("apibinding").
			Complete(r)
	case "workspace":
		return ctrl.NewControllerManagedBy(mgr).
			For(&kcptenancy.Workspace{}).
			Named("workspace").
			Complete(r)
	default:
		return fmt.Errorf("invalid managerType: %s; must be 'apibinding' or 'workspace'", managerType)
	}
}
