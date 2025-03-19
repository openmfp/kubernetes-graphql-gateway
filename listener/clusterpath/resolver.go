package clusterpath

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	kcpapis "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	kcpcore "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	kcptenancy "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	errNilConfig = errors.New("config should not be nil")
	errNilScheme = errors.New("scheme should not be nil")
)

type clientFactory func(config *rest.Config, options client.Options) (client.Client, error)

type Resolver struct {
	*runtime.Scheme
	*rest.Config
	clientFactory
}

func NewResolver(cfg *rest.Config, scheme *runtime.Scheme) (*Resolver, error) {
	if cfg == nil {
		return nil, errNilConfig
	}
	if scheme == nil {
		return nil, errNilScheme
	}
	return &Resolver{
		Scheme:        scheme,
		Config:        cfg,
		clientFactory: client.New,
	}, nil
}

func (rf *Resolver) ClientForCluster(name string) (client.Client, error) {
	clusterConfig, err := getClusterConfig(name, rf.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster config: %w", err)
	}
	return rf.clientFactory(clusterConfig, client.Options{Scheme: rf.Scheme})
}

// PathForCluster resolves the full workspace path (e.g., root:alpha:beta:delta) for a given cluster and resource name.
func PathForCluster(clusterName, resourceName string, clt client.Client) (string, error) {
	logger := log.Log.WithName("clusterpath")

	// Resolve clusterName to its logical path first
	basePath, err := resolveClusterName(clusterName, clt)
	if err != nil {
		logger.Error(err, "failed to resolve clusterName", "clusterName", clusterName)
		return "", err
	}

	// If no resourceName or it’s the same as clusterName, return the resolved base path
	if resourceName == "" || resourceName == clusterName || resourceName == "root" {
		logger.V(4).Info("resolved base path", "clusterName", clusterName, "path", basePath)
		return basePath, nil
	}

	// Handle Workspace events
	ws := &kcptenancy.Workspace{}
	if err := clt.Get(context.TODO(), client.ObjectKey{Name: resourceName}, ws); err == nil {
		fullPath, err := getFullWorkspacePath(ws, clt, basePath)
		if err != nil {
			return "", err
		}
		logger.V(4).Info("resolved full path for Workspace", "resourceName", resourceName, "path", fullPath)
		return fullPath, nil
	} else {
		logger.V(4).Info("resource is not a Workspace", "resourceName", resourceName, "error", err)
	}

	// Handle APIBinding events
	apiBinding := &kcpapis.APIBinding{}
	if err := clt.Get(context.TODO(), client.ObjectKey{Name: resourceName}, apiBinding); err == nil {
		// For APIBinding, use the resolved basePath (cluster context)
		logger.V(4).Info("resolved path for APIBinding", "resourceName", resourceName, "path", basePath)
		return basePath, nil
	}

	// If neither Workspace nor APIBinding, append resourceName to basePath as a fallback
	fullPath := fmt.Sprintf("%s:%s", basePath, resourceName)
	logger.V(4).Info("fallback path resolution", "clusterName", clusterName, "resourceName", resourceName, "path", fullPath)
	return fullPath, nil
}

// getFullWorkspacePath constructs the full path by traversing Workspace OwnerReferences
func getFullWorkspacePath(ws *kcptenancy.Workspace, clt client.Client, basePath string) (string, error) {
	logger := log.Log.WithName("clusterpath")

	// Check if kcp.io/path is available and valid
	if path, ok := ws.GetAnnotations()["kcp.io/path"]; ok && path != "" && strings.HasPrefix(path, "root") {
		logger.V(4).Info("resolved path from Workspace annotation", "resourceName", ws.Name, "path", path)
		return path, nil
	}

	// Build path by traversing OwnerReferences
	pathParts := []string{ws.Name}
	current := ws
	for {
		owners := current.GetOwnerReferences()
		if len(owners) == 0 {
			break
		}
		var parentName string
		for _, owner := range owners {
			if owner.Kind == "Workspace" {
				parentName = owner.Name
				break
			}
		}
		if parentName == "" {
			break
		}
		parent := &kcptenancy.Workspace{}
		if err := clt.Get(context.TODO(), client.ObjectKey{Name: parentName}, parent); err != nil {
			logger.Error(err, "failed to get parent Workspace", "parentName", parentName)
			break
		}
		pathParts = append([]string{parent.Name}, pathParts...)
		current = parent
	}

	// Construct full path
	fullPath := strings.Join(pathParts, ":")
	if !strings.HasPrefix(fullPath, "root") {
		// Prepend resolved basePath (e.g., "root:beta" or "root")
		fullPath = fmt.Sprintf("%s:%s", basePath, fullPath)
	}

	logger.V(4).Info("constructed full path from Workspace hierarchy", "resourceName", ws.Name, "path", fullPath)
	return fullPath, nil
}

// resolveClusterName resolves a clusterName (e.g., hash or "root") to its full logical path
func resolveClusterName(clusterName string, clt client.Client) (string, error) {
	logger := log.Log.WithName("clusterpath")

	if clusterName == "root" || strings.HasPrefix(clusterName, "root:") {
		return clusterName, nil
	}

	// Assume clusterName might be a hash; resolve via LogicalCluster
	lc := &kcpcore.LogicalCluster{}
	if err := clt.Get(context.TODO(), client.ObjectKey{Name: "cluster"}, lc); err != nil {
		logger.V(4).Info("clusterName not found as LogicalCluster, assuming it’s a base path", "clusterName", clusterName, "error", err)
		return clusterName, nil // Fallback to clusterName if resolution fails
	}

	path, ok := lc.GetAnnotations()["kcp.io/path"]
	if !ok || path == "" {
		logger.Info("no kcp.io/path annotation found, defaulting to clusterName", "clusterName", clusterName)
		return clusterName, nil
	}

	logger.V(4).Info("resolved path from LogicalCluster", "clusterName", clusterName, "path", path)
	return path, nil
}

func getClusterConfig(name string, cfg *rest.Config) (*rest.Config, error) {
	if cfg == nil {
		return nil, errors.New("config should not be nil")
	}
	clusterCfg := rest.CopyConfig(cfg)
	clusterCfgURL, err := url.Parse(clusterCfg.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to parse rest config's Host URL: %w", err)
	}
	clusterCfgURL.Path = fmt.Sprintf("/clusters/%s", name)
	clusterCfg.Host = clusterCfgURL.String()
	return clusterCfg, nil
}
