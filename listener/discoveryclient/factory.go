package discoveryclient

import (
	"errors"
	"fmt"
	"net/url"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
)

type clientFactory func(cfg *rest.Config) (discovery.DiscoveryInterface, error)

func discoveryCltFactory(cfg *rest.Config) (discovery.DiscoveryInterface, error) {
	return discovery.NewDiscoveryClientForConfig(cfg)
}

type Factory struct {
	restCfg *rest.Config
	clientFactory
}

func NewFactory(cfg *rest.Config) (*Factory, error) {
	if cfg == nil {
		return nil, errors.New("config should not be nil")
	}
	return &Factory{
		restCfg:       cfg,
		clientFactory: discoveryCltFactory,
	}, nil
}

func (h *Factory) ClientForCluster(name string) (discovery.DiscoveryInterface, error) {
	clusterCfg, err := getClusterConfig(name, h.restCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster config: %w", err)
	}
	return h.clientFactory(clusterCfg)
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
