package gateway

import (
	"context"
	"github.com/graphql-go/graphql"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"
	"sync"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (suite *CommonTestSuite) TestSchemaSubscribe() {
	tests := []struct {
		testName       string
		namespace      string
		nameArg        string // if set, we will subscribe to a single deployment
		subscribeToAll bool
		nameFilter     string
		setupFunc      func(ctx context.Context)
		expectedEvents int
		expectError    bool
	}{
		{
			testName:  "Subscribe_and_create_deployment_OK",
			namespace: "default",
			//nameArg:   "my-new-deployment",
			setupFunc: func(ctx context.Context) {
				suite.createDeployment(ctx, "my-new-deployment", "default", map[string]string{"app": "my-app"})
			},
			expectedEvents: 1,
		},
		{
			testName:   "Subscribe_and_update_deployment_OK",
			namespace:  "default",
			nameFilter: "my-new-deployment",
			setupFunc: func(ctx context.Context) {
				suite.createDeployment(ctx, "my-new-deployment", "default", map[string]string{"app": "my-app"})
				// this event will be ignored because we didn't subscribe to labels change.
				suite.updateDeployment(ctx, "my-new-deployment", "default", map[string]string{"app": "my-app", "newLabel": "changed"}, 1)
				// this event will be received because we subscribed to replicas change.
				suite.updateDeployment(ctx, "my-new-deployment", "default", map[string]string{"app": "my-app", "newLabel": "changed"}, 2)
			},
			expectedEvents: 2,
		},
		{
			testName:   "Subscribe_and_delete_deployment_OK",
			namespace:  "default",
			nameFilter: "my-new-deployment",
			setupFunc: func(ctx context.Context) {
				suite.createDeployment(ctx, "my-new-deployment", "default", map[string]string{"app": "my-app"})
				suite.deleteDeployment(ctx, "my-new-deployment", "default")
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
				requestString = SubscribeDeployment(tt.namespace, tt.nameArg, tt.subscribeToAll)
			} else {
				requestString = SubscribeDeployments(tt.namespace, tt.subscribeToAll)
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
			}

			wg.Wait()
		})
	}
}

func (suite *CommonTestSuite) createDeployment(ctx context.Context, name, namespace string, labels map[string]string) {
	err := suite.runtimeClient.Create(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
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

func (suite *CommonTestSuite) updateDeployment(ctx context.Context, name, namespace string, labels map[string]string, replicas int32) {
	deployment := &appsv1.Deployment{}
	err := suite.runtimeClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, deployment)
	require.NoError(suite.T(), err)

	deployment.Labels = labels
	deployment.Spec.Replicas = &replicas
	err = suite.runtimeClient.Update(ctx, deployment)
	require.NoError(suite.T(), err)
}

func (suite *CommonTestSuite) deleteDeployment(ctx context.Context, name, namespace string) {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	err := suite.runtimeClient.Delete(ctx, deployment)
	require.NoError(suite.T(), err)
}

func SubscribeDeployments(namespace string, subscribeToAll bool) string {
	return `
		subscription {
			apps_deployments(namespace: "` + namespace + `", subscribeToAll: ` + strconv.FormatBool(subscribeToAll) + `) {
				metadata { name }
				spec { replicas }
			}
		}
	`
}

func SubscribeDeployment(namespace, name string, subscribeToAll bool) string {
	return `
		subscription {
			apps_deployment(namespace: "` + namespace + `", name: "` + name + `", subscribeToAll: ` + strconv.FormatBool(subscribeToAll) + `) {
				metadata { name }
				spec { replicas }
			}
		}
	`
}
