package tests

import (
	"github.com/openmfp/crd-gql-gateway/internal/manager"
	"github.com/openmfp/golang-commons/logger"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"k8s.io/client-go/rest"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"testing"
	"time"
)

type CommonTestSuite struct {
	suite.Suite
	testEnv    *envtest.Environment
	log        *logger.Logger
	cfg        *rest.Config
	watchedDir string
	manager    manager.Provider
	server     *httptest.Server
}

func TestCommonTestSuite(t *testing.T) {
	suite.Run(t, new(CommonTestSuite))
}

func (suite *CommonTestSuite) SetupTest() {
	var err error
	suite.testEnv = &envtest.Environment{}
	suite.cfg, err = suite.testEnv.Start()
	require.NoError(suite.T(), err)

	suite.watchedDir, err = os.MkdirTemp("", "watchedDir")
	require.NoError(suite.T(), err)

	logCfg := logger.DefaultConfig()
	logCfg.Name = "crdGateway"
	suite.log, err = logger.New(logCfg)
	require.NoError(suite.T(), err)

	suite.manager, err = manager.NewManager(suite.log, suite.cfg, suite.watchedDir)
	require.NoError(suite.T(), err)

	suite.server = httptest.NewServer(suite.manager)
}

func (suite *CommonTestSuite) TearDownTest() {
	require.NoError(suite.T(), os.RemoveAll(suite.watchedDir))
	require.NoError(suite.T(), suite.testEnv.Stop())
	suite.server.Close()
}

// writeToFile adds a new file to the watched directory which will trigger schema generation
func (suite *CommonTestSuite) writeToFile(sourceName, dest string) {
	specFilePath := filepath.Join(suite.watchedDir, dest)

	sourceSpecFilePath := filepath.Join("testdata", sourceName)

	specContent, err := os.ReadFile(sourceSpecFilePath)
	require.NoError(suite.T(), err)

	err = os.WriteFile(specFilePath, specContent, 0644)
	require.NoError(suite.T(), err)

	time.Sleep(sleepTime) // let's give some time to the manager to process the file and create a url
}
