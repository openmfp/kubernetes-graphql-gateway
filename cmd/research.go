package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/rs/zerolog"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-openapi/spec"
	"github.com/graphql-go/handler"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openmfp/crd-gql-gateway/gateway"
	"github.com/openmfp/crd-gql-gateway/research"
	"github.com/openmfp/golang-commons/logger"
)

// getFilteredResourceList returns a set of resource names allowed for filtering.
func getFilteredResourceList() map[string]struct{} {
	return map[string]struct{}{
		"io.k8s.api.core.v1.Pod":                   {},
		"io.k8s.api.core.v1.Endpoints":             {},
		"io.k8s.api.core.v1.Service":               {},
		"io.k8s.api.core.v1.Namespace":             {},
		"io.k8s.api.core.v1.Node":                  {},
		"io.k8s.api.core.v1.Secret":                {},
		"io.k8s.api.core.v1.ConfigMap":             {},
		"io.k8s.api.core.v1.PersistentVolume":      {},
		"io.k8s.api.core.v1.PersistentVolumeClaim": {},
		"io.k8s.api.core.v1.ServiceAccount":        {},
		"io.k8s.api.core.v1.Event":                 {},
		"io.k8s.api.core.v1.ReplicationController": {},
		"io.k8s.api.core.v1.LimitRange":            {},
		"io.k8s.api.core.v1.ResourceQuota":         {},
		"io.k8s.api.core.v1.PodTemplate":           {},
		"io.k8s.api.core.v1.ReplicaSet":            {},
		"io.k8s.api.apps.v1.Deployment":            {},
	}
}

var researchCmd = &cobra.Command{
	Use: "research",
	RunE: func(cmd *cobra.Command, args []string) error {
		start := time.Now()
		log, err := logger.New(logger.Config{
			Name:  "gateway",
			Level: zerolog.DebugLevel.String(),
		})
		if err != nil {
			return err
		}

		log.Info().Msg("Starting server...")

		cfg, err := getKubeConfig()
		if err != nil {
			return fmt.Errorf("error getting kubeconfig: %w", err)
		}

		runtimeClient, err := setupK8sClients(cfg)
		if err != nil {
			return err
		}

		discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
		if err != nil {
			return fmt.Errorf("error starting discovery client: %w", err)
		}

		resolver := research.NewResolver(log, runtimeClient)
		definitions, filteredDefinitions := getDefinitionsAndFilteredDefinitions(cfg)
		g, err := research.New(log, discoveryClient, definitions, filteredDefinitions, resolver)
		if err != nil {
			return fmt.Errorf("error creating gateway: %w", err)
		}

		gqlSchema, err := g.GetGraphqlSchema()
		if err != nil {
			return fmt.Errorf("error creating GraphQL schema: %w", err)
		}

		log.Info().Any("Setup took seconds:", time.Since(start).Seconds())
		log.Info().Msg("Server is running on http://localhost:3000/graphql")

		http.Handle("/graphql", gateway.Handler(gateway.HandlerConfig{
			Config: &handler.Config{
				Schema:     &gqlSchema,
				Pretty:     true,
				Playground: true,
			},
			UserClaim:   "mail",
			GroupsClaim: "groups",
		}))

		return http.ListenAndServe(":3000", nil)
	},
}

// setupK8sClients initializes and returns the runtime client and cache for Kubernetes.
func setupK8sClients(cfg *rest.Config) (client.Client, error) {
	if err := corev1.AddToScheme(scheme.Scheme); err != nil {
		return nil, fmt.Errorf("error adding core v1 to scheme: %w", err)
	}

	k8sCache, err := cache.New(cfg, cache.Options{Scheme: scheme.Scheme})
	if err != nil {
		return nil, err
	}

	go k8sCache.Start(context.Background())
	if !k8sCache.WaitForCacheSync(context.Background()) {
		return nil, fmt.Errorf("failed to sync cache")
	}

	runtimeClient, err := client.NewWithWatch(cfg, client.Options{
		Scheme: scheme.Scheme,
		Cache: &client.CacheOptions{
			Reader: k8sCache,
		},
	})
	return runtimeClient, err
}

// getDefinitionsAndFilteredDefinitions fetches OpenAPI schema definitions and filters resources.
func getDefinitionsAndFilteredDefinitions(config *rest.Config) (spec.Definitions, spec.Definitions) {
	httpClient, err := rest.HTTPClientFor(config)
	if err != nil {
		panic(fmt.Sprintf("Error creating HTTP client: %v", err))
	}

	url := config.Host + "/openapi/v2"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		panic(fmt.Sprintf("Error creating request: %v", err))
	}

	if config.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+config.BearerToken)
	}

	resp, err := httpClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		panic(fmt.Sprintf("Error fetching OpenAPI schema: %v", err))
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic(fmt.Sprintf("Error reading response body: %v", err))
	}

	var swagger spec.Swagger
	if err := json.Unmarshal(body, &swagger); err != nil {
		panic(fmt.Sprintf("Error unmarshalling OpenAPI schema: %v", err))
	}
	if err := spec.ExpandSpec(&swagger, nil); err != nil {
		panic(fmt.Sprintf("Error expanding OpenAPI schema: %v", err))
	}

	filteredDefinitions := filterDefinitions(swagger.Definitions, getFilteredResourceList())
	return swagger.Definitions, filteredDefinitions
}

// filterDefinitions filters definitions based on allowed resources.
func filterDefinitions(definitions spec.Definitions, allowedResources map[string]struct{}) spec.Definitions {
	filtered := make(map[string]spec.Schema)
	for key, val := range definitions {
		if _, ok := allowedResources[key]; ok {
			filtered[key] = val
		}
	}
	return filtered
}

// getKubeConfig returns a Kubernetes client configuration.
func getKubeConfig() (*rest.Config, error) {
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		home := os.Getenv("HOME")
		if home == "" {
			return nil, fmt.Errorf("cannot find kubeconfig")
		}
		kubeconfigPath = filepath.Join(home, ".kube", "config")
	}
	return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
}
