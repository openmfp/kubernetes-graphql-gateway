package gateway_test

import (
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/graphql-go/graphql"
	"github.com/openmfp/golang-commons/logger"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/kcp"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/openmfp/account-operator/api/v1alpha1"
	appConfig "github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"github.com/openmfp/kubernetes-graphql-gateway/gateway/manager"
)

// Initialize the logger for the test suite
// This is necessary to avoid the "[controller-runtime] log.SetLogger(...) was never called" error
// when running the tests
func TestMain(m *testing.M) {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	os.Exit(m.Run())
}

type CommonTestSuite struct {
	suite.Suite
	testEnv       *envtest.Environment
	log           *logger.Logger
	restCfg       *rest.Config
	appCfg        appConfig.Config
	runtimeClient client.WithWatch
	graphqlSchema graphql.Schema
	manager       manager.Provider
	server        *httptest.Server

	LocalDevelopment           bool
	AuthenticateSchemaRequests bool

	staticTokenFile string
	staticToken     string
}

func TestCommonTestSuite(t *testing.T) {
	suite.Run(t, new(CommonTestSuite))
}

func (suite *CommonTestSuite) SetupSuite() {
	suite.LocalDevelopment = true
}

func (suite *CommonTestSuite) SetupTest() {
	runtimeScheme := runtime.NewScheme()
	utilruntime.Must(v1alpha1.AddToScheme(runtimeScheme))
	utilruntime.Must(appsv1.AddToScheme(runtimeScheme))
	utilruntime.Must(v1.AddToScheme(runtimeScheme))
	utilruntime.Must(corev1.AddToScheme(runtimeScheme))

	var err error

	// 1. Generate a static token and write it to a file
	suite.staticToken = "test-token-123"
	tokenFile, err := os.CreateTemp("", "static-token.csv")
	require.NoError(suite.T(), err)
	_, err = tokenFile.WriteString(fmt.Sprintf("%s,admin,admin,system:masters\n", suite.staticToken))
	require.NoError(suite.T(), err)
	require.NoError(suite.T(), tokenFile.Close())
	suite.staticTokenFile = tokenFile.Name()

	// 2. Prepare envtest.Environment and configure the API server with the token file
	suite.testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("testdata", "crd"),
		},
	}
	// Add the token-auth-file argument before starting the environment
	suite.testEnv.ControlPlane.GetAPIServer().Configure().Append("token-auth-file", suite.staticTokenFile)

	suite.restCfg, err = suite.testEnv.Start()
	require.NoError(suite.T(), err)

	// 3. Set BearerToken in restCfg
	suite.restCfg.BearerToken = suite.staticToken

	suite.appCfg.OpenApiDefinitionsPath, err = os.MkdirTemp("", "watchedDir")
	require.NoError(suite.T(), err)

	suite.appCfg.LocalDevelopment = suite.LocalDevelopment
	suite.appCfg.Gateway.Cors.Enabled = true
	suite.appCfg.IntrospectionAuthentication = suite.AuthenticateSchemaRequests

	suite.log, err = logger.New(logger.DefaultConfig())
	require.NoError(suite.T(), err)

	suite.runtimeClient, err = kcp.NewClusterAwareClientWithWatch(suite.restCfg, client.Options{
		Scheme: runtimeScheme,
	})
	require.NoError(suite.T(), err)

	// Gateway automatically loads schemas from files

	suite.manager, err = manager.NewGateway(suite.log, suite.appCfg)
	require.NoError(suite.T(), err)

	suite.server = httptest.NewServer(suite.manager)
}

func (suite *CommonTestSuite) TearDownTest() {
	require.NoError(suite.T(), os.RemoveAll(suite.appCfg.OpenApiDefinitionsPath))
	require.NoError(suite.T(), suite.testEnv.Stop())
	suite.server.Close()
	if suite.staticTokenFile != "" {
		os.Remove(suite.staticTokenFile)
	}
}
