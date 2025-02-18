package discoveryclient

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/discovery"
	fakediscovery "k8s.io/client-go/discovery/fake"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

func TestNewFactory(t *testing.T) {
	tests := map[string]struct {
		inputCfg  *rest.Config
		expectErr bool
	}{
		"valid config": {inputCfg: &rest.Config{}, expectErr: false},
		"nil config":   {inputCfg: nil, expectErr: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			factory, err := NewFactory(tc.inputCfg)
			if tc.expectErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.NotNil(t, factory)
			assert.Equal(t, factory.restCfg, tc.inputCfg)
		})
	}
}

func TestClientForCluster(t *testing.T) {
	tests := map[string]struct {
		clusterName string
		restCfg     *rest.Config
		expectErr   bool
	}{
		"nil config":     {clusterName: "test-cluster", restCfg: nil, expectErr: true},
		"non nil config": {clusterName: "test-cluster", restCfg: &rest.Config{}, expectErr: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			factory := &Factory{
				restCfg:       tc.restCfg,
				clientFactory: fakeClientFactory,
			}
			dc, err := factory.ClientForCluster(tc.clusterName)
			if tc.expectErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.NotNil(t, dc)
		})
	}
}

func fakeClientFactory(_ *rest.Config) (discovery.DiscoveryInterface, error) {
	client := fakeclientset.NewClientset()
	fakeDiscovery, ok := client.Discovery().(*fakediscovery.FakeDiscovery)
	if !ok {
		return nil, errors.New("failed to get fake discovery client")
	}
	return fakeDiscovery, nil
}
