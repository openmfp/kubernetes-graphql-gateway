package kcp

import (
	"context"
	"fmt"

	"github.com/openmfp/crd-gql-gateway/listener/apischema"
	"github.com/openmfp/crd-gql-gateway/listener/clusterpath"
	"github.com/openmfp/crd-gql-gateway/listener/controller"
	"github.com/openmfp/crd-gql-gateway/listener/discoveryclient"
	"github.com/openmfp/crd-gql-gateway/listener/flags"
	"github.com/openmfp/crd-gql-gateway/listener/workspacefile"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CustomReconciler interface {
	Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error)
	SetupWithManager(mgr ctrl.Manager) error
}

type ReconcilerOpts struct {
	*rest.Config
	*runtime.Scheme
	OpenAPIDefinitionsPath string
}

type NewReconcilerFunc func(opts ReconcilerOpts) (CustomReconciler, error)

func ReconcilerFactory(opFlags *flags.Flags) NewReconcilerFunc {
	if opFlags.EnableKcp {
		return NewKcpReconciler
	}
	return NewReconciler
}

func NewReconciler(opts ReconcilerOpts) (CustomReconciler, error) {
	clt, err := client.New(opts.Config, client.Options{
		Scheme: opts.Scheme,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}
	dc, err := discovery.NewDiscoveryClientForConfig(opts.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery client: %w", err)
	}

	ioHandler, err := workspacefile.NewIOHandler(opts.OpenAPIDefinitionsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create IO Handler: %w", err)
	}

	return controller.NewCRDReconciler("kubernetes", clt, dc, ioHandler, apischema.NewResolver()), nil
}

func NewKcpReconciler(opts ReconcilerOpts) (CustomReconciler, error) {
	virtualWorkspaceCfg, err := virtualWorkspaceConfigFromCfg(opts.Config, opts.Scheme)
	if err != nil {
		return nil, fmt.Errorf("unable to get virtual workspace config: %w", err)
	}
	ioHandler, err := workspacefile.NewIOHandler(opts.OpenAPIDefinitionsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create IO Handler: %w", err)
	}

	df, err := discoveryclient.NewFactory(virtualWorkspaceCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Discovery client factory: %w", err)
	}

	return controller.NewAPIBindingReconciler(
		ioHandler, df, apischema.NewResolver(), &clusterpath.Resolver{
			Scheme:       opts.Scheme,
			Config:       opts.Config,
			ResolverFunc: clusterpath.Resolve,
		},
	), nil
}
