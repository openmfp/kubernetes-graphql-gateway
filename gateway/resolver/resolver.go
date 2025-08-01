package resolver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/graphql-go/graphql"
	pkgErrors "github.com/pkg/errors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"gopkg.in/yaml.v3"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openmfp/golang-commons/logger"
)

// convertMapsToArrays transforms label-like fields from maps to arrays (for GraphQL output)
// map[string]string → []Label
func convertMapsToArrays(obj any) any {
	objMap, ok := obj.(map[string]interface{})
	if !ok {
		return obj
	}

	// Process metadata fields
	if metadata := objMap["metadata"]; metadata != nil {
		objMap["metadata"] = processMetadataToArrays(metadata)
	}

	// Process spec fields
	if spec := objMap["spec"]; spec != nil {
		objMap["spec"] = processSpecToArrays(spec)
	}

	return obj
}

// convertArraysToMaps transforms label-like fields from arrays to maps (for Kubernetes input)
// []Label → map[string]string
func convertArraysToMaps(obj any) any {
	objMap, ok := obj.(map[string]interface{})
	if !ok {
		return obj
	}

	// Process metadata fields
	if metadata := objMap["metadata"]; metadata != nil {
		objMap["metadata"] = processMetadataToMaps(metadata)
	}

	// Process spec fields
	if spec := objMap["spec"]; spec != nil {
		objMap["spec"] = processSpecToMaps(spec)
	}

	return obj
}

// processMetadataToArrays handles metadata.labels and metadata.annotations conversion to arrays
func processMetadataToArrays(metadata any) any {
	metadataMap, ok := metadata.(map[string]interface{})
	if !ok {
		return metadata
	}

	if labels := metadataMap["labels"]; labels != nil {
		metadataMap["labels"] = mapToArray(labels)
	}
	if annotations := metadataMap["annotations"]; annotations != nil {
		metadataMap["annotations"] = mapToArray(annotations)
	}

	return metadataMap
}

// processMetadataToMaps handles metadata.labels and metadata.annotations conversion to maps
func processMetadataToMaps(metadata any) any {
	metadataMap, ok := metadata.(map[string]interface{})
	if !ok {
		return metadata
	}

	if labels := metadataMap["labels"]; labels != nil {
		metadataMap["labels"] = arrayToMap(labels)
	}
	if annotations := metadataMap["annotations"]; annotations != nil {
		metadataMap["annotations"] = arrayToMap(annotations)
	}

	return metadataMap
}

// processSpecToArrays handles spec.nodeSelector and spec.selector.matchLabels conversion to arrays
func processSpecToArrays(spec any) any {
	specMap, ok := spec.(map[string]interface{})
	if !ok {
		return spec
	}

	if nodeSelector := specMap["nodeSelector"]; nodeSelector != nil {
		specMap["nodeSelector"] = mapToArray(nodeSelector)
	}
	if selector := specMap["selector"]; selector != nil {
		specMap["selector"] = processSelector(selector, true)
	}

	return specMap
}

// processSpecToMaps handles spec.nodeSelector and spec.selector.matchLabels conversion to maps
func processSpecToMaps(spec any) any {
	specMap, ok := spec.(map[string]interface{})
	if !ok {
		return spec
	}

	if nodeSelector := specMap["nodeSelector"]; nodeSelector != nil {
		specMap["nodeSelector"] = arrayToMap(nodeSelector)
	}
	if selector := specMap["selector"]; selector != nil {
		specMap["selector"] = processSelector(selector, false)
	}

	return specMap
}

// processSelector handles selector.matchLabels conversion
func processSelector(selector any, toArray bool) any {
	selectorMap, ok := selector.(map[string]interface{})
	if !ok {
		return selector
	}

	if matchLabels := selectorMap["matchLabels"]; matchLabels != nil {
		if toArray {
			selectorMap["matchLabels"] = mapToArray(matchLabels)
		} else {
			selectorMap["matchLabels"] = arrayToMap(matchLabels)
		}
	}

	return selectorMap
}

// mapToArray converts a label map to array format
func mapToArray(value any) any {
	labelMap, ok := value.(map[string]interface{})
	if !ok {
		return value
	}

	var labels []map[string]interface{}
	for k, v := range labelMap {
		if strValue, ok := v.(string); ok {
			labels = append(labels, map[string]interface{}{
				"key":   k,
				"value": strValue,
			})
		}
	}
	return labels
}

