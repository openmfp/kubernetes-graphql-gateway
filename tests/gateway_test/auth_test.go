package gateway_test

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func (suite *CommonTestSuite) TestTokenValidation() {
	suite.LocalDevelopment = false
	suite.SetupTest()
	defer suite.TearDownTest()

	workspaceName := "myWorkspace"

	require.NoError(suite.T(), writeToFile(
		filepath.Join("testdata", "kubernetes"),
		filepath.Join(suite.appCfg.OpenApiDefinitionsPath, workspaceName),
	))

	url := fmt.Sprintf("%s/%s/graphql", suite.server.URL, workspaceName)

	req, err := http.NewRequest("POST", url, nil)
	require.NoError(suite.T(), err)

	// Use the BearerToken from restCfg, which is valid for envtest
	req.Header.Set("Authorization", "Bearer "+suite.restCfg.BearerToken)

	resp, err := http.DefaultClient.Do(req)
	defer resp.Body.Close()
	require.NoError(suite.T(), err)

	require.NotEqual(suite.T(), http.StatusUnauthorized, resp.StatusCode, "Token should be valid for test cluster")
}

func (suite *CommonTestSuite) TestWorkspaceRemove() {
	workspaceName := "myWorkspace"
	url := fmt.Sprintf("%s/%s/graphql", suite.server.URL, workspaceName)

	require.NoError(suite.T(), writeToFile(
		filepath.Join("testdata", "kubernetes"),
		filepath.Join(suite.appCfg.OpenApiDefinitionsPath, workspaceName),
	))

	// Create the Pod
	_, statusCode, err := sendRequest(url, createPodMutation())
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), http.StatusOK, statusCode, "Expected status code 200")

	err = os.Remove(filepath.Join(suite.appCfg.OpenApiDefinitionsPath, workspaceName))
	require.NoError(suite.T(), err)

	// Wait until the handler is removed
	time.Sleep(sleepTime)

	// Attempt to access the URL again
	_, statusCode, _ = sendRequest(url, createPodMutation())
	require.Equal(suite.T(), http.StatusNotFound, statusCode, "Expected StatusNotFound after handler is removed")
}
