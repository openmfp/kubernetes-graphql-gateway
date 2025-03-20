package kcp

import (
	"context"
	"fmt"
	"time"

	kcpapis "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openmfp/kubernetes-graphql-gateway/common/config"
)

func virtualWorkspaceConfigFromCfg(appCfg *config.Config, restCfg *rest.Config, clt client.Client) (*rest.Config, error) {
	key := client.ObjectKey{
		Namespace: appCfg.ApiExportWorkspace,
		Name:      appCfg.ApiExportName,
	}

	var apiExport kcpapis.APIExport
	ctx, cancelFn := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFn()

	if err := clt.Get(ctx, key, &apiExport); err != nil {
		return nil, fmt.Errorf("failed to get %s APIExport in %s workspace: %v ", appCfg.ApiExportName, appCfg.ApiExportWorkspace, err)
	}

	if len(apiExport.Status.VirtualWorkspaces) == 0 { // nolint: staticcheck
		return nil, fmt.Errorf("no virtual URLs found for APIExport %s in %s", appCfg.ApiExportName, appCfg.ApiExportWorkspace)
	}

	virtualWorkspaceURL := apiExport.Status.VirtualWorkspaces[0].URL // nolint: staticcheck
	if virtualWorkspaceURL == "" {
		return nil, fmt.Errorf("empty URL in virtual workspace for APIExport %s", appCfg.ApiExportName)
	}

	restCfg.Host = virtualWorkspaceURL

	return restCfg, nil
}
