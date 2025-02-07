package gateway_test

import (
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/openmfp/crd-gql-gateway/tests/gateway_test/testutils"
	"github.com/stretchr/testify/require"
)

const (
	sleepTime     = 2000 * time.Millisecond
	workspaceName = "myWorkspace"
)

// TestFullSchemaGeneration checks schema generation
func (suite *CommonTestSuite) TestFullSchemaGeneration() {
	suite.writeToFile("fullSchema", workspaceName)

	// Create the Pod and check results
	createResp, statusCode, err := testutils.SendRequest(suite.getUrl(workspaceName), testutils.CreatePodMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), createResp.Errors, "GraphQL errors: %v", createResp.Errors)
	require.Equal(suite.T(), "test-pod", createResp.Data.Core.CreatePod.Metadata.Name)
}

// TestPodCRUD tests schema generation and then CRUD operations on Pod resource.
func (suite *CommonTestSuite) TestPodCRUD() {
	suite.writeToFile("podOnly", workspaceName)
	url := suite.getUrl(workspaceName)

	// Create the Pod and check results
	createResp, statusCode, err := testutils.SendRequest(url, testutils.CreatePodMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), createResp.Errors, "GraphQL errors: %v", createResp.Errors)

	// Let's update Pod and add new label to it
	updateResp, statusCode, err := testutils.SendRequest(url, testutils.UpdatePodMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Equal(suite.T(), "labelForTest", updateResp.Data.Core.UpdatePod.Metadata.Labels["labelForTest"])

	// Get the Pod
	getResp, statusCode, err := testutils.SendRequest(url, testutils.GetPodQuery())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), getResp.Errors, "GraphQL errors: %v", getResp.Errors)

	podData := getResp.Data.Core.Pod
	require.Equal(suite.T(), "test-pod", podData.Metadata.Name)
	require.Equal(suite.T(), "default", podData.Metadata.Namespace)
	require.Equal(suite.T(), "test-container", podData.Spec.Containers[0].Name)
	require.Equal(suite.T(), "nginx", podData.Spec.Containers[0].Image)

	// List pods
	listResp, statusCode, err := testutils.SendRequest(url, testutils.ListPodsQuery())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), getResp.Errors, "GraphQL errors: %v", getResp.Errors)

	listPodsData := listResp.Data.Core.Pods
	require.Equal(suite.T(), 1, len(listPodsData))
	require.Equal(suite.T(), "test-pod", listPodsData[0].Metadata.Name)
	require.Equal(suite.T(), "default", listPodsData[0].Metadata.Namespace)
	require.Equal(suite.T(), "test-container", listPodsData[0].Spec.Containers[0].Name)
	require.Equal(suite.T(), "nginx", listPodsData[0].Spec.Containers[0].Image)

	// Delete the Pod
	deleteResp, statusCode, err := testutils.SendRequest(url, testutils.DeletePodMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), deleteResp.Errors, "GraphQL errors: %v", deleteResp.Errors)

	// Try to get the Pod after deletion
	getRespAfterDelete, statusCode, err := testutils.SendRequest(url, testutils.GetPodQuery())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.NotNil(suite.T(), getRespAfterDelete.Errors, "Expected error when querying deleted Pod, but got none")
}

// TestSchemaUpdate checks if Graphql schema is updated after the file is changed.
// Initial spec contains only Pod, and then we update that file to include Service
func (suite *CommonTestSuite) TestSchemaUpdate() {
	suite.writeToFile("podOnly", workspaceName)
	url := suite.getUrl(workspaceName)

	// Create the Pod
	createPodResp, statusCode, err := testutils.SendRequest(url, testutils.CreatePodMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), createPodResp.Errors, "GraphQL errors: %v", createPodResp.Errors)

	// Get the Pod
	getPodResp, statusCode, err := testutils.SendRequest(url, testutils.GetPodQuery())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), getPodResp.Errors, "GraphQL errors: %v", getPodResp.Errors)

	podData := getPodResp.Data.Core.Pod
	require.Equal(suite.T(), "test-pod", podData.Metadata.Name)
	require.Equal(suite.T(), "default", podData.Metadata.Namespace)

	// Write into existing workspace file extended schema with Service included
	suite.writeToFile("podAndServiceOnly", workspaceName)

	// Create the Service
	createServiceResp, statusCode, err := testutils.SendRequest(url, testutils.CreateServiceMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), createServiceResp.Errors, "GraphQL errors during creation: %v", createServiceResp.Errors)

	// Get the Service
	getServiceResp, statusCode, err := testutils.SendRequest(url, testutils.GetServiceQuery())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), getServiceResp.Errors, "GraphQL errors during query: %v", getServiceResp.Errors)

	serviceData := getServiceResp.Data.Core.Service
	require.Equal(suite.T(), "test-service", serviceData.Metadata.Name)
	require.Equal(suite.T(), "default", serviceData.Metadata.Namespace)
	require.Equal(suite.T(), "ClusterIP", serviceData.Spec.Type)
	require.Equal(suite.T(), 80, serviceData.Spec.Ports[0].Port)

	// Delete the Service
	deleteServiceResp, statusCode, err := testutils.SendRequest(url, testutils.DeleteServiceMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), deleteServiceResp.Errors, "GraphQL errors during deletion: %v", deleteServiceResp.Errors)

	// Try to get the Service after deletion
	getServiceRespAfterDelete, statusCode, err := testutils.SendRequest(url, testutils.GetServiceQuery())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.NotNil(suite.T(), getServiceRespAfterDelete.Errors, "Expected error when querying deleted Service, but got none")
}

