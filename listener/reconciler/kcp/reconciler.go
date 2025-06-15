package kcp

import (
	"errors"

	"k8s.io/client-go/rest"

	"github.com/openmfp/golang-commons/logger"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/apischema"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/clusterpath"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/controller"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/discoveryclient"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/reconciler"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/workspacefile"
)

var (
	ErrCreateIOHandler       = errors.New("failed to create IO Handler")
	ErrCreatePathResolver    = errors.New("failed to create cluster path resolver")
	ErrCreateDiscoveryClient = errors.New("failed to create discovery client")
)

func NewReconciler(
	opts reconciler.ReconcilerOpts,
	restcfg *rest.Config,
	newDiscoveryFactoryFunc func(cfg *rest.Config) (*discoveryclient.FactoryProvider, error),
	log *logger.Logger,
) (reconciler.CustomReconciler, error) {
	ioHandler, err := workspacefile.NewIOHandler(opts.OpenAPIDefinitionsPath)
	if err != nil {
		return nil, errors.Join(ErrCreateIOHandler, err)
	}

	pr, err := clusterpath.NewResolver(opts.Config, opts.Scheme)
	if err != nil {
		return nil, errors.Join(ErrCreatePathResolver, err)
	}

	df, err := newDiscoveryFactoryFunc(restcfg)
	if err != nil {
		return nil, errors.Join(ErrCreateDiscoveryClient, err)
	}

	return controller.NewAPIBindingReconciler(
		ioHandler, df, apischema.NewResolver(), pr, log,
	), nil
}
