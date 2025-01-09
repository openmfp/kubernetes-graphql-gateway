/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.

	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	kcpctrl "sigs.k8s.io/controller-runtime/pkg/kcp"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	kcpapis "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	kcpcore "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	kcptenancy "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"

	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"github.com/openmfp/crd-gql-gateway/kcp-listener/internal/apischema"
	"github.com/openmfp/crd-gql-gateway/kcp-listener/internal/controller"
	"github.com/openmfp/crd-gql-gateway/kcp-listener/internal/discoveryclient"
	"github.com/openmfp/crd-gql-gateway/kcp-listener/internal/workspacefile"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(kcpapis.AddToScheme(scheme))
	utilruntime.Must(kcpcore.AddToScheme(scheme))
	utilruntime.Must(kcptenancy.AddToScheme(scheme))
	utilruntime.Must(apiextensions.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var tlsOpts []func(*tls.Config)
	var openAPIdefinitionsPath string
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	flag.StringVar(&openAPIdefinitionsPath, "openapi-out-dir", "./definitions", "The root dir the controllers "+
		"write the Open API specs to.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
	})

	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	cfg := ctrl.GetConfigOrDie()
	clt, err := client.New(cfg, client.Options{
		Scheme: scheme,
	})
	if err != nil {
		setupLog.Error(err, "")
		os.Exit(1)
	}
	tenancyAPIExport := &kcpapis.APIExport{}
	err = clt.Get(context.TODO(), client.ObjectKey{Name: kcptenancy.SchemeGroupVersion.Group}, tenancyAPIExport)
	if err != nil {
		setupLog.Error(err, "")
		os.Exit(1)
	}
	virtualWorkspaces := tenancyAPIExport.Status.VirtualWorkspaces
	if len(virtualWorkspaces) == 0 {
		err := errors.New("")
		setupLog.Error(err, "")
		os.Exit(1)
	}
	virtualWorkspaceCfg := rest.CopyConfig(cfg)
	// w/o cluster aware manager implementation
	//virtualWorkspaceCfg.Host = fmt.Sprintf("%s/clusters/*/", virtualWorkspaces[0].URL)
	virtualWorkspaceCfg.Host = virtualWorkspaces[0].URL

	mgr, err := kcpctrl.NewClusterAwareManager(virtualWorkspaceCfg, ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "72231e1f.openmfp.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	ioHandler, err := workspacefile.NewIOHandler(openAPIdefinitionsPath)
	if err != nil {
		setupLog.Error(err, "failed to create IO Handler")
		os.Exit(1)
	}

	df, err := discoveryclient.NewFactory(virtualWorkspaceCfg)
	if err != nil {
		setupLog.Error(err, "failed to create Discovery client factory")
		os.Exit(1)
	}

	reconciler := controller.NewAPIBindingReconciler(
		//mgr.GetClient(),
		ioHandler, df, apischema.NewResolver(),
	)

	err = reconciler.SetupWithManager(mgr)
	if err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Workspace")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	signalHandler := ctrl.SetupSignalHandler()
	err = mgr.Start(signalHandler)
	if err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
