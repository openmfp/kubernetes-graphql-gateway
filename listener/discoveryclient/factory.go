package discoveryclient

import (
	"errors"
	"fmt"
	"net/url"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
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

func (f *Factory) ClientForCluster(name string) (*discovery.DiscoveryClient, error) {
	clusterCfg, err := configForCluster(name, f.restCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get rest config for cluster: %w", err)
	}
	return discovery.NewDiscoveryClientForConfig(clusterCfg)
}

func (f *Factory) RestMapperForCluster(name string) (meta.RESTMapper, error) {
	clusterCfg, err := configForCluster(name, f.restCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get rest config for cluster: %w", err)
	}
	httpClt, err := rest.HTTPClientFor(clusterCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create http client: %w", err)
	}
	return apiutil.NewDynamicRESTMapper(clusterCfg, httpClt)
}

func configForCluster(name string, cfg *rest.Config) (*rest.Config, error) {
	clusterCfg := rest.CopyConfig(cfg)
	clusterCfgURL, err := url.Parse(clusterCfg.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to parse rest config's Host URL: %w", err)
	}
	clusterCfgURL.Path = fmt.Sprintf("/clusters/%s", name)
	clusterCfg.Host = clusterCfgURL.String()
	return clusterCfg, nil
}
