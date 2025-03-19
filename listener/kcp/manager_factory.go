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
func (f *ManagerFactory) NewManagers(cfg *rest.Config, rootMgrOpts, vwMgrOpts ctrl.Options, clt client.Client) (mgr manager.Manager, wsMgr manager.Manager, err error) {
	if !f.IsKCPEnabled {
		mgr, err = ctrl.NewManager(cfg, rootMgrOpts)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to create root manager: %w", err)
		}
		return mgr, nil, nil
	}

	// Create the root manager for KCP
	mgr, err = kcpctrl.NewClusterAwareManager(cfg, rootMgrOpts)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to create root manager: %w", err)
	}

	// Create the virtual workspace manager for KCP
	virtualWorkspaceCfg, err := virtualWorkspaceConfigFromCfg(cfg, clt)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to get virtual workspace config: %w", err)
	}
	wsMgr, err = kcpctrl.NewClusterAwareManager(virtualWorkspaceCfg, vwMgrOpts)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to create virtual workspace manager: %w", err)
	}

	return mgr, wsMgr, nil
}
