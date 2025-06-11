package reconciler

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openmfp/golang-commons/logger"
	"github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/discoveryclient"
)

// CustomReconciler defines the interface that all reconcilers must implement
type CustomReconciler interface {
	Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error)
	SetupWithManager(mgr ctrl.Manager) error
}

// ReconcilerOpts contains common options needed by all reconciler strategies
type ReconcilerOpts struct {
	*rest.Config
	*runtime.Scheme
	client.Client
	OpenAPIDefinitionsPath string
}

// ReconcilerStrategy defines the interface for reconciler creation strategies
type ReconcilerStrategy interface {
	CreateReconciler(opts ReconcilerOpts, restCfg *rest.Config, discoveryInterface discovery.DiscoveryInterface, discoverFactory func(cfg *rest.Config) (*discoveryclient.FactoryProvider, error), log *logger.Logger) (CustomReconciler, error)
	CanHandle(appCfg config.Config, opts ReconcilerOpts, log *logger.Logger) bool
	Priority() int
	Name() string
}

// StrategyPriority defines priority levels for strategies
const (
	PriorityKCP           = 3 // Highest priority
	PriorityStandard      = 2
	PriorityClusterAccess = 1 // Lowest priority (fallback)
)
