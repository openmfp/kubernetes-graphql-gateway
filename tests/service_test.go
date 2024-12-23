package tests

import (
	"fmt"
	"github.com/openmfp/crd-gql-gateway/tests/graphql"
	"github.com/stretchr/testify/require"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const sleepTime = 1000 * time.Millisecond

// TestFullSchemaGeneration checks schema generation from not edited OpenAPI spec file.
func (suite *CommonTestSuite) TestFullSchemaGeneration() {
	workspaceName := "myWorkspace"

	// Trigger schema generation and URL creation
	suite.writeToFile("fullSchema", workspaceName)

	// this url must be generated after new file added
	url := fmt.Sprintf("%s/%s/graphql", suite.server.URL, workspaceName)

	// Create the Pod and check results
	createResp, statusCode, err := graphql.SendRequest(url, graphql.CreatePodMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), createResp.Errors, "GraphQL errors: %v", createResp.Errors)
	require.Equal(suite.T(), "test-pod", createResp.Data.Core.CreatePod.Metadata.Name)
}

// TestCreateGetAndDeletePod generates a schema containing only Pod and its references.
// It then creates a Pod, gets it and deletes it.
func (suite *CommonTestSuite) TestCreateGetAndDeletePod() {
	workspaceName := "myWorkspace"

	// Trigger schema generation and URL creation
	suite.writeToFile("podOnly", workspaceName)

	// this url must be generated after new file added
	url := fmt.Sprintf("%s/%s/graphql", suite.server.URL, workspaceName)

	// Create the Pod and check results
	createResp, statusCode, err := graphql.SendRequest(url, graphql.CreatePodMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.NoError(suite.T(), err)
	require.Nil(suite.T(), createResp.Errors, "GraphQL errors: %v", createResp.Errors)

	// Get the Pod
	getResp, statusCode, err := graphql.SendRequest(url, graphql.GetPodQuery())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), getResp.Errors, "GraphQL errors: %v", getResp.Errors)

	podData := getResp.Data.Core.Pod
	require.Equal(suite.T(), "test-pod", podData.Metadata.Name)
	require.Equal(suite.T(), "default", podData.Metadata.Namespace)
	require.Equal(suite.T(), "test-container", podData.Spec.Containers[0].Name)
	require.Equal(suite.T(), "nginx", podData.Spec.Containers[0].Image)

	// Delete the Pod
	deleteResp, statusCode, err := graphql.SendRequest(url, graphql.DeletePodMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), deleteResp.Errors, "GraphQL errors: %v", deleteResp.Errors)

	// Try to get the Pod after deletion
	getRespAfterDelete, statusCode, err := graphql.SendRequest(url, graphql.GetPodQuery())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.NotNil(suite.T(), getRespAfterDelete.Errors, "Expected error when querying deleted Pod, but got none")
}

// TestSchemaUpdate checks if Graphql schema is updated after the file is changed.
// We load schema with Pod only at first, then we update the workspace file to include Service
func (suite *CommonTestSuite) TestSchemaUpdate() {
	workspaceName := "myWorkspace"
	url := fmt.Sprintf("%s/%s/graphql", suite.server.URL, workspaceName)

	// Add "podOnly" spec to the workspace
	suite.writeToFile("podOnly", workspaceName)

	// Create the Pod
	createPodResp, statusCode, err := graphql.SendRequest(url, graphql.CreatePodMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), createPodResp.Errors, "GraphQL errors: %v", createPodResp.Errors)

	// Get the Pod
	getPodResp, statusCode, err := graphql.SendRequest(url, graphql.GetPodQuery())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), getPodResp.Errors, "GraphQL errors: %v", getPodResp.Errors)

	podData := getPodResp.Data.Core.Pod
	require.Equal(suite.T(), "test-pod", podData.Metadata.Name)
	require.Equal(suite.T(), "default", podData.Metadata.Namespace)

	// Write into existing workspace file extended schema with Service included
	suite.writeToFile("podAndServiceOnly", workspaceName)

	// Create the Service
	createServiceResp, statusCode, err := graphql.SendRequest(url, graphql.CreateServiceMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), createServiceResp.Errors, "GraphQL errors during creation: %v", createServiceResp.Errors)

	// Get the Service
	getServiceResp, statusCode, err := graphql.SendRequest(url, graphql.GetServiceQuery())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), getServiceResp.Errors, "GraphQL errors during query: %v", getServiceResp.Errors)

	serviceData := getServiceResp.Data.Core.Service
	require.Equal(suite.T(), "test-service", serviceData.Metadata.Name)
	require.Equal(suite.T(), "default", serviceData.Metadata.Namespace)
	require.Equal(suite.T(), "ClusterIP", serviceData.Spec.Type)
	require.Equal(suite.T(), 80, serviceData.Spec.Ports[0].Port)

	// Delete the Service
	deleteServiceResp, statusCode, err := graphql.SendRequest(url, graphql.DeleteServiceMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), deleteServiceResp.Errors, "GraphQL errors during deletion: %v", deleteServiceResp.Errors)

	// Try to get the Service after deletion
	getServiceRespAfterDelete, statusCode, err := graphql.SendRequest(url, graphql.GetServiceQuery())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.NotNil(suite.T(), getServiceRespAfterDelete.Errors, "Expected error when querying deleted Service, but got none")
}

