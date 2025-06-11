package standard

import (
	"errors"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/openmfp/golang-commons/logger"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/apischema"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/controller"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/reconciler"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/workspacefile"
)

const (
	kubernetesClusterName = "kubernetes" // Used as schema file name for standard k8s cluster
)

var (
	ErrCreateIOHandler  = errors.New("failed to create IO Handler")
	ErrCreateRestMapper = errors.New("failed to create rest mapper")
	ErrCreateHTTPClient = errors.New("failed to create http client")
	ErrGenerateSchema   = errors.New("failed to generate OpenAPI Schema")
)

// NewReconciler creates a reconciler for standard non-KCP clusters
// This uses a hardcoded "kubernetes" filename for the schema file
func NewReconciler(
	opts reconciler.ReconcilerOpts,
	discoveryInterface discovery.DiscoveryInterface,
	log *logger.Logger,
) (reconciler.CustomReconciler, error) {
	ioHandler, err := workspacefile.NewIOHandler(opts.OpenAPIDefinitionsPath)
	if err != nil {
		return nil, errors.Join(ErrCreateIOHandler, err)
	}

	rm, err := restMapperFromConfig(opts.Config)
	if err != nil {
		return nil, err
	}

	schemaResolver := &apischema.CRDResolver{
		DiscoveryInterface: discoveryInterface,
		RESTMapper:         rm,
	}

	// For standard clusters, use the preReconcile approach with "kubernetes" filename
	if err = preReconcile(schemaResolver, ioHandler); err != nil {
		return nil, errors.Join(ErrGenerateSchema, err)
	}

	log.Info().Str("clusterName", kubernetesClusterName).Msg("Generated schema for standard cluster connection")
	return controller.NewCRDReconciler(kubernetesClusterName, opts.Client, schemaResolver, ioHandler, log), nil
}

func restMapperFromConfig(cfg *rest.Config) (meta.RESTMapper, error) {
	httpClt, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return nil, errors.Join(ErrCreateHTTPClient, err)
	}
	rm, err := apiutil.NewDynamicRESTMapper(cfg, httpClt)
	if err != nil {
		return nil, errors.Join(ErrCreateRestMapper, err)
	}

	return rm, nil
}
