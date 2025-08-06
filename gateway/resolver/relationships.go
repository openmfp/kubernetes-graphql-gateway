package resolver

import (
	"fmt"
	"strings"

	"github.com/graphql-go/graphql"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/openmfp/golang-commons/logger"
)

// RelationshipResolver handles resolution of relationships between Kubernetes resources
type RelationshipResolver struct {
	log *logger.Logger
}

// EnhancedRef represents our custom relationship reference structure
type EnhancedRef struct {
	Kind         string `json:"kind,omitempty"`
	GroupVersion string `json:"groupVersion,omitempty"`
	Name         string `json:"name"`
	Namespace    string `json:"namespace,omitempty"`
}

// NewRelationshipResolver creates a new relationship resolver
func NewRelationshipResolver(log *logger.Logger) *RelationshipResolver {
	return &RelationshipResolver{
		log: log,
	}
}

// IsRelationshipField checks if a field name follows the relationship convention (<kind>Ref)
func IsRelationshipField(fieldName string) bool {
	return strings.HasSuffix(fieldName, "Ref")
}

// ExtractTargetKind extracts the target kind from a relationship field name
// e.g., "roleRef" -> "Role"
func ExtractTargetKind(fieldName string) string {
	if !IsRelationshipField(fieldName) {
		return ""
	}

	// Remove "Ref" suffix and capitalize first letter
	kindName := strings.TrimSuffix(fieldName, "Ref")
	if len(kindName) == 0 {
		return ""
	}

	return strings.ToUpper(kindName[:1]) + kindName[1:]
}

// CreateSingleRelationResolver creates GraphQL resolvers for <Kind>Relation fields (e.g. roleRelation)
func (r *RelationshipResolver) CreateSingleRelationResolver(sourceGVK schema.GroupVersionKind, fieldName string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		_, span := otel.Tracer("").Start(p.Context, "ResolveSingleRelation",
			trace.WithAttributes(
				attribute.String("sourceKind", sourceGVK.Kind),
				attribute.String("fieldName", fieldName),
			))
		defer span.End()

		// Get the source object from the parent resolver
		sourceObj, ok := p.Source.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("expected source to be map[string]interface{}, got %T", p.Source)
		}

		// Extract the relationship reference from the source object
		refValue, found, err := unstructured.NestedFieldNoCopy(sourceObj, fieldName)
		if err != nil {
			r.log.Debug().Err(err).Str("field", fieldName).Msg("Error accessing relationship field")
			return nil, nil // Return nil for optional field
		}
		if !found {
			return nil, nil // Field not present, return nil
		}

		refMap, ok := refValue.(map[string]interface{})
		if !ok {
			r.log.Debug().Str("field", fieldName).Msg("Relationship field is not a map")
			return nil, nil
		}

		// Create enhanced reference
		enhancedRef, err := r.createEnhancedRef(refMap, sourceObj, sourceGVK, fieldName)
		if err != nil {
			r.log.Debug().Err(err).Str("field", fieldName).Msg("Error creating enhanced reference")
			return nil, nil
		}

		return enhancedRef, nil
	}
}

// createEnhancedRef transforms native Kubernetes references (e.g. roleRef) into our enhanced structure with explicit groupVersion and inferred namespace
func (r *RelationshipResolver) createEnhancedRef(nativeRefMap map[string]interface{}, sourceObj map[string]interface{}, sourceGVK schema.GroupVersionKind, fieldName string) (map[string]interface{}, error) {
	enhancedRef := make(map[string]interface{})

	// Extract name (required)
	name, ok := nativeRefMap["name"].(string)
	if !ok {
		return nil, fmt.Errorf("name is required in relationship reference")
	}
	enhancedRef["name"] = name

	// Extract or infer kind
	if kind, ok := nativeRefMap["kind"].(string); ok {
		enhancedRef["kind"] = kind
	} else {
		// Infer kind from field name
		targetKind := ExtractTargetKind(fieldName)
		if targetKind != "" {
			enhancedRef["kind"] = targetKind
		}
	}

	// Extract or construct groupVersion
	if groupVersion, ok := nativeRefMap["groupVersion"].(string); ok {
		enhancedRef["groupVersion"] = groupVersion
	} else if apiGroup, ok := nativeRefMap["apiGroup"].(string); ok {
		// Construct groupVersion from apiGroup
		// For most Kubernetes APIs, v1 is the stable version
		if apiGroup == "" {
			enhancedRef["groupVersion"] = "v1" // Core API group
		} else {
			enhancedRef["groupVersion"] = apiGroup + "/v1" // Default to v1 for all API groups
		}
	}

	// Extract or infer namespace
	if namespace, ok := nativeRefMap["namespace"].(string); ok {
		enhancedRef["namespace"] = namespace
		return enhancedRef, nil
	}

	// Infer namespace from source object and relationship context
	targetKind, _ := enhancedRef["kind"].(string)
	inferredNamespace := r.inferNamespaceForReference(sourceObj, sourceGVK, fieldName, targetKind)
	if inferredNamespace != "" {
		enhancedRef["namespace"] = inferredNamespace
	}

	return enhancedRef, nil
}

// inferNamespaceForReference infers the namespace for a reference based on Kubernetes conventions
func (r *RelationshipResolver) inferNamespaceForReference(sourceObj map[string]interface{}, sourceGVK schema.GroupVersionKind, fieldName string, targetKind string) string {
	// Default: try to use source object's namespace for namespaced targets
	// This works for most cases where references point to resources in the same namespace
	metadata, found := sourceObj["metadata"]
	if !found {
		return ""
	}

	metadataMap, ok := metadata.(map[string]interface{})
	if !ok {
		return ""
	}

	namespace, ok := metadataMap["namespace"].(string)
	if !ok {
		return "" // Source has no namespace (cluster-scoped), so target likely cluster-scoped too
	}

	return namespace
}

// getOriginalGroupName converts sanitized group name back to original
