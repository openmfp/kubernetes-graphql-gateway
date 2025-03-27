package gateway_test

import (
	"context"
	"fmt"
	"github.com/openmfp/account-operator/api/v1alpha1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net/http"
	"path/filepath"
	"testing"
)

// TestSortBy tests the sorting functionality of accounts by displayName
func (suite *CommonTestSuite) TestSortBy() {
	workspaceName := "myWorkspace"
	url := fmt.Sprintf("%s/%s/graphql", suite.server.URL, workspaceName)

	require.NoError(suite.T(), writeToFile(
		filepath.Join("testdata", "kubernetes"),
		filepath.Join(suite.appCfg.OpenApiDefinitionsPath, workspaceName),
	))

	suite.createAccountsForSorting(context.Background())

	suite.T().Run("accounts_sorted_by_default", func(t *testing.T) {
		listResp, statusCode, err := sendRequest(url, listAccountsQuery(false))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, statusCode, "Expected status code 200")
		require.Nil(t, listResp.Errors, "GraphQL errors: %v", listResp.Errors)

		accounts := listResp.Data.CoreOpenmfpOrg.Accounts
		require.Len(t, accounts, 4, "Expected 4 accounts")

		expectedOrder := []string{"account-a", "account-b", "account-c", "account-d"}
		for i, oneAccount := range accounts {
			displayName := oneAccount.Metadata.Name
			require.Equal(t, expectedOrder[i], displayName,
				"Account at position %d should have displayName %s, got %s",
				i, expectedOrder[i], displayName)
		}
	})

	// Test sorted case
	suite.T().Run("accounts_sorted_by_displayName", func(t *testing.T) {
		listResp, statusCode, err := sendRequest(url, listAccountsQuery(true))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, statusCode, "Expected status code 200")
		require.Nil(t, listResp.Errors, "GraphQL errors: %v", listResp.Errors)

		accounts := listResp.Data.CoreOpenmfpOrg.Accounts
		require.Len(t, accounts, 4, "Expected 4 accounts")

		// Verify accounts are sorted by displayName in ascending order
		expectedOrder := []string{"account-d", "account-c", "account-b", "account-a"}
		for i, oneAccount := range accounts {
			displayName := oneAccount.Metadata.Name
			require.Equal(t, expectedOrder[i], displayName,
				"Account at position %d should have displayName %s, got %s",
				i, expectedOrder[i], displayName)
		}
	})
}

func (suite *CommonTestSuite) createAccountsForSorting(ctx context.Context) {
	accounts := map[string]string{ // map[name]displayName
		"account-a": "displayName-D",
		"account-b": "displayName-C",
		"account-c": "displayName-B",
		"account-d": "displayName-A",
	}

	for name, displayName := range accounts {
		err := suite.runtimeClient.Create(ctx, &v1alpha1.Account{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: v1alpha1.AccountSpec{
				Type:        v1alpha1.AccountTypeAccount,
				DisplayName: displayName,
			},
		})
		require.NoError(suite.T(), err)
	}
}
