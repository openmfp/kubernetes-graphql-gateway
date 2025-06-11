package clusteraccess

import (
	"context"
	"errors"

	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openmfp/golang-commons/logger"
	gatewayv1alpha1 "github.com/openmfp/kubernetes-graphql-gateway/common/apis/gateway/v1alpha1"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/apischema"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/workspacefile"
)

func PreReconcileWithClusterAccess(
	cr *apischema.CRDResolver,
	io workspacefile.IOHandler,
	client client.Client,
	log *logger.Logger,
) error {
	ctx := context.Background()

	log.Info().Msg("starting PreReconcileWithClusterAccess")

	// List all ClusterAccess resources
	clusterAccessList := &gatewayv1alpha1.ClusterAccessList{}

	if err := client.List(ctx, clusterAccessList); err != nil {
		log.Error().Err(err).Msg("failed to list ClusterAccess resources")
		return errors.Join(errors.New("failed to list ClusterAccess resources"), err)
	}

	log.Info().Int("count", len(clusterAccessList.Items)).Msg("found ClusterAccess resources")

	// For each ClusterAccess resource, generate schema for target cluster
	for _, item := range clusterAccessList.Items {
		clusterAccessName := item.GetName()
		log.Info().Str("clusterAccess", clusterAccessName).Msg("processing ClusterAccess resource")

		// Extract target cluster config from ClusterAccess spec
		targetConfig, clusterName, err := buildTargetClusterConfigFromTyped(item, client)
		if err != nil {
			log.Error().Err(err).Str("clusterAccess", clusterAccessName).Msg("failed to build target cluster config")
			continue
		}

		log.Info().Str("clusterAccess", clusterAccessName).Str("host", targetConfig.Host).Str("clusterName", clusterName).Msg("extracted target cluster config")

		// Create discovery client for target cluster
		targetDiscovery, err := discovery.NewDiscoveryClientForConfig(targetConfig)
		if err != nil {
			log.Error().Err(err).Str("clusterAccess", clusterAccessName).Msg("failed to create discovery client for target cluster")
			continue
		}

		log.Info().Str("clusterAccess", clusterAccessName).Msg("created discovery client for target cluster")

		// Create REST mapper for target cluster
		targetRM, err := restMapperFromConfig(targetConfig)
		if err != nil {
			log.Error().Err(err).Str("clusterAccess", clusterAccessName).Msg("failed to create REST mapper for target cluster")
			continue
		}

		log.Info().Str("clusterAccess", clusterAccessName).Msg("created REST mapper for target cluster")

		// Create schema resolver for target cluster
		targetResolver := &apischema.CRDResolver{
			DiscoveryInterface: targetDiscovery,
			RESTMapper:         targetRM,
		}

		log.Info().Str("clusterAccess", clusterAccessName).Msg("attempting to resolve schema for target cluster")

		// Generate schema for target cluster
		JSON, err := targetResolver.Resolve()
		if err != nil {
			log.Error().Err(err).Str("clusterAccess", clusterAccessName).Msg("failed to resolve schema for target cluster")
			continue
		}

		log.Info().Str("clusterAccess", clusterAccessName).Int("schemaSize", len(JSON)).Msg("successfully resolved schema for target cluster")

		// Create the complete schema file with x-cluster-metadata
		schemaWithMetadata, err := injectClusterMetadata(JSON, item, client, log)
		if err != nil {
			log.Error().Err(err).Str("clusterAccess", clusterAccessName).Msg("failed to inject cluster metadata into schema")
			continue
		}

		// Write schema to file using cluster name from path or resource name
		if err := io.Write(schemaWithMetadata, clusterName); err != nil {
			log.Error().Err(err).Str("clusterAccess", clusterAccessName).Str("clusterName", clusterName).Msg("failed to write schema for target cluster")
			continue
		}

		log.Info().Str("clusterAccess", clusterAccessName).Str("clusterName", clusterName).Msg("successfully generated schema for target cluster")
	}

	log.Info().Msg("completed PreReconcileWithClusterAccess")
	return nil
}
