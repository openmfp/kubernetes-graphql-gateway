package resolver

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
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

type Provider interface {
	CrudProvider
	FieldResolverProvider
	ArgumentsProvider
}

type CrudProvider interface {
	ListItems(gvk schema.GroupVersionKind) graphql.FieldResolveFn
	GetItem(gvk schema.GroupVersionKind) graphql.FieldResolveFn
	CreateItem(gvk schema.GroupVersionKind) graphql.FieldResolveFn
	UpdateItem(gvk schema.GroupVersionKind) graphql.FieldResolveFn
	DeleteItem(gvk schema.GroupVersionKind) graphql.FieldResolveFn
	SubscribeItem(gvk schema.GroupVersionKind) graphql.FieldResolveFn
	SubscribeItems(gvk schema.GroupVersionKind) graphql.FieldResolveFn
}

type FieldResolverProvider interface {
	CommonResolver() graphql.FieldResolveFn
	UnstructuredFieldResolver(fieldName string) graphql.FieldResolveFn
	SanitizeGroupName(string) string
	GetOriginalGroupName(string) string
}

type ArgumentsProvider interface {
	GetListItemsArguments() graphql.FieldConfigArgument
	GetMutationArguments(resourceInputType *graphql.InputObject) graphql.FieldConfigArgument
	GetNameAndNamespaceArguments() graphql.FieldConfigArgument
}

type Service struct {
	log           *logger.Logger
	runtimeClient client.WithWatch
	groupNames    map[string]string
}

func New(log *logger.Logger, runtimeClient client.WithWatch) *Service {
	return &Service{
		log:           log,
		runtimeClient: runtimeClient,
		groupNames:    make(map[string]string),
	}
}

// unstructuredFieldResolver returns a GraphQL FieldResolveFn to resolve a field from an unstructured object.
func (r *Service) UnstructuredFieldResolver(fieldName string) graphql.FieldResolveFn {
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
			return nil, nil
		}

		return value, nil
	}
}

// ListItems returns a GraphQL CommonResolver function that lists Kubernetes resources of the given GroupVersionKind.
func (r *Service) ListItems(gvk schema.GroupVersionKind) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		ctx, span := otel.Tracer("").Start(p.Context, "ListItems", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
		defer span.End()

		gvk.Group = r.GetOriginalGroupName(gvk.Group)

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

// GetItem returns a GraphQL CommonResolver function that retrieves a single Kubernetes resource of the given GroupVersionKind.
func (r *Service) GetItem(gvk schema.GroupVersionKind) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		ctx, span := otel.Tracer("").Start(p.Context, "GetItem", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
		defer span.End()

		gvk.Group = r.GetOriginalGroupName(gvk.Group)

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
		if err = r.runtimeClient.Get(ctx, key, obj); err != nil {
			log.Error().Err(err).
				Str("name", name).
				Str("namespace", namespace).
				Msg("Unable to get object")
			return nil, err
		}

		return obj, nil
	}
}

func (r *Service) CreateItem(gvk schema.GroupVersionKind) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		ctx, span := otel.Tracer("").Start(p.Context, "CreateItem", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
		defer span.End()

		gvk.Group = r.GetOriginalGroupName(gvk.Group)

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

func (r *Service) UpdateItem(gvk schema.GroupVersionKind) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		ctx, span := otel.Tracer("").Start(p.Context, "UpdateItem", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
		defer span.End()

		gvk.Group = r.GetOriginalGroupName(gvk.Group)

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

// DeleteItem returns a CommonResolver function for deleting a resource.
func (r *Service) DeleteItem(gvk schema.GroupVersionKind) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		ctx, span := otel.Tracer("").Start(p.Context, "DeleteItem", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
		defer span.End()

		gvk.Group = r.GetOriginalGroupName(gvk.Group)

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

func (r *Service) SubscribeItem(gvk schema.GroupVersionKind) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {

		// Ensure the group is correctly set
		gvk.Group = r.GetOriginalGroupName(gvk.Group)

		ctx := p.Context

		// Extract required arguments
		namespace, nsOK := p.Args[namespaceArg].(string)
		name, nameOK := p.Args[nameArg].(string)

		if !nsOK || namespace == "" {
			return nil, errors.New("namespace argument is required")
		}
		if !nameOK || name == "" {
			return nil, errors.New("name argument is required")
		}

		// Create a channel to stream results
		resultChannel := make(chan interface{})

		go func() {
			defer close(resultChannel)

			// Set up the unstructured list for watching
			list := &unstructured.UnstructuredList{}
			list.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   gvk.Group,
				Version: gvk.Version,
				Kind:    gvk.Kind + "List",
			})

			// Set up watch options
			opts := []client.ListOption{
				client.InNamespace(namespace),
				client.MatchingFields{"metadata.name": name},
			}

			// Start the watch
			watcher, err := r.runtimeClient.Watch(ctx, list, opts...)
			if err != nil {
				r.log.Error().Err(err).
					Str("gvk", gvk.String()).
					Msg("Failed to start watch")
				return
			}
			defer watcher.Stop()

			// Process events
			for {
				select {
				case event, ok := <-watcher.ResultChan():
					if !ok {
						return
					}
					obj, ok := event.Object.(*unstructured.Unstructured)
					if !ok {
						continue
					}

					r.log.Debug().
						Str("eventType", string(event.Type)).
						Str("resource", obj.GetKind()).
						Str("name", obj.GetName()).
						Msg("Received event")

					// Send the object to the result channel
					select {
					case <-ctx.Done():
						return
					case resultChannel <- obj:
					}
				case <-ctx.Done():
					return
				}
			}
		}()

		// Return the result channel
		return resultChannel, nil
	}
}

