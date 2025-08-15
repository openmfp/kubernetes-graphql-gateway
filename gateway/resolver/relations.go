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
func (rr *RelationResolver) CreateResolver(fieldName string, targetGVK schema.GroupVersionKind) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		parentObj, ok := p.Source.(map[string]interface{})
		if !ok {
			return nil, nil
		}

		refInfo := rr.extractReferenceInfo(parentObj, fieldName)
		if refInfo.name == "" {
			return nil, nil
		}

		return rr.resolveReference(p.Context, refInfo, targetGVK)
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

// resolveReference fetches a referenced Kubernetes resource using provided target GVK
func (rr *RelationResolver) resolveReference(ctx context.Context, ref referenceInfo, targetGVK schema.GroupVersionKind) (interface{}, error) {
	gvk := targetGVK

	// Allow overrides from the reference object if specified
	if ref.apiGroup != "" {
		gvk.Group = ref.apiGroup
	}
	if ref.kind != "" {
		gvk.Kind = ref.kind
	}

	// Convert sanitized group to original before calling the client
	gvk.Group = rr.service.getOriginalGroupName(gvk.Group)

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)

	key := client.ObjectKey{Name: ref.name}
	if ref.namespace != "" {
		key.Namespace = ref.namespace
	}

	if err := rr.service.runtimeClient.Get(ctx, key, obj); err == nil {
		return obj.Object, nil
	}

	return nil, nil
}
