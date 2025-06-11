package clusteraccess

import (
	"context"
	"errors"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openmfp/golang-commons/logger"
	gatewayv1alpha1 "github.com/openmfp/kubernetes-graphql-gateway/common/apis/gateway/v1alpha1"
	"github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/discoveryclient"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/reconciler"
)

// CRDStatus represents the status of ClusterAccess CRD
type CRDStatus int

const (
	CRDNotRegistered CRDStatus = iota
	CRDRegisteredNoObjects
	CRDRegisteredWithObjects
)

// Strategy implements ReconcilerStrategy for ClusterAccess multi-cluster connections
type Strategy struct{}

// NewStrategy creates a new ClusterAccess reconciler strategy
func NewStrategy() *Strategy {
	return &Strategy{}
}

// CanHandle returns true if this strategy can handle the given configuration
func (s *Strategy) CanHandle(appCfg config.Config, opts reconciler.ReconcilerOpts, log *logger.Logger) bool {
	// ClusterAccess is used in production mode when ClusterAccess CRD is available
	if appCfg.EnableKcp || appCfg.LocalDevelopment {
		return false
	}

	// Check ClusterAccess CRD availability
	crdStatus := s.checkClusterAccessCRDStatus(opts.Client, log)
	return crdStatus != CRDNotRegistered
}

// Priority returns the priority of this strategy
func (s *Strategy) Priority() int {
	return reconciler.PriorityClusterAccess
}

// Name returns the name of this strategy
func (s *Strategy) Name() string {
	return "clusteraccess"
}

// CreateReconciler creates a ClusterAccess reconciler instance
func (s *Strategy) CreateReconciler(
	opts reconciler.ReconcilerOpts,
	restCfg *rest.Config,
	discoveryInterface discovery.DiscoveryInterface,
	discoverFactory func(cfg *rest.Config) (*discoveryclient.FactoryProvider, error),
	log *logger.Logger,
) (reconciler.CustomReconciler, error) {
	crdStatus := s.checkClusterAccessCRDStatus(opts.Client, log)
	switch crdStatus {
	case CRDNotRegistered:
		return nil, errors.New("ClusterAccess CRD is not registered - cannot proceed in production mode without ClusterAccess support")
	case CRDRegisteredNoObjects:
		log.Info().Msg("Using ClusterAccess reconciler - waiting for ClusterAccess objects to be created")
	case CRDRegisteredWithObjects:
		log.Info().Msg("Using ClusterAccess reconciler")
	}

	return NewReconciler(opts, discoveryInterface, log)
}

// checkClusterAccessCRDStatus checks the availability and usage of ClusterAccess CRD
func (s *Strategy) checkClusterAccessCRDStatus(k8sClient client.Client, log *logger.Logger) CRDStatus {
	ctx := context.Background()
	clusterAccessList := &gatewayv1alpha1.ClusterAccessList{}

	if err := k8sClient.List(ctx, clusterAccessList); err != nil {
		log.Info().Err(err).Msg("ClusterAccess CRD not registered")
		return CRDNotRegistered
	}

	// CRD is registered, check if there are any objects
	if len(clusterAccessList.Items) == 0 {
		log.Info().Msg("ClusterAccess CRD registered but no objects found")
		return CRDRegisteredNoObjects
	}

	log.Info().Int("count", len(clusterAccessList.Items)).Msg("ClusterAccess CRD registered with objects")
	return CRDRegisteredWithObjects
}
