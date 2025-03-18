package kcp

import (
	"fmt"

	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	kcpctrl "sigs.k8s.io/controller-runtime/pkg/kcp"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type ManagerFactory struct {
	IsKCPEnabled bool
}

// NewManagers returns the root manager and, if KCP is enabled, the virtual workspace manager.
func (f *ManagerFactory) NewManagers(cfg *rest.Config, rootMgrOpts, vwMgrOpts ctrl.Options, clt client.Client) (rootMgr manager.Manager, vwMgr manager.Manager, err error) {
	if !f.IsKCPEnabled {
		rootMgr, err = ctrl.NewManager(cfg, rootMgrOpts)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to create root manager: %w", err)
		}

		return rootMgr, nil, nil
	}

	// Create the root manager for KCP
	rootMgr, err = kcpctrl.NewClusterAwareManager(cfg, rootMgrOpts)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to create root manager: %w", err)
	}

	// Create the virtual workspace manager for KCP
	virtualWorkspaceCfg, err := virtualWorkspaceConfigFromCfg(cfg, clt)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to get virtual workspace config: %w", err)
	}
	vwMgr, err = kcpctrl.NewClusterAwareManager(virtualWorkspaceCfg, vwMgrOpts)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to create virtual workspace manager: %w", err)
	}

	return rootMgr, vwMgr, nil
}
