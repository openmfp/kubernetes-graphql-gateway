package gateway

import (
	"context"
	"github.com/graphql-go/graphql"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"
	"sync"
	"testing"
	"time"
)

func (suite *CommonTestSuite) TestSchemaSubscribe() {
	tests := []struct {
		testName       string
		nameArg        string // if set, we will subscribe to a single deployment
		labelsMap      map[string]string
		subscribeToAll bool

		setupFunc      func(ctx context.Context)
		expectedEvents int
		expectError    bool
	}{
		{
			testName: "subscribe_deployment_and_create_deployment_OK",
			nameArg:  "my-new-deployment",
			setupFunc: func(ctx context.Context) {
				suite.createDeployment(ctx, "my-new-deployment", map[string]string{"app": "my-app"})
			},
			expectedEvents: 1,
		},
		{
			testName: "subscribe_to_replicas_change_OK",
			setupFunc: func(ctx context.Context) {
				suite.createDeployment(ctx, "my-new-deployment", map[string]string{"app": "my-app"})
				// this event will be ignored because we didn't subscribe to labels change.
				suite.updateDeployment(ctx, "my-new-deployment", map[string]string{"app": "my-app", "newLabel": "changed"}, 1)
				// this event will be received because we subscribed to replicas change.
				suite.updateDeployment(ctx, "my-new-deployment", map[string]string{"app": "my-app", "newLabel": "changed"}, 2)
			},
			expectedEvents: 2,
		},
		{
			testName:       "subscribe_to_deployments_by_labels_OK",
			labelsMap:      map[string]string{"deployment": "first"},
			subscribeToAll: true,
			setupFunc: func(ctx context.Context) {
				suite.createDeployment(ctx, "my-first-deployment", map[string]string{"deployment": "first"})
				// this event will be ignored because we subscribe to deployment=first labels only
				suite.createDeployment(ctx, "my-second-deployment", map[string]string{"deployment": "second"})
			},
			expectedEvents: 1,
		},
		{
			testName: "subscribe_deployments_and_delete_deployment_OK",
			setupFunc: func(ctx context.Context) {
				suite.createDeployment(ctx, "my-new-deployment", map[string]string{"app": "my-app"})
				suite.deleteDeployment(ctx, "my-new-deployment")
			},
			expectedEvents: 2,
		},
	}

	for _, tt := range tests {
		suite.T().Run(tt.testName, func(t *testing.T) {
			// To prevent naming conflict, lets start each table test with a clean slate
			suite.SetupTest()
			defer suite.TearDownTest()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			var requestString string
			if tt.nameArg != "" {
				requestString = SubscribeDeployment(tt.nameArg, tt.subscribeToAll)
			} else {
				requestString = SubscribeDeployments(tt.labelsMap, tt.subscribeToAll)
			}

			c := graphql.Subscribe(graphql.Params{
				Context:       ctx,
				RequestString: requestString,
				Schema:        suite.schema,
			})

			wg := sync.WaitGroup{}
			wg.Add(tt.expectedEvents)

			go func() {
				for res := range c {
					if tt.expectError {
						require.NotNil(t, res.Errors)
					} else {
						require.NotNil(t, res.Data)
					}
					wg.Done()
				}
			}()

			if tt.setupFunc != nil {
				tt.setupFunc(ctx)
				// we need this to wait for negative WaitGroup counter in case of more events than expected
				time.Sleep(100 * time.Millisecond)
			}

			wg.Wait()
		})
	}
}

func (suite *CommonTestSuite) createDeployment(ctx context.Context, name string, labels map[string]string) {
	err := suite.runtimeClient.Create(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "nginx", Image: "nginx:latest"}}},
			},
		},
	})
	require.NoError(suite.T(), err)
}

func (suite *CommonTestSuite) updateDeployment(ctx context.Context, name string, labels map[string]string, replicas int32) {
	deployment := &appsv1.Deployment{}
	err := suite.runtimeClient.Get(ctx, client.ObjectKey{
		Name: name, Namespace: "default",
	}, deployment)
	require.NoError(suite.T(), err)

	deployment.Labels = labels
	deployment.Spec.Replicas = &replicas
	err = suite.runtimeClient.Update(ctx, deployment)
	require.NoError(suite.T(), err)
}

func (suite *CommonTestSuite) deleteDeployment(ctx context.Context, name string) {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
	}
	err := suite.runtimeClient.Delete(ctx, deployment)
	require.NoError(suite.T(), err)
}

func SubscribeDeployments(labelsMap map[string]string, subscribeToAll bool) string {
	if labelsMap != nil {
		return `
		subscription {
			apps_deployments(labelselector: "` + labels.FormatLabels(labelsMap) + `", namespace: "default", subscribeToAll: ` + strconv.FormatBool(subscribeToAll) + `) {
				metadata { name }
				spec { replicas }
			}
		}
	`
	}

	return `
		subscription {
			apps_deployments(namespace: "default", subscribeToAll: ` + strconv.FormatBool(subscribeToAll) + `) {
				metadata { name }
				spec { replicas }
			}
		}
	`

}

func SubscribeDeployment(name string, subscribeToAll bool) string {
	return `
		subscription {
			apps_deployment(namespace: "default", name: "` + name + `", subscribeToAll: ` + strconv.FormatBool(subscribeToAll) + `) {
				metadata { name }
				spec { replicas }
			}
		}
	`
}
