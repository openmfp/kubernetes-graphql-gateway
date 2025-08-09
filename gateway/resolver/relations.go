package resolver

import (
	"context"

	"github.com/graphql-go/graphql"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RelationResolver handles runtime resolution of relation fields
type RelationResolver struct {
	service *Service
}

// NewRelationResolver creates a new relation resolver
func NewRelationResolver(service *Service) *RelationResolver {
	return &RelationResolver{
		service: service,
	}
}

// CreateResolver creates a GraphQL resolver for relation fields
func (rr *RelationResolver) CreateResolver(fieldName string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		parentObj, ok := p.Source.(map[string]interface{})
		if !ok {
			return nil, nil
		}

		refInfo := rr.extractReferenceInfo(parentObj, fieldName)
		if refInfo.name == "" {
			return nil, nil
		}

		return rr.resolveReference(p.Context, refInfo.name, refInfo.namespace, refInfo.kind, refInfo.apiGroup)
	}
}

// referenceInfo holds extracted reference details
type referenceInfo struct {
	name      string
	namespace string
	kind      string
	apiGroup  string
}

// extractReferenceInfo extracts reference details from a *Ref object
func (rr *RelationResolver) extractReferenceInfo(parentObj map[string]interface{}, fieldName string) referenceInfo {
	name, _ := parentObj["name"].(string)
	if name == "" {
		return referenceInfo{}
	}

	namespace, _ := parentObj["namespace"].(string)
	apiGroup, _ := parentObj["apiGroup"].(string)

	kind, _ := parentObj["kind"].(string)
	if kind == "" {
		// Fallback: infer kind from field name (e.g., "role" -> "Role")
		kind = cases.Title(language.English).String(fieldName)
	}

	return referenceInfo{
		name:      name,
		namespace: namespace,
		kind:      kind,
		apiGroup:  apiGroup,
	}
}

// resolveReference fetches a referenced Kubernetes resource
func (rr *RelationResolver) resolveReference(ctx context.Context, name, namespace, kind, apiGroup string) (interface{}, error) {
	versions := []string{"v1", "v1beta1", "v1alpha1"}

	for _, version := range versions {
		if obj := rr.tryFetchResource(ctx, name, namespace, kind, apiGroup, version); obj != nil {
			return obj, nil
		}
	}

	return nil, nil
}

// tryFetchResource attempts to fetch a Kubernetes resource with the given parameters
func (rr *RelationResolver) tryFetchResource(ctx context.Context, name, namespace, kind, apiGroup, version string) map[string]interface{} {
	gvk := schema.GroupVersionKind{
		Group:   apiGroup,
		Version: version,
		Kind:    kind,
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)

	key := client.ObjectKey{Name: name}
	if namespace != "" {
		key.Namespace = namespace
	}

	if err := rr.service.runtimeClient.Get(ctx, key, obj); err == nil {
		return obj.Object
	}

	return nil
}

// GetSupportedVersions returns the list of API versions to try for resource resolution
func (rr *RelationResolver) GetSupportedVersions() []string {
	return []string{"v1", "v1beta1", "v1alpha1"}
}

// SetSupportedVersions allows customizing the API versions to try (for future extensibility)
func (rr *RelationResolver) SetSupportedVersions(versions []string) {
	// Future: Store in resolver state for customization
	// For now, this is a placeholder for extensibility
}
