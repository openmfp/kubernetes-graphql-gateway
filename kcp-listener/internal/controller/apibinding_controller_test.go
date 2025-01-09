package controller_test

import (
	"context"
	"os"
	"testing"

	schemamocks "github.com/openmfp/crd-gql-gateway/kcp-listener/internal/apischema/mocks"
	"github.com/openmfp/crd-gql-gateway/kcp-listener/internal/controller"
	discoverymocks "github.com/openmfp/crd-gql-gateway/kcp-listener/internal/discoveryclient/mocks"
	iomocks "github.com/openmfp/crd-gql-gateway/kcp-listener/internal/workspacefile/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	ctrl "sigs.k8s.io/controller-runtime"
)

type testCase struct {
	name          string
	req           ctrl.Request
	expectError   bool
	expectRequeue bool
	ioMocks       func(*iomocks.MockIOHandler)
	dfMocks       func(*discoverymocks.MockFactory)
	schemaMocks   func(*schemamocks.MockResolver)
}

func TestReconcile(t *testing.T) {
	testCases := []testCase{
		{
			name: "should save root cluster API schema when an apibinding resource is created",
			req: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "",
					Name:      "tenancy.kcp.io",
				},
				ClusterName: "root",
			},
			expectError:   false,
			expectRequeue: false,
			ioMocks: func(mh *iomocks.MockIOHandler) {
				mh.EXPECT().Read(mock.Anything).Return(nil, os.ErrNotExist).Once()
				mh.EXPECT().Write(mock.Anything, mock.Anything).Return(nil).Once()
			},
			dfMocks: func(mf *discoverymocks.MockFactory) {
				mf.EXPECT().ClientForCluster(mock.Anything).Return(nil, nil).Once()
			},
			schemaMocks: func(mr *schemamocks.MockResolver) {
				mr.EXPECT().Resolve(mock.Anything).RunAndReturn(
					func(_ discovery.DiscoveryInterface) ([]byte, error) {
						return os.ReadFile("../apischema/mocks/schemas/root_api_mock.json")
					},
				)
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {

			reconciler := setupMocks(t, tc)

			res, err := reconciler.Reconcile(context.TODO(), tc.req)
			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.Nil(t, err)
			}

			assert.Equal(t, tc.expectRequeue, res.Requeue)
		})
	}
}

func setupMocks(t *testing.T, tc testCase) *controller.APIBindingReconciler {
	ioHandler := iomocks.NewMockIOHandler(t)
	if tc.ioMocks != nil {
		tc.ioMocks(ioHandler)
	}

	df := discoverymocks.NewMockFactory(t)
	if tc.dfMocks != nil {
		tc.dfMocks(df)
	}

	sc := schemamocks.NewMockResolver(t)
	if tc.schemaMocks != nil {
		tc.schemaMocks(sc)
	}

	return controller.NewAPIBindingReconciler(ioHandler, df, sc)
}
