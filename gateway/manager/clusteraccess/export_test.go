package clusteraccess

import (
	"github.com/openmfp/golang-commons/logger/testlogger"
	appConfig "github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"github.com/openmfp/kubernetes-graphql-gateway/gateway/manager/roundtripper"
	"github.com/openmfp/kubernetes-graphql-gateway/gateway/resolver"
)

// NewClusterClientForTest creates a ClusterClient instance for testing
func NewClusterClientForTest() *ClusterClient {
	cfg := appConfig.Config{}
	cfg.LocalDevelopment = true
	cfg.OpenApiDefinitionsPath = "/tmp/test-schemas"

	resolvers := make(map[string]resolver.Provider)

	return NewClusterClient(cfg, testlogger.New().HideLogOutput().Logger, resolvers, roundtripper.New)
}
