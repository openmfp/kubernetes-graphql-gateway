package resolver

import (
	"context"

	"github.com/graphql-go/graphql"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// referenceInfo holds extracted reference details
type referenceInfo struct {
	name      string
	namespace string
	kind      string
	apiGroup  string
}

// RelationResolver creates a GraphQL resolver for relation fields
func (r *Service) RelationResolver(fieldName string, gvk schema.GroupVersionKind) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		parentObj, ok := p.Source.(map[string]any)
		if !ok {
			return nil, nil
		}

		refInfo := r.extractReferenceInfo(parentObj, fieldName)
		if refInfo.name == "" {
			return nil, nil
		}

		return r.resolveReference(p.Context, refInfo, gvk)
	}
}

// extractReferenceInfo extracts reference details from a *Ref object
func (r *Service) extractReferenceInfo(parentObj map[string]any, fieldName string) referenceInfo {
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
func (r *Service) resolveReference(ctx context.Context, ref referenceInfo, targetGVK schema.GroupVersionKind) (interface{}, error) {
	gvk := targetGVK

	// Allow overrides from the reference object if specified
	if ref.apiGroup != "" {
		gvk.Group = ref.apiGroup
	}
	if ref.kind != "" {
		gvk.Kind = ref.kind
	}

	// Convert sanitized group to original before calling the client
	gvk.Group = r.getOriginalGroupName(gvk.Group)

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)

	key := client.ObjectKey{Name: ref.name}
	if ref.namespace != "" {
		key.Namespace = ref.namespace
	}

	err := r.runtimeClient.Get(ctx, key, obj)
	if err != nil {
		// For "not found" errors, return nil to allow graceful degradation
		// This handles cases where referenced resources are deleted or don't exist
		if apierrors.IsNotFound(err) {
			return nil, nil
		}

		// For other errors (network, permission, etc.), log and return the actual error
		// This ensures proper error propagation for debugging and monitoring
		r.log.Error().
			Err(err).
			Str("operation", "resolve_relation").
			Str("group", gvk.Group).
			Str("version", gvk.Version).
			Str("kind", gvk.Kind).
			Str("name", ref.name).
			Str("namespace", ref.namespace).
			Msg("Unable to resolve referenced object")
		return nil, err
	}

	// Happy path: resource found successfully
	return obj.Object, nil
}
