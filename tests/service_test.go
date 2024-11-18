package tests

import (
	"fmt"
	"github.com/openmfp/crd-gql-gateway/tests/extractors"
	"github.com/openmfp/crd-gql-gateway/tests/graphql"
	"github.com/stretchr/testify/require"
)

// TestFullSchemaGeneration generates the schema from not edited OpenAPI spec file.
func (suite *CommonTestSuite) TestFullSchemaGeneration() {
	workspaceName := "myWorkspace"
	url := fmt.Sprintf("%s/%s/graphql", suite.server.URL, workspaceName)

	// this will trigger schema generation and url creation
	suite.addNewFile("fullSchema", workspaceName)

	createResp, err := graphql.SendGraphQLRequest(url, graphql.CreatePodMutation())
	require.NoError(suite.T(), err)
	if errors, ok := createResp["errors"]; ok {
		suite.T().Fatalf("GraphQL errors: %v", errors)
	}
}

func (suite *CommonTestSuite) TestCreateGetAndDeletePod() {
	workspaceName := "myWorkspace"
	url := fmt.Sprintf("%s/%s/graphql", suite.server.URL, workspaceName)

	suite.addNewFile("podOnly", workspaceName)

	createResp, err := graphql.SendGraphQLRequest(url, graphql.CreatePodMutation())
	require.NoError(suite.T(), err)
	if errors, ok := createResp["errors"]; ok {
		suite.T().Fatalf("GraphQL errors: %v", errors)
	}

	getResp, err := graphql.SendGraphQLRequest(url, graphql.GetPodQuery())
	require.NoError(suite.T(), err)
	if errors, ok := getResp["errors"]; ok {
		suite.T().Fatalf("GraphQL errors: %v", errors)
	}

	podData, err := extractors.ExtractPodData(getResp)
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), "test-pod", podData.Metadata.Name)
	require.Equal(suite.T(), "default", podData.Metadata.Namespace)
	require.Equal(suite.T(), "test-container", podData.Spec.Containers[0].Name)
	require.Equal(suite.T(), "nginx", podData.Spec.Containers[0].Image)

	deleteResp, err := graphql.SendGraphQLRequest(url, graphql.DeletePodMutation())
	require.NoError(suite.T(), err)
	if errors, ok := deleteResp["errors"]; ok {
		suite.T().Fatalf("GraphQL errors: %v", errors)
	}

	getRespAfterDelete, err := graphql.SendGraphQLRequest(url, graphql.GetPodQuery())
	require.NoError(suite.T(), err)
	if errors, ok := getRespAfterDelete["errors"]; ok {
		suite.T().Logf("Expected error after deletion: %v", errors)
	} else {
		suite.T().Fatalf("Expected error when querying deleted Pod, but got none")
	}
}

// TestSchemaUpdate checks if Graphql schema is updated after the file is changed.
// We load schema with Pod only at first, then we update the workspace file to include Service
func (suite *CommonTestSuite) TestSchemaUpdate() {
	workspaceName := "myWorkspace"
	url := fmt.Sprintf("%s/%s/graphql", suite.server.URL, workspaceName)

	suite.addNewFile("podOnly", workspaceName)

	createResp, err := graphql.SendGraphQLRequest(url, graphql.CreatePodMutation())
	require.NoError(suite.T(), err)
	if errors, ok := createResp["errors"]; ok {
		suite.T().Fatalf("GraphQL errors: %v", errors)
	}

	getResp, err := graphql.SendGraphQLRequest(url, graphql.GetPodQuery())
	require.NoError(suite.T(), err)
	if errors, ok := getResp["errors"]; ok {
		suite.T().Fatalf("GraphQL errors: %v", errors)
	}

	// now let's add spec with service under the same workspace name
	suite.addNewFile("podAndServiceOnly", workspaceName)
	createResp, err = graphql.SendGraphQLRequest(url, graphql.CreateServiceMutation())
	require.NoError(suite.T(), err)
	if errors, ok := createResp["errors"]; ok {
		suite.T().Fatalf("GraphQL errors during creation: %v", errors)
	}

	getResp, err = graphql.SendGraphQLRequest(url, graphql.GetServiceQuery())
	require.NoError(suite.T(), err)
	if errors, ok := getResp["errors"]; ok {
		suite.T().Fatalf("GraphQL errors during query: %v", errors)
	}

	serviceData, err := extractors.ExtractServiceData(getResp)
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), "test-service", serviceData.Metadata.Name)
	require.Equal(suite.T(), "default", serviceData.Metadata.Namespace)
	require.Equal(suite.T(), "ClusterIP", serviceData.Spec.Type)
	require.Equal(suite.T(), 80, serviceData.Spec.Ports[0].Port)
	require.Equal(suite.T(), 8080, serviceData.Spec.Ports[0].TargetPort)

	deleteResp, err := graphql.SendGraphQLRequest(url, graphql.DeleteServiceMutation())
	require.NoError(suite.T(), err)
	if errors, ok := deleteResp["errors"]; ok {
		suite.T().Fatalf("GraphQL errors during deletion: %v", errors)
	}

	getRespAfterDelete, err := graphql.SendGraphQLRequest(url, graphql.GetServiceQuery())
	require.NoError(suite.T(), err)
	if errors, ok := getRespAfterDelete["errors"]; ok {
		suite.T().Logf("Expected error after deletion: %v", errors)
	} else {
		suite.T().Fatalf("Expected error when querying deleted Service, but got none")
	}
}
