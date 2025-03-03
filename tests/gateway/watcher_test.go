package gateway

import (
	"fmt"
	"github.com/openmfp/crd-gql-gateway/tests/gateway/helpers"
	"github.com/stretchr/testify/require"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// TestSchemaUpdate checks if Graphql schema is updated after the file is changed.
// We load schema with Pod only at first, then we update the workspace file to include Service
func (suite *CommonTestSuite) TestSchemaUpdate() {
	suite.T().Skip()
}

func (suite *CommonTestSuite) TestWorkspaceRemove() {
	workspaceName := "myWorkspace"
	url := fmt.Sprintf("%s/%s/graphql", suite.server.URL, workspaceName)

	require.NoError(suite.T(), helpers.WriteToFile(
		filepath.Join("testdata", "kubernetes"),
		filepath.Join(suite.appCfg.OpenApiDefinitionsPath, workspaceName),
	))

	// Create the Pod
	_, statusCode, err := helpers.SendRequest(url, helpers.CreatePodMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")

	err = os.Remove(filepath.Join(suite.appCfg.OpenApiDefinitionsPath, workspaceName))
	require.NoError(suite.T(), err)

	// Wait until the handler is removed
	time.Sleep(helpers.SleepTime)

	// Attempt to access the URL again
	_, statusCode, _ = helpers.SendRequest(url, helpers.CreatePodMutation())
	require.Equal(suite.T(), http.StatusNotFound, statusCode, "Expected StatusNotFound after handler is removed")
}

func (suite *CommonTestSuite) TestWorkspaceRename() {
	workspaceName := "myWorkspace"
	url := fmt.Sprintf("%s/%s/graphql", suite.server.URL, workspaceName)

	require.NoError(suite.T(), helpers.WriteToFile(
		filepath.Join("testdata", "kubernetes"),
		filepath.Join(suite.appCfg.OpenApiDefinitionsPath, workspaceName),
	))

	// Create the Pod
	_, statusCode, err := helpers.SendRequest(url, helpers.CreatePodMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")

	newWorkspaceName := "myNewWorkspace"
	err = os.Rename(filepath.Join(suite.appCfg.OpenApiDefinitionsPath, workspaceName), filepath.Join(suite.appCfg.OpenApiDefinitionsPath, newWorkspaceName))
	require.NoError(suite.T(), err)
	time.Sleep(helpers.SleepTime) // let's give some time to the manager to process the file and create a url

	// old url should not be accessible, status should be NotFound
	_, statusCode, _ = helpers.SendRequest(url, helpers.CreatePodMutation())
	require.Equal(suite.T(), http.StatusNotFound, statusCode, "Expected StatusNotFound after workspace rename")

	// now new url should be accessible
	newUrl := fmt.Sprintf("%s/%s/graphql", suite.server.URL, newWorkspaceName)
	_, statusCode, err = helpers.SendRequest(newUrl, helpers.CreatePodMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")
}
