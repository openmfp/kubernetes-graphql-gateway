package cmd

import (
	"crypto/tls"
	"fmt"
	"os"

	kcpapis "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	kcpcore "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	kcptenancy "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"github.com/spf13/cobra"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/openmfp/kubernetes-graphql-gateway/common/config"
	"github.com/openmfp/kubernetes-graphql-gateway/listener/kcp"
)

func init() {
	rootCmd.AddCommand(listenCmd)
}

var (
	scheme               = runtime.NewScheme()
	setupLog             = ctrl.Log.WithName("setup")
	webhookServer        webhook.Server
	metricsServerOptions metricsserver.Options
	appCfg               *config.Config
)

var listenCmd = &cobra.Command{
	Use:     "listener",
	Example: "KUBECONFIG=<path to kubeconfig file> go run . listener",
	PreRun: func(cmd *cobra.Command, args []string) {
		utilruntime.Must(clientgoscheme.AddToScheme(scheme))
		utilruntime.Must(kcpapis.AddToScheme(scheme))
		utilruntime.Must(kcpcore.AddToScheme(scheme))
		utilruntime.Must(kcptenancy.AddToScheme(scheme))
		utilruntime.Must(apiextensionsv1.AddToScheme(scheme))

		opts := zap.Options{Development: true}
		ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

		var err error
		appCfg, err = config.NewFromEnv()
		if err != nil {
			setupLog.Error(err, "failed to get operator flags from env, exiting...")
			os.Exit(1)
		}

		disableHTTP2 := func(c *tls.Config) {
			setupLog.Info("disabling http/2")
			c.NextProtos = []string{"http/1.1"}
		}

		var tlsOpts []func(*tls.Config)
		if !appCfg.EnableHTTP2 {
			tlsOpts = []func(c *tls.Config){disableHTTP2}
		}

		webhookServer = webhook.NewServer(webhook.Options{TLSOpts: tlsOpts})
		metricsServerOptions = metricsserver.Options{
			BindAddress:   appCfg.MetricsAddr,
			SecureServing: appCfg.SecureMetrics,
			TLSOpts:       tlsOpts,
		}
		if appCfg.SecureMetrics {
			metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
		}
	},
	Run: func(cmd *cobra.Command, args []string) {
		cfg := ctrl.GetConfigOrDie()

		// Base options for both managers
		baseOpts := ctrl.Options{
			Scheme:           scheme,
			Metrics:          metricsServerOptions,
			WebhookServer:    webhookServer,
			LeaderElection:   appCfg.EnableLeaderElection,
			LeaderElectionID: "72231e1f.openmfp.io",
		}

		rootMgrOpts := baseOpts
		rootMgrOpts.HealthProbeBindAddress = appCfg.ProbeAddr // e.g., ":8081"

		vwMgrOpts := baseOpts
		vwMgrOpts.HealthProbeBindAddress = ":9444" // Distinct port for vwMgr

		clt, err := client.New(cfg, client.Options{
			Scheme: scheme,
		})
		if err != nil {
			setupLog.Error(err, "failed to create client from config")
			os.Exit(1)
		}

		mf := &kcp.ManagerFactory{
			IsKCPEnabled: appCfg.EnableKcp,
		}

		rootMgr, vwMgr, err := mf.NewManagers(cfg, rootMgrOpts, vwMgrOpts, clt)
		if err != nil {
			setupLog.Error(err, "unable to start managers")
			os.Exit(1)
		}

		reconcilerOptsBase := kcp.ReconcilerOpts{
			Config:                 cfg,
			Scheme:                 scheme,
			Client:                 clt,
			OpenAPIDefinitionsPath: appCfg.OpenApiDefinitionsPath,
		}

		factory := kcp.NewReconcilerFactory(appCfg)

		if appCfg.EnableKcp {
			apiBindingReconcilerOpts := reconcilerOptsBase
			apiBindingReconcilerOpts.Config = rootMgr.GetConfig()
			apiBindingReconciler, err := factory.NewReconciler(apiBindingReconcilerOpts)
			if err != nil {
				setupLog.Error(err, "unable to instantiate root reconciler")
				os.Exit(1)
			}
			if err := apiBindingReconciler.SetupWithManager(rootMgr, "root"); err != nil {
				setupLog.Error(err, "unable to create controller for root manager")
				os.Exit(1)
			}

			virtualWorkspaceReconcilerOpts := reconcilerOptsBase
			virtualWorkspaceReconcilerOpts.Config = vwMgr.GetConfig()
			virtualWorkspaceReconciler, err := factory.NewReconciler(virtualWorkspaceReconcilerOpts)
			if err != nil {
				setupLog.Error(err, "unable to instantiate virtual workspace reconciler")
				os.Exit(1)
			}
			if err := virtualWorkspaceReconciler.SetupWithManager(vwMgr, "virtualworkspace"); err != nil {
				setupLog.Error(err, "unable to create controller for virtual workspace manager")
				os.Exit(1)
			}
		} else {
			// Single reconciler for non-KCP mode
			reconciler, err := factory.NewReconciler(reconcilerOptsBase)
			if err != nil {
				setupLog.Error(err, "unable to instantiate reconciler")
				os.Exit(1)
			}
			if err := reconciler.SetupWithManager(rootMgr, "root"); err != nil {
				setupLog.Error(err, "unable to create controller")
				os.Exit(1)
			}
		}

		// Set up health and readiness checks for both managers
		for _, mgr := range []ctrl.Manager{rootMgr, vwMgr} {
			if mgr == nil {
				continue
			}
			if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
				setupLog.Error(err, "unable to set up health check")
				os.Exit(1)
			}
			if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
				setupLog.Error(err, "unable to set up ready check")
				os.Exit(1)
			}
		}

		setupLog.Info("starting managers")
		signalHandler := ctrl.SetupSignalHandler()

		errChan := make(chan error, 2)
		go func() {
			if err := rootMgr.Start(signalHandler); err != nil {
				errChan <- fmt.Errorf("problem running root manager: %w", err)
			}
		}()
		if vwMgr != nil {
			go func() {
				if err := vwMgr.Start(signalHandler); err != nil {
					errChan <- fmt.Errorf("problem running virtual workspace manager: %w", err)
				}
			}()
		}

		// Wait for any manager to fail
		if err := <-errChan; err != nil {
			setupLog.Error(err, "manager failed")
			os.Exit(1)
		}
	},
}
