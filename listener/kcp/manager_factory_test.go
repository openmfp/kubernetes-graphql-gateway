package kcp

import (
	"testing"

	kcpapis "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
)

func TestNewKcpManager(t *testing.T) {

	//TODO: fix
	t.Skip()

	tests := map[string]struct {
		cfg       *rest.Config
		expectErr bool
	}{
		"successful manager creation": {
			cfg: &rest.Config{},
		},
		"error from virtualWorkspaceConfigFromCfg": {
			cfg:       &rest.Config{},
			expectErr: true,
		},
		"error from NewClusterAwareManager": {
			cfg:       &rest.Config{},
			expectErr: true,
		},
	}

	for name, tt := range tests {
		scheme := runtime.NewScheme()
		err := kcpapis.AddToScheme(scheme)
		assert.NoError(t, err)
		t.Run(name, func(t *testing.T) {
			mgr, err := NewKcpManager(tt.cfg, ctrl.Options{
				Scheme: scheme,
			})

			if tt.expectErr {
				assert.Error(t, err)
				assert.Nil(t, mgr)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, mgr)
		})
	}
}
