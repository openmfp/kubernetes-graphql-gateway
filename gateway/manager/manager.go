package manager

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"

	"github.com/openmfp/golang-commons/logger"
	"github.com/pkg/errors"
	"k8s.io/client-go/rest"

	appConfig "github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"github.com/openmfp/kubernetes-graphql-gateway/gateway/manager/roundtripper"
	"github.com/openmfp/kubernetes-graphql-gateway/gateway/manager/targetcluster"
	"github.com/openmfp/kubernetes-graphql-gateway/gateway/manager/watcher"
)

type Provider interface {
	Start()
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

// Gateway orchestrates the domain-driven architecture with target clusters
type Gateway struct {
	log             *logger.Logger
	clusterRegistry ClusterManager
	clusterRouter   HTTPHandler
	schemaWatcher   SchemaWatcher
	appCfg          appConfig.Config
}

// NewGateway creates a new domain-driven Gateway instance
func NewGateway(log *logger.Logger, appCfg appConfig.Config) (*Gateway, error) {
	// Create round tripper factory
	roundTripperFactory := func(config *rest.Config) http.RoundTripper {
		// Create a simple HTTP transport that respects our TLS configuration
		tlsConfig := &tls.Config{
			InsecureSkipVerify: config.TLSClientConfig.Insecure,
			ServerName:         config.TLSClientConfig.ServerName,
		}

		log.Debug().
			Bool("insecure", tlsConfig.InsecureSkipVerify).
			Str("serverName", tlsConfig.ServerName).
			Int("caDataLen", len(config.TLSClientConfig.CAData)).
			Int("certDataLen", len(config.TLSClientConfig.CertData)).
			Msg("Creating TLS config for round tripper")

		// Add CA data if present
		if len(config.TLSClientConfig.CAData) > 0 {
			caCertPool := x509.NewCertPool()
			caCertPool.AppendCertsFromPEM(config.TLSClientConfig.CAData)
			tlsConfig.RootCAs = caCertPool
		}

		// Add client certificates if present
		if len(config.TLSClientConfig.CertData) > 0 && len(config.TLSClientConfig.KeyData) > 0 {
			cert, err := tls.X509KeyPair(config.TLSClientConfig.CertData, config.TLSClientConfig.KeyData)
			if err == nil {
				tlsConfig.Certificates = []tls.Certificate{cert}
			}
		}

		transport := &http.Transport{
			TLSClientConfig: tlsConfig,
		}
		return roundtripper.New(log, transport, appCfg.Gateway.UsernameClaim, appCfg.Gateway.ShouldImpersonate)
	}

	clusterRegistry := targetcluster.NewClusterRegistry(log, appCfg, roundTripperFactory)

	clusterRouter := targetcluster.NewClusterRouter(clusterRegistry, log, appCfg)

	schemaWatcher, err := watcher.NewFileWatcher(log, clusterRegistry)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create schema watcher")
	}

	gateway := &Gateway{
		log:             log,
		clusterRegistry: clusterRegistry,
		clusterRouter:   clusterRouter,
		schemaWatcher:   schemaWatcher,
		appCfg:          appCfg,
	}

	// Initialize schema watcher
	if err := schemaWatcher.Initialize(appCfg.OpenApiDefinitionsPath); err != nil {
		return nil, fmt.Errorf("failed to initialize schema watcher: %w", err)
	}

	log.Info().
		Str("definitions_path", appCfg.OpenApiDefinitionsPath).
		Str("port", appCfg.Gateway.Port).
		Msg("Gateway initialized successfully")

	return gateway, nil
}

// Start starts the gateway (implementation for Provider interface)
func (g *Gateway) Start() {}

// ServeHTTP delegates HTTP requests to the cluster router
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.clusterRouter.ServeHTTP(w, r)
}

// GetClusterStats returns statistics about managed clusters
func (g *Gateway) GetClusterStats() targetcluster.ClusterStats {
	return g.clusterRegistry.GetClusterStats()
}

// GetCluster returns a specific cluster by name
func (g *Gateway) GetCluster(name string) (*targetcluster.TargetCluster, bool) {
	return g.clusterRegistry.GetCluster(name)
}

// GetAllClusters returns all managed clusters
func (g *Gateway) GetAllClusters() map[string]*targetcluster.TargetCluster {
	return g.clusterRegistry.GetAllClusters()
}

// GetHealthyClusters returns only healthy clusters
func (g *Gateway) GetHealthyClusters() map[string]*targetcluster.TargetCluster {
	return g.clusterRegistry.GetHealthyClusters()
}

// Close gracefully shuts down the gateway and all its services
func (g *Gateway) Close() error {
	if g.schemaWatcher != nil {
		g.schemaWatcher.Close()
	}
	if g.clusterRegistry != nil {
		g.clusterRegistry.Close()
	}
	g.log.Info().Msg("The Gateway has been closed")
	return nil
}
