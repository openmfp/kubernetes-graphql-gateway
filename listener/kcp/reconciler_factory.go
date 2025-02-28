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
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
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

type newDiscoveryFactoryFunc func(cfg *rest.Config) (*discoveryclient.Factory, error)

type preReconcileFunc func(cr *apischema.CRDResolver, io *workspacefile.IOHandler) error

type newDiscoveryIFFunc func(cfg *rest.Config) (discovery.DiscoveryInterface, error)

func discoveryCltFactory(cfg *rest.Config) (discovery.DiscoveryInterface, error) {
	return discovery.NewDiscoveryClientForConfig(cfg)
}

type ReconcilerFactory struct {
	IsKCPEnabled bool
	newDiscoveryIFFunc
	preReconcileFunc
	newDiscoveryFactoryFunc
}

func NewReconcilerFactory(opFlags *flags.Flags) *ReconcilerFactory {
	return &ReconcilerFactory{
		IsKCPEnabled:            opFlags.EnableKcp,
		newDiscoveryIFFunc:      discoveryCltFactory,
		preReconcileFunc:        preReconcile,
		newDiscoveryFactoryFunc: discoveryclient.NewFactory,
	}
}

func (f *ReconcilerFactory) NewReconciler(opts ReconcilerOpts) (CustomReconciler, error) {
	if !f.IsKCPEnabled {
		return f.newStdReconciler(opts)
	}
	return f.newKcpReconciler(opts)
}

func (f *ReconcilerFactory) newStdReconciler(opts ReconcilerOpts) (CustomReconciler, error) {
	dc, err := f.newDiscoveryIFFunc(opts.Config)
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

	if err := f.preReconcileFunc(schemaResolver, ioHandler); err != nil {
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

func preReconcile(
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
	virtualWorkspaceCfg, err := virtualWorkspaceConfigFromCfg(opts.Config, opts.Client)
	if err != nil {
		return nil, fmt.Errorf("unable to get virtual workspace config: %w", err)
	}
	df, err := f.newDiscoveryFactoryFunc(virtualWorkspaceCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Discovery client factory: %w", err)
	}
	return controller.NewAPIBindingReconciler(
		ioHandler, df, apischema.NewResolver(), pr,
	), nil
}
