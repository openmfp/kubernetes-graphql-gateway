package main

import (
	"context"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"log"
	"time"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/kcp"
)

var enableCache bool

func main() {
	enableCache = true
	pflag.BoolVar(&enableCache, "cached", enableCache, "whether to configure a cache or not")
	pflag.Parse()

	ctx := context.Background()

	delay("Waiting %v for debugger to attach…", 5*time.Second)

	//scheme := runtime.NewScheme()
	restConfig := ctrl.GetConfigOrDie()

	var cacheOpt *ctrlruntimeclient.CacheOptions
	if enableCache {
		cacheObj, err := kcp.NewClusterAwareCache(restConfig, cache.Options{
			//Scheme: scheme,
			DefaultNamespaces: map[string]cache.Config{
				cache.AllNamespaces: {},
			},
		})
		if err != nil {
			log.Fatalf("Failed to create cache: %v", err)
		}

		cacheOpt = &ctrlruntimeclient.CacheOptions{
			Unstructured: true,
			Reader:       cacheObj,
		}

		log.Println("Waiting for caches to sync...")

		go cacheObj.Start(ctx)
		if !cacheObj.WaitForCacheSync(ctx) {
			log.Fatal("Failed to wait for caches to sync.")
		}

		delay("Caches have synced, waiting %v for things to settle.", 3*time.Second)
	} else {
		log.Println("Skipping caches.")
	}

	httpClient, err := kcp.NewClusterAwareHTTPClient(restConfig)
	if err != nil {
		log.Fatalf("Failed to create HTTP client: %v", err)
	}

	mapperCreator := kcp.NewClusterAwareMapperProvider(restConfig, httpClient)

	client, err := kcp.NewClusterAwareClientWithWatch(restConfig, ctrlruntimeclient.Options{
		//Scheme:            scheme,
		MapperWithContext: mapperCreator,
		HTTPClient:        httpClient,
		Cache:             cacheOpt,
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	log.Println("Sanity check: listing Accounts...")

	accounts := &unstructured.UnstructuredList{}
	accounts.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "core.openmfp.io",
		Version: "v1alpha1",
		Kind:    "Account",
	})

	//ctx = kontext.WithCluster(ctx, "root")
	if err := client.List(ctx, accounts); err != nil {
		log.Fatalf("Failed to list Accounts: %v", err)
	}

	log.Printf("Found %d Accounts via listing.", len(accounts.Items))

	for _, secret := range accounts.Items {
		annotations := secret.GetAnnotations()
		clusterName := annotations["kcp.io/cluster"]

		log.Printf("  Found for %s:%s/%s (UID %v).\n", clusterName, secret.GetNamespace(), secret.GetName(), secret.GetUID())
	}
}

func delay(pattern string, duration time.Duration) {
	log.Printf(pattern+"\n", duration)
	time.Sleep(duration)
}
