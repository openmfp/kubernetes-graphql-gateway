package kcp

import (
	"errors"
	"fmt"
	"net/url"
	"os"

	"gopkg.in/yaml.v3"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	ErrInvalidVirtualWorkspaceURL = errors.New("invalid virtual workspace URL")
	ErrParseVirtualWorkspaceURL   = errors.New("failed to parse virtual workspace URL")
)

// VirtualWorkspace represents a virtual workspace configuration
type VirtualWorkspace struct {
	Name       string `yaml:"name"`
	URL        string `yaml:"url"`
	Kubeconfig string `yaml:"kubeconfig,omitempty"` // Optional path to kubeconfig for authentication
}

// VirtualWorkspacesConfig represents the configuration file structure
type VirtualWorkspacesConfig struct {
	VirtualWorkspaces []VirtualWorkspace `yaml:"virtualWorkspaces"`
}

// VirtualWorkspaceManager handles virtual workspace operations
type VirtualWorkspaceManager struct {
}

// NewVirtualWorkspaceManager creates a new virtual workspace manager
func NewVirtualWorkspaceManager() *VirtualWorkspaceManager {
	return &VirtualWorkspaceManager{}
}

// GetWorkspacePath returns the file path for storing the virtual workspace schema
func (v *VirtualWorkspaceManager) GetWorkspacePath(workspace VirtualWorkspace) string {
	return fmt.Sprintf("virtual-workspace/%s", workspace.Name)
}

// createVirtualConfig creates a REST config for a virtual workspace
func createVirtualConfig(workspace VirtualWorkspace) (*rest.Config, error) {
	if workspace.URL == "" {
		return nil, fmt.Errorf("%w: empty URL for workspace %s", ErrInvalidVirtualWorkspaceURL, workspace.Name)
	}

	// Parse the virtual workspace URL to validate it
	_, err := url.Parse(workspace.URL)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrParseVirtualWorkspaceURL, err)
	}

	var virtualConfig *rest.Config

	if workspace.Kubeconfig != "" {
		// Load authentication from the specified kubeconfig
		cfg, err := clientcmd.LoadFromFile(workspace.Kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to load kubeconfig %s: %w", workspace.Kubeconfig, err)
		}

		restConfig, err := clientcmd.NewDefaultClientConfig(*cfg, &clientcmd.ConfigOverrides{}).ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to create client config from kubeconfig %s: %w", workspace.Kubeconfig, err)
		}

		virtualConfig = restConfig
		virtualConfig.Host = workspace.URL + "/clusters/root"
	} else {
		// Use minimal configuration for virtual workspaces without authentication
		virtualConfig = &rest.Config{
			Host:      workspace.URL + "/clusters/root",
			UserAgent: "kubernetes-graphql-gateway-listener",
			TLSClientConfig: rest.TLSClientConfig{
				Insecure: true,
			},
		}
	}

	return virtualConfig, nil
}

// CreateDiscoveryClient creates a discovery client for the virtual workspace
func (v *VirtualWorkspaceManager) CreateDiscoveryClient(workspace VirtualWorkspace) (discovery.DiscoveryInterface, error) {
	virtualConfig, err := createVirtualConfig(workspace)
	if err != nil {
		return nil, err
	}

	// Create discovery client
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(virtualConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery client for virtual workspace %s (URL: %s): %w", workspace.Name, workspace.URL, err)
	}

	return discoveryClient, nil
}

// CreateRESTConfig creates a REST config for the virtual workspace (for REST mappers)
func (v *VirtualWorkspaceManager) CreateRESTConfig(workspace VirtualWorkspace) (*rest.Config, error) {
	return createVirtualConfig(workspace)
}

// LoadConfig loads the virtual workspaces configuration from a file
func (v *VirtualWorkspaceManager) LoadConfig(configPath string) (*VirtualWorkspacesConfig, error) {
	if configPath == "" {
		return &VirtualWorkspacesConfig{}, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &VirtualWorkspacesConfig{}, nil
		}
		return nil, fmt.Errorf("failed to read virtual workspaces config file: %w", err)
	}

	var config VirtualWorkspacesConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse virtual workspaces config: %w", err)
	}

	return &config, nil
}
