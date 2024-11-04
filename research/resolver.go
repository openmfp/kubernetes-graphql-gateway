package research

import (
	"encoding/json"
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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openmfp/golang-commons/logger"
)

const (
	labelSelectorArg = "labelselector"
	nameArg          = "name"
	namespaceArg     = "namespace"
)

type Resolver struct {
	log           *logger.Logger
	runtimeClient client.WithWatch
}

func NewResolver(log *logger.Logger, runtimeClient client.WithWatch) *Resolver {
	return &Resolver{
		log:           log,
		runtimeClient: runtimeClient,
	}
}

// unstructuredFieldResolver returns a GraphQL FieldResolveFn to resolve a field from an unstructured object.
func (r *Resolver) unstructuredFieldResolver(fieldName string) graphql.FieldResolveFn {
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
			r.log.Error().
				Str("type", fmt.Sprintf("%T", p.Source)).
				Msg("Source is of unexpected type")
			return nil, errors.New("source is of unexpected type")
		}

		value, found, err := unstructured.NestedFieldNoCopy(objMap, fieldName)
		if err != nil {
			r.log.Error().Err(err).Str("field", fieldName).Msg("Error retrieving field")
			return nil, err
		}
		if !found {
			r.log.Debug().Str("field", fieldName).Msg("Field not found")
			return nil, nil
		}

		return value, nil
	}
}

// getListItemsArguments returns the GraphQL arguments for listing resources.
func (r *Resolver) getListItemsArguments() graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		labelSelectorArg: &graphql.ArgumentConfig{
			Type:        graphql.String,
			Description: "A label selector to filter the objects by",
		},
		namespaceArg: &graphql.ArgumentConfig{
			Type:        graphql.String,
			Description: "The namespace in which to search for the objects",
		},
	}
}

// listItems returns a GraphQL resolver function that lists Kubernetes resources of the given GroupVersionKind.
func (r *Resolver) listItems(gvk schema.GroupVersionKind) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		ctx, span := otel.Tracer("").Start(p.Context, "listItems", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
		defer span.End()

		log, err := r.log.ChildLoggerWithAttributes(
			"operation", "list",
			"group", gvk.Group,
			"version", gvk.Version,
			"kind", gvk.Kind,
		)
		if err != nil {
			r.log.Error().Err(err).Msg("Failed to create child logger")
			// Proceed with parent logger if child logger creation fails
			log = r.log
		}

		// Create an unstructured list to hold the results
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   gvk.Group,
			Version: gvk.Version,
			Kind:    gvk.Kind + "List",
		})

		var opts []client.ListOption
		// Handle label selector argument
		if labelSelector, ok := p.Args[labelSelectorArg].(string); ok && labelSelector != "" {
			selector, err := labels.Parse(labelSelector)
			if err != nil {
				log.Error().Err(err).
					Str(labelSelectorArg, labelSelector).
					Msg("Unable to parse given label selector")
				return nil, err
			}
			opts = append(opts, client.MatchingLabelsSelector{Selector: selector})
		}

		// Handle namespace argument
		if namespace, ok := p.Args[namespaceArg].(string); ok && namespace != "" {
			opts = append(opts, client.InNamespace(namespace))
		}

		if err := r.runtimeClient.List(ctx, list, opts...); err != nil {
			log.Error().
				Err(err).
				Msg("Unable to list objects")
			return nil, err
		}

		items := list.Items

		// Sort the items by name for consistent ordering
		sort.Slice(items, func(i, j int) bool {
			return items[i].GetName() < items[j].GetName()
		})

		return items, nil
	}
}

// getItemArguments returns the GraphQL arguments for getting a single resource.
func (r *Resolver) getItemArguments() graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		nameArg: &graphql.ArgumentConfig{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "The name of the object",
		},
		namespaceArg: &graphql.ArgumentConfig{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "The namespace of the object",
		},
	}
}

// getItem returns a GraphQL resolver function that retrieves a single Kubernetes resource of the given GroupVersionKind.
func (r *Resolver) getItem(gvk schema.GroupVersionKind) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		ctx, span := otel.Tracer("").Start(p.Context, "getItem", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
		defer span.End()

		log, err := r.log.ChildLoggerWithAttributes(
			"operation", "get",
			"group", gvk.Group,
			"version", gvk.Version,
			"kind", gvk.Kind,
		)
		if err != nil {
			r.log.Error().Err(err).Msg("Failed to create child logger")
			// Proceed with parent logger if child logger creation fails
			log = r.log
		}

		// Retrieve required arguments
		name, nameOK := p.Args["name"].(string)
		namespace, nsOK := p.Args["namespace"].(string)

		if !nameOK || name == "" {
			err := errors.New("missing required argument: name")
			log.Error().Err(err).Msg("Name argument is required")
			return nil, err
		}
		if !nsOK || namespace == "" {
			err := errors.New("missing required argument: namespace")
			log.Error().Err(err).Msg("Namespace argument is required")
			return nil, err
		}

		// Create an unstructured object to hold the result
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)

		key := client.ObjectKey{
			Namespace: namespace,
			Name:      name,
		}

		// Get the object using the runtime client
		if err := r.runtimeClient.Get(ctx, key, obj); err != nil {
			log.Error().Err(err).
				Str("name", name).
				Str("namespace", namespace).
				Msg("Unable to get object")
			return nil, err
		}

		return obj, nil
	}
}

