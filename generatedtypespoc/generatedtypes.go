package generatedtypespoc

import (
	"context"
	"fmt"
	"os"

	v1 "github.com/openmfp/crd-gql-gateway/generatedtypespoc/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func main() {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypes(schema.GroupVersion{
		Group: "", Version: "v1",
	}, &v1.EndpointsList{}, &v1.Endpoints{},
		// I needed to register metav1.ListOptions in the runtime.Scheme
		// so that https://github.com/kubernetes-sigs/controller-runtime/blob/aaaefb43f7e0e8e1b81371cc1b4705a967dfa0bc/pkg/client/typed_client.go#L163C3-L163C18
		// doesn't fail
		&metav1.ListOptions{},
	)

	cfg := ctrl.GetConfigOrDie()
	// this call ensures that the client accepts "application/json"
	// as contentType, without it, the content type negotiation fails
	// and we get protobuf response body from the server
	cfg = dynamic.ConfigFor(cfg)
	clt, err := client.New(cfg, client.Options{
		Scheme: scheme,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating client: %s", err.Error())
	}
	epl := &v1.EndpointsList{}
	err = clt.List(context.TODO(), epl)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error listing resources: %s", err.Error())
	}
}