// arrayToMap converts a label array to map format
func arrayToMap(value any) any {
	labelArray, ok := value.([]interface{})
	if !ok {
		return value
	}

	labelMap := make(map[string]string)
	for _, item := range labelArray {
		if labelObj, ok := item.(map[string]interface{}); ok {
			if key, keyOk := labelObj["key"].(string); keyOk {
				if val, valOk := labelObj["value"].(string); valOk {
					labelMap[key] = val
				}
			}
		}
	}
	return labelMap
}

type Provider interface {
	CrudProvider
	CustomQueriesProvider
	CommonResolver() graphql.FieldResolveFn
	SanitizeGroupName(string) string
}

type CrudProvider interface {
	ListItems(gvk schema.GroupVersionKind, scope v1.ResourceScope) graphql.FieldResolveFn
	GetItem(gvk schema.GroupVersionKind, scope v1.ResourceScope) graphql.FieldResolveFn
	GetItemAsYAML(gvk schema.GroupVersionKind, scope v1.ResourceScope) graphql.FieldResolveFn
	CreateItem(gvk schema.GroupVersionKind, scope v1.ResourceScope) graphql.FieldResolveFn
	UpdateItem(gvk schema.GroupVersionKind, scope v1.ResourceScope) graphql.FieldResolveFn
	DeleteItem(gvk schema.GroupVersionKind, scope v1.ResourceScope) graphql.FieldResolveFn
	SubscribeItem(gvk schema.GroupVersionKind, scope v1.ResourceScope) graphql.FieldResolveFn
	SubscribeItems(gvk schema.GroupVersionKind, scope v1.ResourceScope) graphql.FieldResolveFn
}

type CustomQueriesProvider interface {
	TypeByCategory(m map[string][]TypeByCategory) graphql.FieldResolveFn
}

type Service struct {
	log *logger.Logger
	// groupNames stores relation between sanitized group names and original group names that are used in the Kubernetes API
	groupNames    map[string]string // map[sanitizedGroupName]originalGroupName
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
func (r *Service) ListItems(gvk schema.GroupVersionKind, scope v1.ResourceScope) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		ctx, span := otel.Tracer("").Start(p.Context, "ListItems", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
		defer span.End()

		gvk.Group = r.getOriginalGroupName(gvk.Group)

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

		if isResourceNamespaceScoped(scope) {
			namespace, err := getStringArg(p.Args, NamespaceArg, false)
			if err != nil {
				return nil, err
			}
			if namespace != "" {
				opts = append(opts, client.InNamespace(namespace))
			}
		}

		if err = r.runtimeClient.List(ctx, list, opts...); err != nil {
			log.Error().Err(err).Msg("Unable to list objects")
			return nil, pkgErrors.Wrap(err, "unable to list objects")
		}

		sortBy, err := getStringArg(p.Args, SortByArg, false)
		if err != nil {
			return nil, err
		}

		err = validateSortBy(list.Items, sortBy)
		if err != nil {
			log.Error().Err(err).Str(SortByArg, sortBy).Msg("Invalid sortBy field path")
			return nil, err
		}

		sort.Slice(list.Items, func(i, j int) bool {
			return compareUnstructured(list.Items[i], list.Items[j], sortBy) < 0
		})

		items := make([]map[string]any, len(list.Items))
		for i, item := range list.Items {
			// Convert maps back to label arrays for GraphQL response
			convertedItem := convertMapsToArrays(item.Object).(map[string]interface{})
			items[i] = convertedItem
		}

		return items, nil
	}
}

// GetItem returns a GraphQL CommonResolver function that retrieves a single Kubernetes resource of the given GroupVersionKind.
func (r *Service) GetItem(gvk schema.GroupVersionKind, scope v1.ResourceScope) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		ctx, span := otel.Tracer("").Start(p.Context, "GetItem", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
		defer span.End()

		gvk.Group = r.getOriginalGroupName(gvk.Group)

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
		name, err := getStringArg(p.Args, NameArg, true)
		if err != nil {
			return nil, err
		}

		// Create an unstructured object to hold the result
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)

		key := client.ObjectKey{
			Name: name,
		}

		if isResourceNamespaceScoped(scope) {
			namespace, err := getStringArg(p.Args, NamespaceArg, true)
			if err != nil {
				return nil, err
			}

			key.Namespace = namespace
		}

		// Get the object using the runtime client
		if err = r.runtimeClient.Get(ctx, key, obj); err != nil {
			log.Error().Err(err).Str("name", name).Str("scope", string(scope)).Msg("Unable to get object")
			return nil, err
		}

		// Convert maps back to label arrays for GraphQL response
		convertedResponse := convertMapsToArrays(obj.Object)
		return convertedResponse, nil
	}
}