func (suite *CommonTestSuite) TestWorkspaceRemove() {
	workspaceName := "myWorkspace"
	url := fmt.Sprintf("%s/%s/graphql", suite.server.URL, workspaceName)

	suite.writeToFile("podOnly", workspaceName)

	// Create the Pod
	_, statusCode, err := graphql.SendRequest(url, graphql.CreatePodMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")

	err = os.Remove(filepath.Join(suite.watchedDir, workspaceName))
	require.NoError(suite.T(), err)

	// Wait until the handler is removed
	time.Sleep(sleepTime)

	// Attempt to access the URL again
	_, statusCode, _ = graphql.SendRequest(url, graphql.CreatePodMutation())
	require.Equal(suite.T(), http.StatusNotFound, statusCode, "Expected StatusNotFound after handler is removed")
}

func (suite *CommonTestSuite) TestWorkspaceRename() {
	suite.T().Skip()
	workspaceName := "myWorkspace"
	url := fmt.Sprintf("%s/%s/graphql", suite.server.URL, workspaceName)

	suite.writeToFile("podOnly", workspaceName)

	// Create the Pod
	_, statusCode, err := graphql.SendRequest(url, graphql.CreatePodMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")

	newWorkspaceName := "myNewWorkspace"
	err = os.Rename(filepath.Join(suite.watchedDir, workspaceName), filepath.Join(suite.watchedDir, newWorkspaceName))
	require.NoError(suite.T(), err)
	time.Sleep(sleepTime) // let's give some time to the manager to process the file and create a url

	// old url should not be accessible, status should be NotFound
	_, statusCode, err = graphql.SendRequest(url, graphql.CreatePodMutation())
	fmt.Println("### error: ", err)
	time.Sleep(2000 * time.Millisecond)
	require.Equal(suite.T(), http.StatusNotFound, statusCode, "Expected StatusNotFound after workspace rename")

	// now new url should be accessible
	newUrl := fmt.Sprintf("%s/%s/graphql", suite.server.URL, newWorkspaceName)
	_, statusCode, err = graphql.SendRequest(newUrl, graphql.CreatePodMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
}

// TestCreateGetAndDeleteAccount tests the creation, retrieval, and deletion of an Account resource.
func (suite *CommonTestSuite) TestCreateGetAndDeleteAccount() {
	workspaceName := "myWorkspace"
	url := fmt.Sprintf("%s/%s/graphql", suite.server.URL, workspaceName)

	suite.writeToFile("fullSchema", workspaceName)

	// Create the Account and verify the response
	createResp, statusCode, err := graphql.SendRequest(url, graphql.CreateAccountMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), createResp.Errors, "GraphQL errors: %v", createResp.Errors)

	// Retrieve the Account and verify its details
	getResp, statusCode, err := graphql.SendRequest(url, graphql.GetAccountQuery())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), getResp.Errors, "GraphQL errors: %v", getResp.Errors)

	accountData := getResp.Data.CoreOpenmfpIO.Account
	require.Equal(suite.T(), "test-account", accountData.Metadata.Name)
	require.Equal(suite.T(), "test-account-display-name", accountData.Spec.DisplayName)
	require.Equal(suite.T(), "account", accountData.Spec.Type)

	// Delete the Account and verify the response
	deleteResp, statusCode, err := graphql.SendRequest(url, graphql.DeleteAccountMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), deleteResp.Errors, "GraphQL errors: %v", deleteResp.Errors)

	// Attempt to retrieve the Account after deletion and expect an error
	getRespAfterDelete, statusCode, err := graphql.SendRequest(url, graphql.GetAccountQuery())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.NotNil(suite.T(), getRespAfterDelete.Errors, "Expected error when querying deleted Account, but got none")
}
