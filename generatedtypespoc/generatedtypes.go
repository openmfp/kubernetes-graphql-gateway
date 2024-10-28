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
	}, &v1.EndpointsList{}, &v1.Endpoints{}, &metav1.ListOptions{})

	cfg := ctrl.GetConfigOrDie()
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
