package kcp

import (
	"errors"
	"github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"path"
	"testing"

	kcpapis "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	fakediscovery "k8s.io/client-go/discovery/fake"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/openmfp/kubernetes-graphql-gateway/listener/apischema"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/discoveryclient"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/workspacefile"
)

const (
	validAPIServerHost      = "https://192.168.1.13:6443"
	schemalessAPIServerHost = "://192.168.1.13:6443"
)

func TestNewReconciler(t *testing.T) {
	tempDir := t.TempDir()

	tests := map[string]struct {
		cfg             *rest.Config
		definitionsPath string
		isKCPEnabled    bool
		err             error
	}{
		"standard_reconciler_creation": {
			cfg:             &rest.Config{Host: validAPIServerHost},
			definitionsPath: tempDir,
			isKCPEnabled:    false,
		},
		"kcp_reconciler_creation": {
			cfg:             &rest.Config{Host: validAPIServerHost},
			definitionsPath: tempDir,
			isKCPEnabled:    true,
		},
		"failure_in_discovery_client_creation_with_kcp_disabled": {
			cfg:             nil,
			definitionsPath: tempDir,
			isKCPEnabled:    false,
			err:             errors.New("failed to create discovery client: config cannot be nil"),
		},
		"failure_in_creation_cluster_path_resolver_due_to_nil_config_with_kcp_enabled": {
			cfg:             nil,
			definitionsPath: tempDir,
			isKCPEnabled:    true,
			err:             errors.New("failed to create cluster path resolver: config should not be nil"),
		},
		"success_in_non-existent-dir": {
			cfg:             &rest.Config{Host: validAPIServerHost},
			definitionsPath: path.Join(tempDir, "non-existent"),
			isKCPEnabled:    false,
		},
		"failure_in_rest_mapper_creation": {
			cfg:             &rest.Config{Host: schemalessAPIServerHost},
			definitionsPath: tempDir,
			isKCPEnabled:    false,
			err:             errors.New("failed to create rest mapper from config: failed to create rest mapper: host must be a URL or a host:port pair: \"://192.168.1.13:6443\""),
		},
	}

	for name, tc := range tests {
		scheme := runtime.NewScheme()
		assert.NoError(t, kcpapis.AddToScheme(scheme))

		t.Run(name, func(t *testing.T) {
			appCfg, err := config.NewFromEnv()
			assert.NoError(t, err)
			appCfg.EnableKcp = tc.isKCPEnabled

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects([]client.Object{
				&kcpapis.APIExport{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: appCfg.ApiExportWorkspace,
						Name:      appCfg.ApiExportName,
					},
					Status: kcpapis.APIExportStatus{
						VirtualWorkspaces: []kcpapis.VirtualWorkspace{
							{URL: validAPIServerHost},
						},
					},
				},
			}...).Build()
			f := &ReconcilerFactory{
				AppCfg:             appCfg,
				newDiscoveryIFFunc: fakeClientFactory,
				preReconcileFunc: func(cr *apischema.CRDResolver, io *workspacefile.IOHandler) error {
					return nil
				},
				newDiscoveryFactoryFunc: func(cfg *rest.Config) (*discoveryclient.Factory, error) {
					return &discoveryclient.Factory{
						Config:             cfg,
						NewDiscoveryIFFunc: fakeClientFactory,
					}, nil
				},
			}
			reconciler, err := f.NewReconciler(ReconcilerOpts{
				Config:                 tc.cfg,
				Scheme:                 scheme,
				Client:                 fakeClient,
				OpenAPIDefinitionsPath: tc.definitionsPath,
			})

			if tc.err != nil {
				assert.Equal(t, tc.err.Error(), err.Error())
				assert.Nil(t, reconciler)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, reconciler)
			}
		})
	}
}

func fakeClientFactory(cfg *rest.Config) (discovery.DiscoveryInterface, error) {
	if cfg == nil {
		return nil, errors.New("config cannot be nil")
	}
	client := fakeclientset.NewClientset()
	fakeDiscovery, ok := client.Discovery().(*fakediscovery.FakeDiscovery)
	if !ok {
		return nil, errors.New("failed to get fake discovery client")
	}
	return fakeDiscovery, nil
}
