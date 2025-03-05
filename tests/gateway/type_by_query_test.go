package gateway

import (
	"context"
	"github.com/openmfp/crd-gql-gateway/gateway/manager"
	"github.com/openmfp/crd-gql-gateway/gateway/resolver"
	"github.com/openmfp/crd-gql-gateway/gateway/schema"
	"github.com/stretchr/testify/require"
	"testing"

	"github.com/graphql-go/graphql"
	"github.com/openmfp/golang-commons/logger"
	"github.com/stretchr/testify/assert"
)

func getGateway() (*schema.Gateway, error) {
	log, err := logger.New(logger.DefaultConfig())
	if err != nil {
		return nil, err
	}
	definitions, err := manager.ReadDefinitionFromFile("./testdata/kubernetes")
	if err != nil {
		return nil, err
	}

	return schema.New(log, definitions, resolver.New(log, nil))
}

func TestTypeByCategory(t *testing.T) {
	g, err := getGateway()
	require.NoError(t, err)

	res := graphql.Do(graphql.Params{
		Context:       context.Background(),
		Schema:        *g.GetSchema(),
		RequestString: typeByCategoryQuery(),
	})

	require.Nil(t, res.Errors)
	require.NotNil(t, res.Data)

	data := res.Data.(map[string]interface{})
	typeByCategory := data["typeByCategory"].([]interface{})
	firstItem := typeByCategory[0].(map[string]interface{})

	assert.Equal(t, "networking_istio_io", firstItem["group"])
}

func typeByCategoryQuery() string {
	return `
		query{
		  typeByCategory(name: "istio-io"){
			group
			version
			kind
			scope
		  }
		}`
}