func (r *Service) GetItemAsYAML(gvk schema.GroupVersionKind, scope v1.ResourceScope) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		var span trace.Span
		p.Context, span = otel.Tracer("").Start(p.Context, "GetItemAsYAML", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
		defer span.End()

		out, err := r.GetItem(gvk, scope)(p)
		if err != nil {
			return "", err
		}

		var returnYaml bytes.Buffer
		if err = yaml.NewEncoder(&returnYaml).Encode(out); err != nil {
			return "", err
		}

		return returnYaml.String(), nil
	}
}

func (r *Service) CreateItem(gvk schema.GroupVersionKind, scope v1.ResourceScope) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		ctx, span := otel.Tracer("").Start(p.Context, "CreateItem", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
		defer span.End()

		gvk.Group = r.getOriginalGroupName(gvk.Group)

		log := r.log.With().Str("operation", "create").Str("kind", gvk.Kind).Logger()

		objectInput := p.Args["object"].(map[string]interface{})

		// Convert label arrays back to maps for Kubernetes compatibility
		convertedInput := convertArraysToMaps(objectInput).(map[string]interface{})

		obj := &unstructured.Unstructured{
			Object: convertedInput,
		}
		obj.SetGroupVersionKind(gvk)

		if isResourceNamespaceScoped(scope) {
			namespace, err := getStringArg(p.Args, NamespaceArg, true)
			if err != nil {
				return nil, err
			}
			obj.SetNamespace(namespace)
		}

		if obj.GetName() == "" {
			return nil, errors.New("object metadata.name is required")
		}

		dryRunBool, err := getBoolArg(p.Args, DryRunArg, false)
		if err != nil {
			return nil, err
		}
		dryRun := []string{}
		if dryRunBool {
			dryRun = []string{"All"}
		}

		if err := r.runtimeClient.Create(ctx, obj, &client.CreateOptions{DryRun: dryRun}); err != nil {
			log.Error().Err(err).Msg("Failed to create object")
			return nil, err
		}

		// Convert maps back to label arrays for GraphQL response
		convertedResponse := convertMapsToArrays(obj.Object)
		return convertedResponse, nil
	}
}

func (r *Service) UpdateItem(gvk schema.GroupVersionKind, scope v1.ResourceScope) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		ctx, span := otel.Tracer("").Start(p.Context, "UpdateItem", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
		defer span.End()

		gvk.Group = r.getOriginalGroupName(gvk.Group)

		log := r.log.With().Str("operation", "update").Str("kind", gvk.Kind).Logger()

		name, err := getStringArg(p.Args, NameArg, true)
		if err != nil {
			return nil, err
		}

		objectInput := p.Args["object"].(map[string]interface{})
		// Convert label arrays back to maps for Kubernetes compatibility
		convertedInput := convertArraysToMaps(objectInput)
		// Marshal the converted input object to JSON to create the patch data
		patchData, err := json.Marshal(convertedInput)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal object input: %v", err)
		}

		// Prepare a placeholder for the existing object
		existingObj := &unstructured.Unstructured{}
		existingObj.SetGroupVersionKind(gvk)

		key := client.ObjectKey{Name: name}
		if isResourceNamespaceScoped(scope) {
			namespace, err := getStringArg(p.Args, NamespaceArg, true)
			if err != nil {
				return nil, err
			}
			key.Namespace = namespace
		}

		// Fetch the existing object from the cluster
		err = r.runtimeClient.Get(ctx, key, existingObj)
		if err != nil {
			log.Error().Err(err).Msg("Failed to get existing object")
			return nil, err
		}

		dryRunBool, err := getBoolArg(p.Args, DryRunArg, false)
		if err != nil {
			return nil, err
		}
		dryRun := []string{}
		if dryRunBool {
			dryRun = []string{"All"}
		}

		// Apply the merge patch to the existing object
		patch := client.RawPatch(types.MergePatchType, patchData)
		if err := r.runtimeClient.Patch(ctx, existingObj, patch, &client.PatchOptions{DryRun: dryRun}); err != nil {
			log.Error().Err(err).Msg("Failed to patch object")
			return nil, err
		}

		// Convert maps back to label arrays for GraphQL response
		convertedResponse := convertMapsToArrays(existingObj.Object)
		return convertedResponse, nil
	}
}