// TestWorkspaceRemove checks if graphql handler is removed after the spec file is deleted.
func (suite *CommonTestSuite) TestSpecFileRemove() {
	suite.writeToFile("podOnly", workspaceName)
	url := suite.getUrl(workspaceName)

	// Create the Pod
	_, statusCode, err := testutils.SendRequest(url, testutils.CreatePodMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")

	err = os.Remove(filepath.Join(suite.appCfg.OpenApiDefinitionsPath, workspaceName))
	require.NoError(suite.T(), err)

	// Wait until the handler is removed
	time.Sleep(sleepTime)

	// Attempt to access the URL again
	_, statusCode, _ = testutils.SendRequest(url, testutils.CreatePodMutation())
	require.Equal(suite.T(), http.StatusNotFound, statusCode, "Expected StatusNotFound after handler is removed")
}

// TestWorkspaceRename checks if graphql handler is updated after the spec file is renamed.
func (suite *CommonTestSuite) TestSpecFileRename() {
	suite.writeToFile("podOnly", workspaceName)
	url := suite.getUrl(workspaceName)

	// Check if the handler is accessible
	_, statusCode, err := testutils.SendRequest(url, testutils.CreatePodMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")

	// rename workspace
	newWorkspaceName := "myNewWorkspace"
	err = os.Rename(filepath.Join(suite.appCfg.OpenApiDefinitionsPath, workspaceName), filepath.Join(suite.appCfg.OpenApiDefinitionsPath, newWorkspaceName))
	require.NoError(suite.T(), err)
	time.Sleep(sleepTime) // let's give some time to the manager to process the file and create a url

	// old url should not be accessible, status should be NotFound
	_, statusCode, _ = testutils.SendRequest(url, testutils.CreatePodMutation())
	require.Equal(suite.T(), http.StatusNotFound, statusCode, "Expected StatusNotFound after workspace rename")

	// now new url should be accessible
	_, statusCode, err = testutils.SendRequest(suite.getUrl(newWorkspaceName), testutils.CreatePodMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
}

// TestCreateGetAndDeleteAccount tests the creation, retrieval, and deletion of an Account resource.
func (suite *CommonTestSuite) TestCreateGetAndDeleteAccount() {
	suite.writeToFile("fullSchema", workspaceName)
	url := suite.getUrl(workspaceName)

	// Create the Account and verify the response
	createResp, statusCode, err := testutils.SendRequest(url, testutils.CreateAccountMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), createResp.Errors, "GraphQL errors: %v", createResp.Errors)

	// Retrieve the Account and verify its details
	getResp, statusCode, err := testutils.SendRequest(url, testutils.GetAccountQuery())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), getResp.Errors, "GraphQL errors: %v", getResp.Errors)

	accountData := getResp.Data.CoreOpenmfpIO.Account
	require.Equal(suite.T(), "test-account", accountData.Metadata.Name)
	require.Equal(suite.T(), "test-account-display-name", accountData.Spec.DisplayName)
	require.Equal(suite.T(), "account", accountData.Spec.Type)

	// Delete the Account and verify the response
	deleteResp, statusCode, err := testutils.SendRequest(url, testutils.DeleteAccountMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), deleteResp.Errors, "GraphQL errors: %v", deleteResp.Errors)

	// Attempt to retrieve the Account after deletion and expect an error
	getRespAfterDelete, statusCode, err := testutils.SendRequest(url, testutils.GetAccountQuery())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.NotNil(suite.T(), getRespAfterDelete.Errors, "Expected error when querying deleted Account, but got none")
}

// TestSubscribeToSingleAccount tests subscription to Account resource.
// We create an account, subscribe to it and then change it.
func (suite *CommonTestSuite) TestSubscribeToSingleAccount() {
	suite.writeToFile("fullSchema", workspaceName)
	url := suite.getUrl(workspaceName)

	events, cancel, err := testutils.Subscribe(url, testutils.SubscribeToSingleAccount(), suite.log)
	if err != nil {
		suite.log.Error().Err(err).Msg("Failed to subscribe to events")
		return
	}

	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		for msg := range events {
			// let's skip the first event triggered by the account creation.
			if msg.Data.Account.Spec.DisplayName == "test-account-display-name" {
				continue
			}
			require.Equal(suite.T(), "new display name", msg.Data.Account.Spec.DisplayName)
			wg.Done()
		}
	}()

	// Create the Account and verify the response
	_, statusCode, err := testutils.SendRequest(url, testutils.CreateAccountMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")

	// Update the Account and verify the response
	updateResp, statusCode, err := testutils.SendRequest(url, testutils.UpdateAccountMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Equal(suite.T(), "new display name", updateResp.Data.CoreOpenmfpIO.UpdateAccount.Spec.DisplayName)

	wg.Wait()
	cancel()
}

// TestSubscribeToAccounts tests subscription to Account resource.
// We subscribe first, then create an Account and verify that subscription returns it.
func (suite *CommonTestSuite) TestSubscribeToAccounts() {
	suite.writeToFile("fullSchema", workspaceName)
	url := suite.getUrl(workspaceName)

	events, cancel, err := testutils.Subscribe(url, testutils.SubscribeToAccounts(), suite.log)
	if err != nil {
		suite.log.Error().Err(err).Msg("Failed to subscribe to events")
		return
	}

	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		for msg := range events {
			require.Equal(suite.T(), "test-account-display-name", msg.Data.Accounts[0].Spec.DisplayName)
			wg.Done()
		}
	}()

	// Create the Account and verify the response
	_, statusCode, err := testutils.SendRequest(url, testutils.CreateAccountMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")

	wg.Wait()
	cancel()
}