func (r *Service) SubscribeItems(gvk schema.GroupVersionKind) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		gvk.Group = r.GetOriginalGroupName(gvk.Group)

		ctx := p.Context

		// Extract optional arguments
		namespace, _ := p.Args[namespaceArg].(string)
		labelSelector, _ := p.Args[labelSelectorArg].(string)

		// Create a channel to stream results
		resultChannel := make(chan interface{})

		go func() {
			defer close(resultChannel)

			// Set up the unstructured list for watching
			list := &unstructured.UnstructuredList{}
			list.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   gvk.Group,
				Version: gvk.Version,
				Kind:    gvk.Kind + "List",
			})

			// Set up watch options
			var opts []client.ListOption
			if namespace != "" {
				opts = append(opts, client.InNamespace(namespace))
			}
			if labelSelector != "" {
				selector, err := labels.Parse(labelSelector)
				if err != nil {
					r.log.Error().Err(err).
						Str("labelSelector", labelSelector).
						Msg("Invalid label selector")
					return
				}
				opts = append(opts, client.MatchingLabelsSelector{Selector: selector})
			}

			// Start the watch
			watcher, err := r.runtimeClient.Watch(ctx, list, opts...)
			if err != nil {
				r.log.Error().Err(err).
					Str("gvk", gvk.String()).
					Msg("Failed to start watch")
				return
			}
			defer watcher.Stop()

			// Process events
			for {
				select {
				case event, ok := <-watcher.ResultChan():
					if !ok {
						return
					}
					obj, ok := event.Object.(*unstructured.Unstructured)
					if !ok {
						continue
					}

					r.log.Info().
						Str("eventType", string(event.Type)).
						Str("resource", obj.GetKind()).
						Str("name", obj.GetName()).
						Msg("Received event")

					// Send the object wrapped in a slice to the result channel
					select {
					case <-ctx.Done():
						return
					case resultChannel <- []interface{}{obj}:
					}
				case <-ctx.Done():
					return
				}
			}
		}()

		// Return the result channel
		return resultChannel, nil
	}
}

func (r *Service) CommonResolver() graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		if p.Source == nil {
			// At the root level, return a non-nil value (e.g., an empty map)
			return map[string]interface{}{}, nil
		}
		return p.Source, nil
	}
}

// GetListItemsArguments returns the GraphQL arguments for listing resources.
func (r *Service) GetListItemsArguments() graphql.FieldConfigArgument {
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

// GetMutationArguments returns the GraphQL arguments for create and update mutations.
func (r *Service) GetMutationArguments(resourceInputType *graphql.InputObject) graphql.FieldConfigArgument {
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

// GetNameAndNamespaceArguments returns the GraphQL arguments for delete mutations.
func (r *Service) GetNameAndNamespaceArguments() graphql.FieldConfigArgument {
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

func (r *Service) SanitizeGroupName(groupName string) string {
	oldGroupName := groupName

	if groupName == "" {
		groupName = "core"
	} else {
		groupName = regexp.MustCompile(`[^_a-zA-Z0-9]`).ReplaceAllString(groupName, "_")
		// If the name doesn't start with a letter or underscore, prepend '_'
		if !regexp.MustCompile(`^[_a-zA-Z]`).MatchString(groupName) {
			groupName = "_" + groupName
		}
	}

	r.groupNames[groupName] = oldGroupName

	return groupName
}

func (r *Service) GetOriginalGroupName(groupName string) string {
	if originalName, ok := r.groupNames[groupName]; ok {
		return originalName
	}

	return groupName
}
