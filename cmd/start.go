package cmd

import (
	"fmt"
	"net/http"

	"context"

	"github.com/graphql-go/handler"
	"github.com/spf13/cobra"

	"github.com/openmfp/crd-gql-gateway/gateway"
	"k8s.io/apimachinery/pkg/runtime"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	jenxv1 "github.tools.sap/automaticd/automaticd/operators/jenx/api/v1"
	jirav1alpha1 "github.tools.sap/automaticd/automaticd/operators/jira/api/v1alpha1"
	authzv1 "k8s.io/api/authorization/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	accounts "github.com/openmfp/account-operator/api/v1alpha1"
)

var startCmd = &cobra.Command{
	Use: "start",
	RunE: func(cmd *cobra.Command, args []string) error {

		cfg := controllerruntime.GetConfigOrDie()

		schema := runtime.NewScheme()
		apiextensionsv1.AddToScheme(schema)
		authzv1.AddToScheme(schema)

		jirav1alpha1.AddToScheme(schema)
		jenxv1.AddToScheme(schema)

		accounts.AddToScheme(schema)

		k8sCache, err := cache.New(cfg, cache.Options{
			Scheme: schema,
		})
		if err != nil {
			return err
		}

		go func() {
			k8sCache.Start(context.Background())
		}()

		if !k8sCache.WaitForCacheSync(context.Background()) {
			panic("no cache sync")
		}

		cfg.Wrap(gateway.NewImpersonationTransport)

		cl, err := client.NewWithWatch(cfg, client.Options{
			Scheme: schema,
			Cache: &client.CacheOptions{
				Reader: k8sCache,
			},
		})
		if err != nil {
			return err
		}

		gqlSchema, err := gateway.New(cmd.Context(), gateway.Config{
			Client: cl,
		})
		if err != nil {
			return err
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
