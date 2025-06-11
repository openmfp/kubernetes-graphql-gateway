package standard

import (
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"

	"github.com/openmfp/golang-commons/logger"
	"github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/discoveryclient"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/reconciler"
)

// Strategy implements ReconcilerStrategy for standard (direct) cluster connections
type Strategy struct{}

// NewStrategy creates a new standard reconciler strategy
func NewStrategy() *Strategy {
	return &Strategy{}
}

// CanHandle returns true if this strategy can handle the given configuration
func (s *Strategy) CanHandle(appCfg config.Config, opts reconciler.ReconcilerOpts, log *logger.Logger) bool {
	// Standard reconciler is used for local development mode
	return appCfg.LocalDevelopment
}

// Priority returns the priority of this strategy
func (s *Strategy) Priority() int {
	return reconciler.PriorityStandard
}

// Name returns the name of this strategy
func (s *Strategy) Name() string {
	return "standard"
}

// CreateReconciler creates a standard reconciler instance
func (s *Strategy) CreateReconciler(
	opts reconciler.ReconcilerOpts,
	restCfg *rest.Config,
	discoveryInterface discovery.DiscoveryInterface,
	discoverFactory func(cfg *rest.Config) (*discoveryclient.FactoryProvider, error),
	log *logger.Logger,
) (reconciler.CustomReconciler, error) {
	log.Info().Msg("Using standard reconciler for local development")
	return NewReconciler(opts, discoveryInterface, log)
}
