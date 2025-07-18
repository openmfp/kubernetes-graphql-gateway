package kcp

import (
	"context"
	"fmt"
	"sync"

	"github.com/openmfp/golang-commons/logger"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/pkg/apischema"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/pkg/workspacefile"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// Virtual workspaces are now fully supported by native discovery clients
// when the URL is properly configured to include /clusters/root prefix.
// No custom wrappers needed!

// VirtualWorkspaceReconciler handles reconciliation of virtual workspaces
type VirtualWorkspaceReconciler struct {
	virtualWSManager  *VirtualWorkspaceManager
	ioHandler         workspacefile.IOHandler
	apiSchemaResolver apischema.Resolver
	log               *logger.Logger
	mu                sync.RWMutex
	currentWorkspaces map[string]VirtualWorkspace
}

// NewVirtualWorkspaceReconciler creates a new virtual workspace reconciler
func NewVirtualWorkspaceReconciler(
	virtualWSManager *VirtualWorkspaceManager,
	ioHandler workspacefile.IOHandler,
	apiSchemaResolver apischema.Resolver,
	log *logger.Logger,
) *VirtualWorkspaceReconciler {
	return &VirtualWorkspaceReconciler{
		virtualWSManager:  virtualWSManager,
		ioHandler:         ioHandler,
		apiSchemaResolver: apiSchemaResolver,
		log:               log,
		currentWorkspaces: make(map[string]VirtualWorkspace),
	}
}

// ReconcileConfig processes a virtual workspaces configuration update
func (r *VirtualWorkspaceReconciler) ReconcileConfig(ctx context.Context, config *VirtualWorkspacesConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.log.Info().Int("count", len(config.VirtualWorkspaces)).Msg("reconciling virtual workspaces")

	// Track new workspaces for comparison
	newWorkspaces := make(map[string]VirtualWorkspace)
	for _, ws := range config.VirtualWorkspaces {
		newWorkspaces[ws.Name] = ws
	}

	// Process new or updated workspaces
	for name, workspace := range newWorkspaces {
		if current, exists := r.currentWorkspaces[name]; !exists || current.URL != workspace.URL {
			r.log.Info().Str("workspace", name).Str("url", workspace.URL).Msg("processing virtual workspace")

			if err := r.processVirtualWorkspace(ctx, workspace); err != nil {
				r.log.Error().Err(err).Str("workspace", name).Msg("failed to process virtual workspace")
				continue
			}
		}
	}

	// Remove deleted workspaces
	for name := range r.currentWorkspaces {
		if _, exists := newWorkspaces[name]; !exists {
			r.log.Info().Str("workspace", name).Msg("removing deleted virtual workspace")
			if err := r.removeVirtualWorkspace(name); err != nil {
				r.log.Error().Err(err).Str("workspace", name).Msg("failed to remove virtual workspace")
			}
		}
	}

	// Update current workspaces
	r.currentWorkspaces = newWorkspaces

	r.log.Info().Msg("completed virtual workspaces reconciliation")
	return nil
}

// processVirtualWorkspace generates schema for a single virtual workspace
func (r *VirtualWorkspaceReconciler) processVirtualWorkspace(ctx context.Context, workspace VirtualWorkspace) error {
	workspacePath := r.virtualWSManager.GetWorkspacePath(workspace)

	r.log.Info().
		Str("workspace", workspace.Name).
		Str("url", workspace.URL).
		Str("path", workspacePath).
		Msg("generating schema for virtual workspace")

	// Create discovery client for the virtual workspace
	discoveryClient, err := r.virtualWSManager.CreateDiscoveryClient(workspace)
	if err != nil {
		return fmt.Errorf("failed to create discovery client: %w", err)
	}

	r.log.Debug().Str("workspace", workspace.Name).Str("url", workspace.URL).Msg("created discovery client for virtual workspace")

	// Create REST config and mapper for the virtual workspace
	virtualConfig, err := r.virtualWSManager.CreateRESTConfig(workspace)
	if err != nil {
		return fmt.Errorf("failed to create REST config: %w", err)
	}

	httpClient, err := rest.HTTPClientFor(virtualConfig)
	if err != nil {
		return fmt.Errorf("failed to create HTTP client for virtual workspace: %w", err)
	}

	restMapper, err := apiutil.NewDynamicRESTMapper(virtualConfig, httpClient)
	if err != nil {
		return fmt.Errorf("failed to create REST mapper for virtual workspace: %w", err)
	}

	// Use the native discovery client directly since URL now includes /clusters/root
	r.log.Debug().Str("workspace", workspace.Name).Msg("starting API schema resolution with native discovery client")
	schemaJSON, err := r.apiSchemaResolver.Resolve(discoveryClient, restMapper)
	if err != nil {
		return fmt.Errorf("failed to resolve API schema: %w", err)
	}
	r.log.Debug().Str("workspace", workspace.Name).Int("schemaSize", len(schemaJSON)).Msg("API schema resolved")

	// Inject KCP cluster metadata into the schema, using virtual workspace URL as host
	schemaWithMetadata, err := injectKCPClusterMetadata(schemaJSON, workspacePath, r.log, workspace.URL)
	if err != nil {
		return fmt.Errorf("failed to inject KCP cluster metadata: %w", err)
	}

	// Write the schema to file
	if err := r.ioHandler.Write(schemaWithMetadata, workspacePath); err != nil {
		return fmt.Errorf("failed to write schema file: %w", err)
	}

	r.log.Info().
		Str("workspace", workspace.Name).
		Str("path", workspacePath).
		Int("schemaSize", len(schemaJSON)).
		Msg("successfully generated schema for virtual workspace")

	return nil
}

// removeVirtualWorkspace removes the schema file for a deleted virtual workspace
func (r *VirtualWorkspaceReconciler) removeVirtualWorkspace(name string) error {
	workspace := VirtualWorkspace{Name: name} // Create minimal workspace for path generation
	workspacePath := r.virtualWSManager.GetWorkspacePath(workspace)

	if err := r.ioHandler.Delete(workspacePath); err != nil {
		return fmt.Errorf("failed to delete schema file for workspace %s: %w", name, err)
	}

	r.log.Info().Str("workspace", name).Str("path", workspacePath).Msg("removed schema file for virtual workspace")
	return nil
}