// DeleteItem returns a CommonResolver function for deleting a resource.
func (r *Service) DeleteItem(gvk schema.GroupVersionKind, scope v1.ResourceScope) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		ctx, span := otel.Tracer("").Start(p.Context, "DeleteItem", trace.WithAttributes(attribute.String("kind", gvk.Kind)))
		defer span.End()

		gvk.Group = r.getOriginalGroupName(gvk.Group)

		log := r.log.With().Str("operation", "delete").Str("kind", gvk.Kind).Logger()

		name, err := getStringArg(p.Args, NameArg, true)
		if err != nil {
			return nil, err
		}

		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)
		obj.SetName(name)

		if isResourceNamespaceScoped(scope) {
			namespace, err := getStringArg(p.Args, NamespaceArg, true)
			if err != nil {
				return nil, err
			}
			obj.SetNamespace(namespace)
		}

		dryRunBool, err := getBoolArg(p.Args, DryRunArg, false)
		if err != nil {
			return nil, err
		}
		dryRun := []string{}
		if dryRunBool {
			dryRun = []string{"All"}
		}

		if err := r.runtimeClient.Delete(ctx, obj, &client.DeleteOptions{DryRun: dryRun}); err != nil {
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

func (r *Service) SanitizeGroupName(groupName string) string {
	originalGroupName := groupName

	if groupName == "" {
		groupName = "core"
	} else {
		groupName = regexp.MustCompile(`[^_a-zA-Z0-9]`).ReplaceAllString(groupName, "_")
		// If the name doesn't start with a letter or underscore, prepend '_'
		if !regexp.MustCompile(`^[_a-zA-Z]`).MatchString(groupName) {
			groupName = "_" + groupName
		}
	}

	r.storeOriginalGroupName(groupName, originalGroupName)

	return groupName
}

func (r *Service) storeOriginalGroupName(groupName, originalName string) {
	r.groupNames[groupName] = originalName
}

func (r *Service) getOriginalGroupName(groupName string) string {
	if originalName, ok := r.groupNames[groupName]; ok {
		return originalName
	}

	return groupName
}

func compareUnstructured(a, b unstructured.Unstructured, fieldPath string) int {
	segments := strings.Split(fieldPath, ".")

	aVal, foundA, errA := unstructured.NestedFieldNoCopy(a.Object, segments...)
	bVal, foundB, errB := unstructured.NestedFieldNoCopy(b.Object, segments...)
	if errA != nil || errB != nil || !foundA || !foundB {
		return 0 // fallback if fields are missing or inaccessible
	}

	switch av := aVal.(type) {
	case string:
		if bv, ok := bVal.(string); ok {
			return strings.Compare(av, bv)
		}
	case int64:
		if bv, ok := bVal.(int64); ok {
			return compareNumbers(av, bv)
		}
	case int32:
		if bv, ok := bVal.(int32); ok {
			return compareNumbers(int64(av), int64(bv))
		} else if bv, ok := bVal.(int64); ok {
			return compareNumbers(int64(av), bv)
		}
	case float64:
		if bv, ok := bVal.(float64); ok {
			return compareNumbers(av, bv)
		}
	case float32:
		if bv, ok := bVal.(float32); ok {
			return compareNumbers(float64(av), float64(bv))
		} else if bv, ok := bVal.(float64); ok {
			return compareNumbers(float64(av), bv)
		}
	case bool:
		if bv, ok := bVal.(bool); ok {
			switch {
			case av && !bv:
				return -1
			case !av && bv:
				return 1
			default:
				return 0
			}
		}
	}
	return 0 // unhandled or non-comparable types
}

func compareNumbers[T int64 | float64](a, b T) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
