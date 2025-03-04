package gateway

import (
	"fmt"
	"github.com/openmfp/crd-gql-gateway/tests/gateway/helpers"
	"github.com/stretchr/testify/require"
	"net/http"
	"path/filepath"
)

func (suite *CommonTestSuite) TestCrudClusterRole() {
	workspaceName := "myWorkspace"

	// Trigger schema generation and URL creation
	require.NoError(suite.T(), helpers.WriteToFile(
		filepath.Join("testdata", "kubernetes"),
		filepath.Join(suite.appCfg.OpenApiDefinitionsPath, workspaceName),
	))

	// this url must be generated after new file added
	url := fmt.Sprintf("%s/%s/graphql", suite.server.URL, workspaceName)

	// Create ClusterRole and check results
	createResp, statusCode, err := helpers.SendRequest(url, helpers.CreateClusterRoleMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.NoError(suite.T(), err)
	require.Nil(suite.T(), createResp.Errors, "GraphQL errors: %v", createResp.Errors)

	// Get ClusterRole
	getResp, statusCode, err := helpers.SendRequest(url, helpers.GetClusterRoleQuery())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), getResp.Errors, "GraphQL errors: %v", getResp.Errors)

	data := getResp.Data.RbacAuthorizationK8sIo.ClusterRole
	require.Equal(suite.T(), "test-cluster-role", data.Metadata.Name)

	// Delete ClusterRole
	deleteResp, statusCode, err := helpers.SendRequest(url, helpers.DeleteClusterRoleMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.Nil(suite.T(), deleteResp.Errors, "GraphQL errors: %v", deleteResp.Errors)

	// Try to get the ClusterRole after deletion
	getRespAfterDelete, statusCode, err := helpers.SendRequest(url, helpers.GetClusterRoleQuery())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
	require.NotNil(suite.T(), getRespAfterDelete.Errors, "Expected error when querying deleted ClusterRole, but got none")
}
