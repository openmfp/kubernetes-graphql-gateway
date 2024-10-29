package research

import (
	"errors"
	"fmt"
	"sort"

	"github.com/graphql-go/graphql"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openmfp/golang-commons/logger"
)

type Resolver struct {
	log           *logger.Logger
	runtimeClient client.Client
}

func NewResolver(log *logger.Logger, runtimeClient client.Client) *Resolver {
	return &Resolver{
		log:           log,
		runtimeClient: runtimeClient,
	}
}

func unstructuredFieldResolver(fieldName string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		var objMap map[string]interface{}

		switch source := p.Source.(type) {
		case *unstructured.Unstructured:
			objMap = source.Object
		case unstructured.Unstructured:
			objMap = source.Object
		case map[string]interface{}:
			objMap = source
		default:
			fmt.Println("Source is of unexpected type")
			return nil, errors.New("source is of unexpected type")
		}

		value, _, err := unstructured.NestedFieldNoCopy(objMap, fieldName)

		return value, err
	}
}

func (r *Resolver) listItems(gvk schema.GroupVersionKind) func(p graphql.ResolveParams) (interface{}, error) {
	return func(p graphql.ResolveParams) (interface{}, error) {
		ctx, span := otel.Tracer("").Start(p.Context, "Resolve", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
		defer span.End()

		log, err := r.log.ChildLoggerWithAttributes(
			"operation", "list",
			"group", gvk.Group,
			"version", gvk.Version, "kind", gvk.Kind,
		)
		if err != nil {
			r.log.Error().Err(err).Msg("failed to create child logger")
		}

		// Create an unstructured list to hold the results
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   gvk.Group,
			Version: gvk.Version,
			Kind:    gvk.Kind + "List",
		})

		var opts []client.ListOption
		var selector labels.Selector
		if labelSelector, ok := p.Args["labelselector"].(string); ok && labelSelector != "" {
			selector, err = labels.Parse(labelSelector)
			if err != nil {
				log.Error().Err(err).Msg("unable to parse given label selector")
				return nil, err
			}
			opts = append(opts, client.MatchingLabelsSelector{Selector: selector})
		}

		if namespace, ok := p.Args["namespace"].(string); ok && namespace != "" {
			opts = append(opts, client.InNamespace(namespace))
		}

		err = r.runtimeClient.List(ctx, list, opts...)
		if err != nil {
			log.Error().Err(err).Msg("unable to list objects")
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

func (r *Resolver) getListArguments() graphql.FieldConfigArgument {
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
