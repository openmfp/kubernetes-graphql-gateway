package kcp

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/apischema"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/clusterpath"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/controller"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/discoveryclient"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/workspacefile"
)

const kubernetesClusterName = "kubernetes"

type CustomReconciler interface {
	Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error)
	SetupWithManager(mgr ctrl.Manager) error
}

type ReconcilerOpts struct {
	*rest.Config
	*runtime.Scheme
	client.Client
	OpenAPIDefinitionsPath string
}

type NewDiscoveryFactoryFunc func(cfg *rest.Config) (*discoveryclient.Factory, error)

type PreReconcileFunc func(cr *apischema.CRDResolver, io *workspacefile.IOHandler) error

type NewDiscoveryIFFunc func(cfg *rest.Config) (discovery.DiscoveryInterface, error)

func DiscoveryCltFactory(cfg *rest.Config) (discovery.DiscoveryInterface, error) {
	return discovery.NewDiscoveryClientForConfig(cfg)
}

type ReconcilerFactory struct {
	AppCfg *config.Config
	NewDiscoveryIFFunc
	PreReconcileFunc
	NewDiscoveryFactoryFunc
}

func NewReconcilerFactory(
	appCfg *config.Config,
	newDiscoveryIFFunc func(cfg *rest.Config) (discovery.DiscoveryInterface, error),
	preReconcileFunc func(cr *apischema.CRDResolver, io *workspacefile.IOHandler) error,
	newDiscoveryFactoryFunc func(cfg *rest.Config) (*discoveryclient.Factory, error),
) *ReconcilerFactory {
	return &ReconcilerFactory{
		AppCfg:                  appCfg,
		NewDiscoveryIFFunc:      newDiscoveryIFFunc,
		PreReconcileFunc:        preReconcileFunc,
		NewDiscoveryFactoryFunc: newDiscoveryFactoryFunc,
	}
}

func (f *ReconcilerFactory) NewReconciler(opts ReconcilerOpts) (CustomReconciler, error) {
	if !f.AppCfg.EnableKcp {
		return f.newStdReconciler(opts)
	}
	return f.newKcpReconciler(opts)
}

func (f *ReconcilerFactory) newStdReconciler(opts ReconcilerOpts) (CustomReconciler, error) {
	dc, err := f.NewDiscoveryIFFunc(opts.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery client: %w", err)
	}

	ioHandler, err := workspacefile.NewIOHandler(opts.OpenAPIDefinitionsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create IO Handler: %w", err)
	}

	rm, err := restMapperFromConfig(opts.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create rest mapper from config: %w", err)
	}

	schemaResolver := &apischema.CRDResolver{
		DiscoveryInterface: dc,
		RESTMapper:         rm,
	}

	if err := f.PreReconcileFunc(schemaResolver, ioHandler); err != nil {
		return nil, fmt.Errorf("failed to generate OpenAPI Schema for cluster: %w", err)
	}

	return controller.NewCRDReconciler(kubernetesClusterName, opts.Client, schemaResolver, ioHandler), nil

}

func restMapperFromConfig(cfg *rest.Config) (meta.RESTMapper, error) {
	httpClt, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create http client: %w", err)
	}
	rm, err := apiutil.NewDynamicRESTMapper(cfg, httpClt)
	if err != nil {
		return nil, fmt.Errorf("failed to create rest mapper: %w", err)
	}
	return rm, nil
}

func PreReconcile(
	cr *apischema.CRDResolver,
	io *workspacefile.IOHandler,
) error {
	JSON, err := cr.Resolve()
	if err != nil {
		return fmt.Errorf("failed to resolve server JSON schema: %w", err)
	}
	if err := io.Write(JSON, kubernetesClusterName); err != nil {
		return fmt.Errorf("failed to write JSON to filesystem: %w", err)
	}
	return nil
}

func (f *ReconcilerFactory) newKcpReconciler(opts ReconcilerOpts) (CustomReconciler, error) {
	ioHandler, err := workspacefile.NewIOHandler(opts.OpenAPIDefinitionsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create IO Handler: %w", err)
	}

	pr, err := clusterpath.NewResolver(opts.Config, opts.Scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to create cluster path resolver: %w", err)
	}

	virtualWorkspaceCfg, err := virtualWorkspaceConfigFromCfg(f.AppCfg, opts.Config, opts.Client)
	if err != nil {
		return nil, fmt.Errorf("unable to get virtual workspace config: %w", err)
	}

	df, err := f.NewDiscoveryFactoryFunc(virtualWorkspaceCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Discovery client factory: %w", err)
	}

	return controller.NewAPIBindingReconciler(
		ioHandler, df, apischema.NewResolver(), pr,
	), nil
}
