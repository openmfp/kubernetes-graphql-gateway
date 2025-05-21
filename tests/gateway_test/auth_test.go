package gateway_test

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"net/http"
	"path/filepath"
)

func (suite *CommonTestSuite) TestTokenValidation() {
	suite.LocalDevelopment = false
	suite.SetupTest()
	defer func() {
		suite.LocalDevelopment = true
		suite.TearDownTest()
	}()

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