// getMutationArguments returns the GraphQL arguments for create and update mutations.
func (r *Resolver) getMutationArguments(resourceInputType *graphql.InputObject) graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		namespaceArg: &graphql.ArgumentConfig{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "The namespace of the object",
		},
		"object": &graphql.ArgumentConfig{
			Type:        graphql.NewNonNull(resourceInputType),
			Description: "The object to create or update",
		},
	}
}

// getDeleteArguments returns the GraphQL arguments for delete mutations.
func (r *Resolver) getDeleteArguments() graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		nameArg: &graphql.ArgumentConfig{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "The name of the object",
		},
		namespaceArg: &graphql.ArgumentConfig{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "The namespace of the object",
		},
	}
}

func (r *Resolver) createItem(gvk schema.GroupVersionKind) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		ctx, span := otel.Tracer("").Start(p.Context, "createItem", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
		defer span.End()

		log := r.log.With().Str("operation", "create").Str("kind", gvk.Kind).Logger()

		namespace := p.Args[namespaceArg].(string)
		objectInput := p.Args["object"].(map[string]interface{})

		obj := &unstructured.Unstructured{
			Object: objectInput,
		}
		obj.SetGroupVersionKind(gvk)
		obj.SetNamespace(namespace)

		if obj.GetName() == "" {
			return nil, errors.New("object metadata.name is required")
		}

		if err := r.runtimeClient.Create(ctx, obj); err != nil {
			log.Error().Err(err).Msg("Failed to create object")
			return nil, err
		}

		return obj, nil
	}
}

func (r *Resolver) updateItem(gvk schema.GroupVersionKind) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		ctx, span := otel.Tracer("").Start(p.Context, "updateItem", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
		defer span.End()

		log := r.log.With().Str("operation", "update").Str("kind", gvk.Kind).Logger()

		namespace := p.Args[namespaceArg].(string)
		objectInput := p.Args["object"].(map[string]interface{})

		// Ensure metadata.name is set
		name, found, err := unstructured.NestedString(objectInput, "metadata", "name")
		if err != nil || !found || name == "" {
			return nil, errors.New("object metadata.name is required")
		}

		// Marshal the input object to JSON to create the patch data
		patchData, err := json.Marshal(objectInput)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal object input: %v", err)
		}

		// Prepare a placeholder for the existing object
		existingObj := &unstructured.Unstructured{}
		existingObj.SetGroupVersionKind(gvk)

		// Fetch the existing object from the cluster
		err = r.runtimeClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, existingObj)
		if err != nil {
			log.Error().Err(err).Msg("Failed to get existing object")
			return nil, err
		}

		// Apply the merge patch to the existing object
		patch := client.RawPatch(types.MergePatchType, patchData)
		if err := r.runtimeClient.Patch(ctx, existingObj, patch); err != nil {
			log.Error().Err(err).Msg("Failed to patch object")
			return nil, err
		}

		return existingObj, nil
	}
}

// deleteItem returns a resolver function for deleting a resource.
func (r *Resolver) deleteItem(gvk schema.GroupVersionKind) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		ctx, span := otel.Tracer("").Start(p.Context, "deleteItem", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
		defer span.End()

		log := r.log.With().Str("operation", "delete").Str("kind", gvk.Kind).Logger()

		name := p.Args[nameArg].(string)
		namespace := p.Args[namespaceArg].(string)

		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)
		obj.SetNamespace(namespace)
		obj.SetName(name)

		if err := r.runtimeClient.Delete(ctx, obj); err != nil {
			log.Error().Err(err).Msg("Failed to delete object")
			return nil, err
		}

		return true, nil
	}
}
func (r *Resolver) subscribeItem(gvk schema.GroupVersionKind) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		ctx := p.Context

		namespace, _ := p.Args[namespaceArg].(string)
		name, _ := p.Args[nameArg].(string)

		resultChannel := make(chan interface{})

		go func() {
			defer close(resultChannel)

			list := &unstructured.UnstructuredList{}
			list.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   gvk.Group,
				Version: gvk.Version,
				Kind:    gvk.Kind + "List",
			})

			var opts []client.ListOption
			if namespace != "" {
				opts = append(opts, client.InNamespace(namespace))
			}
			if name != "" {
				opts = append(opts, client.MatchingFields{"metadata.name": name})
			}

			watcher, err := r.runtimeClient.Watch(ctx, list, opts...)
			if err != nil {
				r.log.Error().Err(err).Msg("Failed to start watch")
				return
			}
			defer watcher.Stop()

			for event := range watcher.ResultChan() {
				obj, ok := event.Object.(*unstructured.Unstructured)
				if !ok {
					continue
				}

				select {
				case <-ctx.Done():
					return
				case resultChannel <- obj:
				}
			}
		}()

		return resultChannel, nil
	}
}

func (r *Resolver) subscriptionResolve() graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		return p.Source, nil
	}
}
