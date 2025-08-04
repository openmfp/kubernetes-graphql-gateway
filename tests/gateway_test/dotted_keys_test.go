package gateway_test

import (
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/stretchr/testify/require"
)

// TestDottedKeysIntegration tests all dotted key fields in a single Deployment resource
func (suite *CommonTestSuite) TestDottedKeysIntegration() {
	workspaceName := "dottedKeysWorkspace"

	require.NoError(suite.T(), suite.writeToFileWithClusterMetadata(
		filepath.Join("testdata", "kubernetes"),
		filepath.Join(suite.appCfg.OpenApiDefinitionsPath, workspaceName),
	))

	url := fmt.Sprintf("%s/%s/graphql", suite.server.URL, workspaceName)

	// Create the Deployment with all dotted key fields
	createResp, statusCode, err := suite.sendAuthenticatedRequest(url, createDeploymentWithDottedKeys())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), createResp.Errors, "GraphQL errors: %v", createResp.Errors)

	// Get the Deployment and verify all dotted key fields
	getResp, statusCode, err := suite.sendAuthenticatedRequest(url, getDeploymentWithDottedKeys())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), getResp.Errors, "GraphQL errors: %v", getResp.Errors)

	deployment := getResp.Data.Apps.Deployment
	require.Equal(suite.T(), "dotted-keys-deployment", deployment.Metadata.Name)
	require.Equal(suite.T(), "default", deployment.Metadata.Namespace)

	// Verify metadata.labels with dotted keys
	labels := deployment.Metadata.Labels
	require.NotNil(suite.T(), labels)
	require.Len(suite.T(), labels, 3)

	labelMap := make(map[string]string)
	for _, label := range labels {
		labelMap[label.Key] = label.Value
	}
	require.Equal(suite.T(), "my-app", labelMap["app.kubernetes.io/name"])
	require.Equal(suite.T(), "1.0.0", labelMap["app.kubernetes.io/version"])
	require.Equal(suite.T(), "production", labelMap["environment"])

	// Verify metadata.annotations with dotted keys
	annotations := deployment.Metadata.Annotations
	require.NotNil(suite.T(), annotations)
	require.Len(suite.T(), annotations, 2)

	annotationMap := make(map[string]string)
	for _, annotation := range annotations {
		annotationMap[annotation.Key] = annotation.Value
	}
	require.Equal(suite.T(), "1", annotationMap["deployment.kubernetes.io/revision"])
	require.Contains(suite.T(), annotationMap["kubectl.kubernetes.io/last-applied-configuration"], "apiVersion")

	// Verify spec.selector.matchLabels with dotted keys
	matchLabels := deployment.Spec.Selector.MatchLabels
	require.NotNil(suite.T(), matchLabels)
	require.Len(suite.T(), matchLabels, 2)

	matchLabelMap := make(map[string]string)
	for _, label := range matchLabels {
		matchLabelMap[label.Key] = label.Value
	}
	require.Equal(suite.T(), "my-app", matchLabelMap["app.kubernetes.io/name"])
	require.Equal(suite.T(), "frontend", matchLabelMap["app.kubernetes.io/component"])

	// Verify spec.template.spec.nodeSelector with dotted keys
	nodeSelector := deployment.Spec.Template.Spec.NodeSelector
	require.NotNil(suite.T(), nodeSelector)
	require.Len(suite.T(), nodeSelector, 2)

	nodeSelectorMap := make(map[string]string)
	for _, selector := range nodeSelector {
		nodeSelectorMap[selector.Key] = selector.Value
	}
	require.Equal(suite.T(), "amd64", nodeSelectorMap["kubernetes.io/arch"])
	require.Equal(suite.T(), "m5.large", nodeSelectorMap["node.kubernetes.io/instance-type"])

	// Clean up: Delete the Deployment
	deleteResp, statusCode, err := suite.sendAuthenticatedRequest(url, deleteDeploymentMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), deleteResp.Errors, "GraphQL errors: %v", deleteResp.Errors)
}

func createDeploymentWithDottedKeys() string {
	return `
	mutation {
		apps {
			createDeployment(
				namespace: "default"
				object: {
					metadata: {
						name: "dotted-keys-deployment"
						labels: [
							{key: "app.kubernetes.io/name", value: "my-app"},
							{key: "app.kubernetes.io/version", value: "1.0.0"},
							{key: "environment", value: "production"}
						]
						annotations: [
							{key: "deployment.kubernetes.io/revision", value: "1"},
							{key: "kubectl.kubernetes.io/last-applied-configuration", value: "{\"apiVersion\":\"apps/v1\",\"kind\":\"Deployment\"}"}
						]
					}
					spec: {
						replicas: 2
						selector: {
							matchLabels: [
								{key: "app.kubernetes.io/name", value: "my-app"},
								{key: "app.kubernetes.io/component", value: "frontend"}
							]
						}
						template: {
							metadata: {
								labels: [
									{key: "app.kubernetes.io/name", value: "my-app"},
									{key: "app.kubernetes.io/component", value: "frontend"}
								]
							}
							spec: {
								nodeSelector: [
									{key: "kubernetes.io/arch", value: "amd64"},
									{key: "node.kubernetes.io/instance-type", value: "m5.large"}
								]
								containers: [
									{
										name: "web"
										image: "nginx:1.21"
										ports: [
											{
												containerPort: 80
											}
										]
									}
								]
							}
						}
					}
				}
			) {
				metadata {
					name
					namespace
				}
			}
		}
	}
	`
}

func getDeploymentWithDottedKeys() string {
	return `
	query {
		apps {
			Deployment(namespace: "default", name: "dotted-keys-deployment") {
				metadata {
					name
					namespace
					labels {
						key
						value
					}
					annotations {
						key
						value
					}
				}
				spec {
					replicas
					selector {
						matchLabels {
							key
							value
						}
					}
					template {
						metadata {
							labels {
								key
								value
							}
						}
						spec {
							nodeSelector {
								key
								value
							}
							containers {
								name
								image
								ports {
									containerPort
								}
							}
						}
					}
				}
			}
		}
	}
	`
}

func deleteDeploymentMutation() string {
	return `
	mutation {
		apps {
			deleteDeployment(namespace: "default", name: "dotted-keys-deployment")
		}
	}
	`
}
