package resolver

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rs/zerolog/log"
	"regexp"

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
	LabelSelectorArg  = "labelselector"
	NameArg           = "name"
	NamespaceArg      = "namespace"
	ObjectArg         = "object"
	SubscribeToAllArg = "subscribeToAll"
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
	SanitizeGroupName(string) string
	GetOriginalGroupName(string) string
}

type ArgumentsProvider interface {
	GetFieldConfigArguments(input map[string]struct{}, resourceInputType *graphql.InputObject) graphql.FieldConfigArgument
}

type Service struct {
	log           *logger.Logger
	groupNames    map[string]string
	runtimeClient client.WithWatch
}

func New(log *logger.Logger, runtimeClient client.WithWatch) *Service {
	return &Service{
		log:           log,
		groupNames:    make(map[string]string),
		runtimeClient: runtimeClient,
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
		list.SetGroupVersionKind(gvk)

		var opts []client.ListOption
		// Handle label selector argument
		if labelSelector, ok := p.Args[LabelSelectorArg].(string); ok && labelSelector != "" {
			selector, err := labels.Parse(labelSelector)
			if err != nil {
				log.Error().Err(err).Str(LabelSelectorArg, labelSelector).Msg("Unable to parse given label selector")
				return nil, err
			}
			opts = append(opts, client.MatchingLabelsSelector{Selector: selector})
		}

		// Handle namespace argument
		if namespace, ok := p.Args[NamespaceArg].(string); ok && namespace != "" {
			opts = append(opts, client.InNamespace(namespace))
		}

		if err = r.runtimeClient.List(ctx, list, opts...); err != nil {
			log.Error().Err(err).Msg("Unable to list objects")
			return nil, err
		}

		items := make([]map[string]any, len(list.Items))
		for i, item := range list.Items {
			items[i] = item.Object
		}

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
		name, namespace, err := getNameAndNameSpace(p.Args)
		if err != nil {
			return nil, err
		}

		// Create an unstructured object to hold the result
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)

		// Get the object using the runtime client
		if err = r.runtimeClient.Get(ctx, client.ObjectKey{
			Namespace: namespace,
			Name:      name,
		}, obj); err != nil {
			log.Error().Err(err).Str("name", name).Str("namespace", namespace).Msg("Unable to get object")
			return nil, err
		}

		return obj.Object, nil
	}
}

func (r *Service) CreateItem(gvk schema.GroupVersionKind) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		ctx, span := otel.Tracer("").Start(p.Context, "CreateItem", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
		defer span.End()

		gvk.Group = r.GetOriginalGroupName(gvk.Group)

		log := r.log.With().Str("operation", "create").Str("kind", gvk.Kind).Logger()

		namespace := p.Args[NamespaceArg].(string)
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

		return obj.Object, nil
	}
}

func (r *Service) UpdateItem(gvk schema.GroupVersionKind) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		ctx, span := otel.Tracer("").Start(p.Context, "UpdateItem", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
		defer span.End()

		gvk.Group = r.GetOriginalGroupName(gvk.Group)

		log := r.log.With().Str("operation", "update").Str("kind", gvk.Kind).Logger()

		name, namespace, err := getNameAndNameSpace(p.Args)
		if err != nil {
			return nil, err
		}

		objectInput := p.Args["object"].(map[string]interface{})
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

		return existingObj.Object, nil
	}
}

// DeleteItem returns a CommonResolver function for deleting a resource.
func (r *Service) DeleteItem(gvk schema.GroupVersionKind) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		ctx, span := otel.Tracer("").Start(p.Context, "DeleteItem", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
		defer span.End()

		gvk.Group = r.GetOriginalGroupName(gvk.Group)

		log := r.log.With().Str("operation", "delete").Str("kind", gvk.Kind).Logger()

		name, namespace, err := getNameAndNameSpace(p.Args)
		if err != nil {
			return nil, err
		}

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

func (r *Service) CommonResolver() graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		return p.Source, nil
	}
}

// GetFieldConfigArguments return specific arguments for the given input and resourceInputType
func (r *Service) GetFieldConfigArguments(input map[string]struct{}, resourceInputType *graphql.InputObject) graphql.FieldConfigArgument {
	arguments := graphql.FieldConfigArgument{}
	for arg := range input {
		switch arg {
		case NameArg:
			arguments[NameArg] = &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "The name of the object",
			}
		case NamespaceArg:
			arguments[NamespaceArg] = &graphql.ArgumentConfig{
				Type:        graphql.String,
				Description: "The namespace in which to search for the objects",
			}
		case LabelSelectorArg:
			arguments[LabelSelectorArg] = &graphql.ArgumentConfig{
				Type:        graphql.String,
				Description: "A label selector to filter the objects by",
			}
		case ObjectArg:
			arguments[ObjectArg] = &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(resourceInputType),
				Description: "The object to create or update",
			}
		case SubscribeToAllArg:
			arguments[SubscribeToAllArg] = &graphql.ArgumentConfig{
				Type:         graphql.Boolean,
				DefaultValue: false,
				Description:  "If true, events will be emitted on every field change",
			}
		}
	}

	return arguments
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

func getNameAndNameSpace(args map[string]interface{}) (name, namespace string, err error) {
	name, ok := args[NameArg].(string)
	if !ok || name == "" {
		log.Error().Err(errors.New("missing required argument: name")).Msg("Name argument is required")
		return "", "", errors.New("name argument is required")
	}

	namespace, ok = args[NamespaceArg].(string)
	if !ok || namespace == "" {
		log.Error().Err(errors.New("missing required argument: namespace")).Msg("Namespace argument is required")
		return "", "", errors.New("namespace argument is required")
	}

	return name, namespace, nil
}
