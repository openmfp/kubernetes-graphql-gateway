package server

import "github.com/openmfp/crd-gql-gateway/gateway"

func initDefaults(cfg *GatewayConfig) {
	if cfg.Service.Port == 0 {
		cfg.Service.Port = 8080
	}
	if cfg.Service.HealthPort == 0 {
		cfg.Service.HealthPort = 8081
	}
	if cfg.Service.MetricsPort == 0 {
		cfg.Service.MetricsPort = 8082
	}

	if cfg.Service.Endpoints.Graphql == "" {
		cfg.Service.Endpoints.Graphql = "graphql"
	}
	if cfg.Service.Endpoints.Healthz == "" {
		cfg.Service.Endpoints.Healthz = "healthz"
	}
	if cfg.Service.Endpoints.Readyz == "" {
		cfg.Service.Endpoints.Readyz = "readyz"
	}
	if cfg.Service.Endpoints.Subscription == "" {
		cfg.Service.Endpoints.Subscription = "subscription"
	}
	if cfg.Handler == nil {
		cfg.Handler = &gateway.HandlerConfig{}
	}
	if cfg.Handler.UserClaim == "" {
		cfg.Handler.UserClaim = "email"
	}
}
