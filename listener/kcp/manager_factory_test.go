package kcp

import (
	"context"
	"testing"

	"github.com/openmfp/golang-commons/logger"
	"github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"github.com/stretchr/testify/require"

	kcpapis "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const validAPIServerHost = "https://192.168.1.13:6443"

func TestNewManager(t *testing.T) {

	tests := map[string]struct {
		isKCPEnabled bool
		expectErr    bool
	}{
		"successful_KCP_manager_creation": {isKCPEnabled: true, expectErr: false},
		"successful_manager_creation":     {isKCPEnabled: false, expectErr: false},
	}

	log, err := logger.New(logger.DefaultConfig())
	require.NoError(t, err)

	for name, tc := range tests {
		scheme := runtime.NewScheme()
		err := kcpapis.AddToScheme(scheme)
		assert.NoError(t, err)
		t.Run(name, func(t *testing.T) {
			appCfg := config.Config{
				EnableKcp: true,
			}

			f := NewManagerFactory(log, appCfg)

			mgr, err := f.NewManager(
				context.Background(),
				&rest.Config{Host: validAPIServerHost},
				ctrl.Options{Scheme: scheme},
				fake.NewClientBuilder().WithScheme(scheme).Build(),
			)

			if tc.expectErr {
				assert.Error(t, err)
				assert.Nil(t, mgr)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, mgr)
		})
	}
}
