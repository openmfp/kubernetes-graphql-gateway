package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/graphql-go/handler"
	"github.com/openmfp/crd-gql-gateway/gateway"
	"github.com/openmfp/crd-gql-gateway/transport"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"k8s.io/apimachinery/pkg/runtime"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

type ManagerConfig struct {
	Server struct {
		Port        int
		HealthPort  int
		MetricsPort int
		Endpoints   struct {
			Healthz      string
			Readyz       string
			Graphql      string
			Subscription string
		}
		ShutdownTimeout time.Duration
	}
	Handler *gateway.HandlerConfig
}

func InitManager(ctx context.Context, schema *runtime.Scheme, cfg *ManagerConfig) (controllerruntime.Manager, error) {
	initDefaults(cfg)

	mgr, err := manager.New(controllerruntime.GetConfigOrDie(), manager.Options{
		HealthProbeBindAddress: fmt.Sprintf(":%d", cfg.Server.HealthPort),
		Metrics: server.Options{
			BindAddress: fmt.Sprintf(":%d", cfg.Server.MetricsPort),
		},
		Scheme:         schema,
		LeaderElection: false,
	})
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()

	withWatch, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: schema,
		Cache: &client.CacheOptions{
			Reader: mgr.GetCache(),
		},
	})
	if err != nil {
		return nil, err
	}

	gqlSchema, err := gateway.New(ctx, gateway.Config{
		Client: withWatch,
		Reader: mgr.GetAPIReader(),
	})
	if err != nil {
		return nil, err
	}

	graphqlUrl := fmt.Sprintf("/%s", cfg.Server.Endpoints.Graphql)
	mux.Handle(
		graphqlUrl,
		otelhttp.NewHandler(
			gateway.Handler(gateway.HandlerConfig{
				UserClaim: cfg.Handler.UserClaim,
				Config: &handler.Config{
					Schema:     &gqlSchema,
					Pretty:     cfg.Handler.Pretty,
					Playground: cfg.Handler.Playground,
				},
			}),
			graphqlUrl,
		),
	)
	subscriptionUrl := fmt.Sprintf("/%s", cfg.Server.Endpoints.Subscription)
	mux.Handle(subscriptionUrl, otelhttp.NewHandler(transport.New(gqlSchema, cfg.Handler.UserClaim), subscriptionUrl))
	err = mgr.Add(&manager.Server{
		Server: &http.Server{
			Handler: mux,
			Addr:    fmt.Sprintf(":%d", cfg.Server.Port),
		},
		Name:            "gateway",
		ShutdownTimeout: &cfg.Server.ShutdownTimeout,
	})
	if err != nil {
		return nil, err
	}

	if err := mgr.AddHealthzCheck(cfg.Server.Endpoints.Healthz, healthz.Ping); err != nil {
		return nil, err
	}
	if err := mgr.AddReadyzCheck(cfg.Server.Endpoints.Readyz, healthz.Ping); err != nil {
		return nil, err
	}
	return mgr, nil
}
