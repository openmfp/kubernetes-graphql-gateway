package research

import (
	"fmt"
	"github.com/graphql-go/graphql"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"log/slog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sort"
)

// resovler
type ResolverProvider struct {
	runtimeClient client.Client
}

func NewResolver(runtimeClient client.Client) *ResolverProvider {
	return &ResolverProvider{
		runtimeClient: runtimeClient,
	}
}

func (r *ResolverProvider) listItems(gvk schema.GroupVersionKind) func(p graphql.ResolveParams) (interface{}, error) {
	return func(p graphql.ResolveParams) (interface{}, error) {
		ctx, span := otel.Tracer("").Start(p.Context, "Resolve", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
		defer span.End()

		logger := slog.With(
			slog.String("operation", "list"),
			slog.String("group", gvk.Group),
			slog.String("version", gvk.Version),
			slog.String("kind", gvk.Kind),
		)

		// Create an unstructured list to hold the results
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   gvk.Group,
			Version: gvk.Version,
			Kind:    gvk.Kind + "List",
		})

		var opts []client.ListOption
		if labelSelector, ok := p.Args["labelselector"].(string); ok && labelSelector != "" {
			selector, err := labels.Parse(labelSelector)
			if err != nil {
				logger.Error("unable to parse given label selector", slog.Any("error", err))
				return nil, err
			}
			opts = append(opts, client.MatchingLabelsSelector{Selector: selector})
		}

		if namespace, ok := p.Args["namespace"].(string); ok && namespace != "" {
			opts = append(opts, client.InNamespace(namespace))
		}

		err := r.runtimeClient.List(ctx, list, opts...)
		if err != nil {
			logger.Error("unable to list objects", slog.Any("error", err))
			return nil, err
		}

		items := list.Items

		// Sort the items by name
		sort.Slice(items, func(i, j int) bool {
			return items[i].GetName() < items[j].GetName()
		})

		return items, nil
	}
}

// Helper function to convert resource name to GroupVersionResource
func getGroupVersionResource(resourceName string) (schema.GroupVersionResource, error) {
	switch resourceName {
	case "pods", "Pod":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}, nil
	case "services", "Service":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}, nil
	// Add more cases for other resource types
	default:
		return schema.GroupVersionResource{}, fmt.Errorf("unknown resource: %s", resourceName)
	}
}

func (r *ResolverProvider) getListArguments() graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		"labelselector": &graphql.ArgumentConfig{
			Type:        graphql.String,
			Description: "a label selector to filter the objects by",
		},
		"namespace": &graphql.ArgumentConfig{
			Type:        graphql.String,
			Description: "the namespace in which to search for the objects",
		},
	}
}

func (r *ResolverProvider) getItemArguments() graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		"name": &graphql.ArgumentConfig{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "the metadata.name of the object",
		},
		"namespace": &graphql.ArgumentConfig{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "the metadata.namespace of the object",
		},
	}
}

func (r *ResolverProvider) getChangeArguments(input graphql.Input) graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		"metadata": &graphql.ArgumentConfig{
			Type:        graphql.NewNonNull(metadataInput),
			Description: "the metadata of the object",
		},
		"spec": &graphql.ArgumentConfig{
			Type:        graphql.NewNonNull(input),
			Description: "the spec of the object",
		},
	}
}

func (r *ResolverProvider) getPatchArguments() graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		"type": &graphql.ArgumentConfig{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "The JSON patch type, it can be json-patch, merge-patch, strategic-merge-patch",
		},
		"payload": &graphql.ArgumentConfig{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "The JSON patch to apply to the object",
		},
		"metadata": &graphql.ArgumentConfig{
			Type:        graphql.NewNonNull(metadataInput),
			Description: "Metadata including name and namespace of the object you want to patch",
		},
	}
}
