package gateway

import (
	"fmt"
	"github.com/openmfp/crd-gql-gateway/tests/gateway/helpers"
	"github.com/stretchr/testify/require"
	"net/http"
	"path/filepath"
)

// TestCreateGetAndDeleteAccount tests the creation, retrieval, and deletion of an Account resource.
func (suite *CommonTestSuite) TestCreateGetAndDeleteAccount() {
	workspaceName := "myWorkspace"
	url := fmt.Sprintf("%s/%s/graphql", suite.server.URL, workspaceName)

	require.NoError(suite.T(), helpers.WriteToFile(
		filepath.Join("testdata", "kubernetes"),
		filepath.Join(suite.appCfg.OpenApiDefinitionsPath, workspaceName),
	))

	// Create the Account and verify the response
	createResp, statusCode, err := helpers.SendRequest(url, helpers.CreateAccountMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), createResp.Errors, "GraphQL errors: %v", createResp.Errors)

	// Retrieve the Account and verify its details
	getResp, statusCode, err := helpers.SendRequest(url, helpers.GetAccountQuery())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), getResp.Errors, "GraphQL errors: %v", getResp.Errors)

	accountData := getResp.Data.CoreOpenmfpIO.Account
	require.Equal(suite.T(), "test-account", accountData.Metadata.Name)
	require.Equal(suite.T(), "test-account-display-name", accountData.Spec.DisplayName)
	require.Equal(suite.T(), "account", accountData.Spec.Type)

	// Delete the Account and verify the response
	deleteResp, statusCode, err := helpers.SendRequest(url, helpers.DeleteAccountMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), deleteResp.Errors, "GraphQL errors: %v", deleteResp.Errors)

	// Attempt to retrieve the Account after deletion and expect an error
	getRespAfterDelete, statusCode, err := helpers.SendRequest(url, helpers.GetAccountQuery())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.NotNil(suite.T(), getRespAfterDelete.Errors, "Expected error when querying deleted Account, but got none")
}
