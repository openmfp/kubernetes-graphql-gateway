package cmd

import (
	"encoding/json"
	"fmt"
	"github.com/go-openapi/spec"
	"github.com/graphql-go/handler"
	"github.com/openmfp/crd-gql-gateway/gateway"
	"github.com/openmfp/crd-gql-gateway/research"
	"github.com/spf13/cobra"
	"io/ioutil"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"net/http"
	"os"
	"path/filepath"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var researchCmd = &cobra.Command{
	Use: "research",
	RunE: func(cmd *cobra.Command, args []string) error {
		err := corev1.AddToScheme(scheme.Scheme)
		if err != nil {
			fmt.Printf("Error adding core v1 to scheme: %v\n", err)
			os.Exit(1)
		}

		config, err := getKubeConfig()
		if err != nil {
			fmt.Printf("Error getting kubeconfig: %v\n", err)
			os.Exit(1)
		}

		httpClient, err := rest.HTTPClientFor(config)
		if err != nil {
			fmt.Printf("Error creating HTTP client: %v\n", err)
			os.Exit(1)
		}

		url := config.Host + "/openapi/v2"

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			fmt.Printf("Error creating request: %v\n", err)
			os.Exit(1)
		}

		// Add authentication headers if needed
		if config.BearerToken != "" {
			req.Header.Set("Authorization", "Bearer "+config.BearerToken)
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			fmt.Printf("Error making request: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			fmt.Printf("Unexpected status code: %d\n", resp.StatusCode)
			os.Exit(1)
		}

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			fmt.Printf("Error reading response body: %v\n", err)
			os.Exit(1)
		}

		var swagger spec.Swagger
		err = json.Unmarshal(body, &swagger)
		if err != nil {
			fmt.Printf("Error unmarshalling OpenAPI schema: %v\n", err)
			os.Exit(1)
		}

		err = spec.ExpandSpec(&swagger, nil)
		if err != nil {
			fmt.Printf("Error expanding OpenAPI schema: %v\n", err)
			os.Exit(1)
		}

		filteredResources := map[string]struct{}{
			"io.k8s.api.core.v1.Pod": {},
			// "io.k8s.api.core.v1.Endpoints": {},
			// "io.k8s.api.core.v1.Service":   {},
		}

		filteredDefinitions := make(map[string]spec.Schema)
		for key, val := range swagger.Definitions {
			if _, ok := filteredResources[key]; ok {
				filteredDefinitions[key] = val
			}
		}

		schema := runtime.NewScheme()
		runtimeClient, err := client.NewWithWatch(config, client.Options{
			Scheme: schema,
			// Cache: &client.CacheOptions{
			// 	Reader: k8sCache,
			// },
		})

		resolver := research.NewResolver(runtimeClient)
		gqlSchema, err := research.GetGraphqlSchema(filteredDefinitions, resolver)
		if err != nil {
			fmt.Println("Error creating GraphQL schema")
			panic(err)
		}

		fmt.Println("Server is running on http://localhost:3000/graphql")

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

// getKubeConfig retrieves Kubernetes configuration
func getKubeConfig() (*rest.Config, error) {
	var kubeconfigPath string
	if envKubeconfig := os.Getenv("KUBECONFIG"); envKubeconfig != "" {
		kubeconfigPath = envKubeconfig
	} else if home := os.Getenv("HOME"); home != "" {
		kubeconfigPath = filepath.Join(home, ".kube", "config")
	} else {
		return nil, fmt.Errorf("cannot find kubeconfig")
	}

	return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
}
