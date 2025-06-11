package standard

import (
	"bytes"
	"errors"
	"io/fs"

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
	ErrResolveSchema    = errors.New("failed to resolve server JSON schema")
	ErrReadJSON         = errors.New("failed to read JSON from filesystem")
	ErrWriteJSON        = errors.New("failed to write JSON to filesystem")
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

// preReconcile generates schema directly from the current cluster (original main branch approach)
func preReconcile(
	cr *apischema.CRDResolver,
	io workspacefile.IOHandler,
) error {
	actualJSON, err := cr.Resolve()
	if err != nil {
		return errors.Join(ErrResolveSchema, err)
	}

	savedJSON, err := io.Read(kubernetesClusterName)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return io.Write(actualJSON, kubernetesClusterName)
		}
		return errors.Join(ErrReadJSON, err)
	}

	if !bytes.Equal(actualJSON, savedJSON) {
		if err := io.Write(actualJSON, kubernetesClusterName); err != nil {
			return errors.Join(ErrWriteJSON, err)
		}
	}

	return nil
}
