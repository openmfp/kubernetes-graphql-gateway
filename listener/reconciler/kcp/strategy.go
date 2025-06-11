package kcp

import (
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"

	"github.com/openmfp/golang-commons/logger"
	"github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/discoveryclient"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/reconciler"
)

// Strategy implements ReconcilerStrategy for KCP workspace connections
type Strategy struct{}

// NewStrategy creates a new KCP reconciler strategy
func NewStrategy() *Strategy {
	return &Strategy{}
}

// CanHandle returns true if this strategy can handle the given configuration
func (s *Strategy) CanHandle(appCfg config.Config, opts reconciler.ReconcilerOpts, log *logger.Logger) bool {
	// KCP reconciler is used when KCP is explicitly enabled
	return appCfg.EnableKcp
}

// Priority returns the priority of this strategy
func (s *Strategy) Priority() int {
	return reconciler.PriorityKCP
}

// Name returns the name of this strategy
func (s *Strategy) Name() string {
	return "kcp"
}

// CreateReconciler creates a KCP reconciler instance
func (s *Strategy) CreateReconciler(
	opts reconciler.ReconcilerOpts,
	restCfg *rest.Config,
	discoveryInterface discovery.DiscoveryInterface,
	discoverFactory func(cfg *rest.Config) (*discoveryclient.FactoryProvider, error),
	log *logger.Logger,
) (reconciler.CustomReconciler, error) {
	log.Info().Msg("Using KCP reconciler with workspace discovery")
	return NewReconciler(opts, restCfg, discoverFactory, log)
}
