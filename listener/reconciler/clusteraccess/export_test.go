package clusteraccess

import (
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openmfp/golang-commons/logger"
	gatewayv1alpha1 "github.com/openmfp/kubernetes-graphql-gateway/common/apis/v1alpha1"
)

// Exported functions for testing private functions

// Config builder exports
func ExtractCAData(ca *gatewayv1alpha1.CAConfig, k8sClient client.Client) ([]byte, error) {
	return extractCAData(ca, k8sClient)
}

func ConfigureAuthentication(config *rest.Config, auth *gatewayv1alpha1.AuthConfig, k8sClient client.Client) error {
	return configureAuthentication(config, auth, k8sClient)
}

func ExtractAuthFromKubeconfig(config *rest.Config, authInfo *api.AuthInfo) error {
	return extractAuthFromKubeconfig(config, authInfo)
}

// Metadata injector exports
func InjectClusterMetadata(schemaJSON []byte, clusterAccess gatewayv1alpha1.ClusterAccess, k8sClient client.Client, log *logger.Logger) ([]byte, error) {
	return injectClusterMetadata(schemaJSON, clusterAccess, k8sClient, log)
}

func ExtractCADataForMetadata(ca *gatewayv1alpha1.CAConfig, k8sClient client.Client) ([]byte, error) {
	return extractCADataForMetadata(ca, k8sClient)
}

func ExtractAuthDataForMetadata(auth *gatewayv1alpha1.AuthConfig, k8sClient client.Client) (map[string]interface{}, error) {
	return extractAuthDataForMetadata(auth, k8sClient)
}

func ExtractCAFromKubeconfig(kubeconfigB64 string, log *logger.Logger) []byte {
	return extractCAFromKubeconfig(kubeconfigB64, log)
}

// Subroutines exports
type GenerateSchemaSubroutine = generateSchemaSubroutine

func NewGenerateSchemaSubroutine(reconciler *ExportedClusterAccessReconciler) *GenerateSchemaSubroutine {
	return &generateSchemaSubroutine{reconciler: reconciler}
}

func (s *generateSchemaSubroutine) RestMapperFromConfig(cfg *rest.Config) (interface{}, error) {
	rm, err := s.restMapperFromConfig(cfg)
	return rm, err
}

// Type and constant exports
type ExportedCRDStatus = CRDStatus
type ExportedClusterAccessReconciler = ClusterAccessReconciler

const (
	ExportedCRDNotRegistered = CRDNotRegistered
	ExportedCRDRegistered    = CRDRegistered
)

// Error exports
var (
	ExportedErrCRDNotRegistered = ErrCRDNotRegistered
	ExportedErrCRDCheckFailed   = ErrCRDCheckFailed
)
