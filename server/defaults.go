package server

import "github.com/openmfp/crd-gql-gateway/gateway"

func initDefaults(cfg *ManagerConfig) {
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Server.HealthPort == 0 {
		cfg.Server.HealthPort = 8081
	}
	if cfg.Server.MetricsPort == 0 {
		cfg.Server.MetricsPort = 8082
	}

	if cfg.Server.Endpoints.Graphql == "" {
		cfg.Server.Endpoints.Graphql = "graphql"
	}
	if cfg.Server.Endpoints.Healthz == "" {
		cfg.Server.Endpoints.Healthz = "healthz"
	}
	if cfg.Server.Endpoints.Readyz == "" {
		cfg.Server.Endpoints.Readyz = "readyz"
	}
	if cfg.Server.Endpoints.Subscription == "" {
		cfg.Server.Endpoints.Subscription = "subscription"
	}
	if cfg.Handler == nil {
		cfg.Handler = &gateway.HandlerConfig{}
	}
	if cfg.Handler.UserClaim == "" {
		cfg.Handler.UserClaim = "email"
	}
}
