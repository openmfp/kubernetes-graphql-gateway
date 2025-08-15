package apischema

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/go-multierror"
	"golang.org/x/exp/maps"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtimeSchema "k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/openapi"
	"k8s.io/kube-openapi/pkg/validation/spec"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/openmfp/golang-commons/logger"
	"github.com/openmfp/kubernetes-graphql-gateway/common"
)

var (
	ErrGetOpenAPIPaths      = errors.New("failed to get OpenAPI paths")
	ErrGetCRDGVK            = errors.New("failed to get CRD GVK")
	ErrParseGroupVersion    = errors.New("failed to parse groupVersion")
	ErrMarshalOpenAPISchema = errors.New("failed to marshal openAPI v3 runtimeSchema")
	ErrConvertOpenAPISchema = errors.New("failed to convert openAPI v3 runtimeSchema to v2")
	ErrCRDNoVersions        = errors.New("CRD has no versions defined")
	ErrMarshalGVK           = errors.New("failed to marshal GVK extension")
	ErrUnmarshalGVK         = errors.New("failed to unmarshal GVK extension")
	ErrBuildKindRegistry    = errors.New("failed to build kind registry")
)

type SchemaBuilder struct {
	schemas      map[string]*spec.Schema
	err          *multierror.Error
	log          *logger.Logger
	kindRegistry map[string][]ResourceInfo
}

// ResourceInfo holds information about a resource for relationship resolution
type ResourceInfo struct {
	Group     string
	Version   string
	Kind      string
	SchemaKey string
}

func NewSchemaBuilder(oc openapi.Client, preferredApiGroups []string, log *logger.Logger) *SchemaBuilder {
	b := &SchemaBuilder{
		schemas:      make(map[string]*spec.Schema),
		kindRegistry: make(map[string][]ResourceInfo),
		log:          log,
	}

	apiv3Paths, err := oc.Paths()
	if err != nil {
		b.err = multierror.Append(b.err, errors.Join(ErrGetOpenAPIPaths, err))
		return b
	}

	for path, gv := range apiv3Paths {
		schema, err := getSchemaForPath(preferredApiGroups, path, gv)
		if err != nil {
			b.log.Debug().Err(err).Str("path", path).Msg("skipping schema path")
			continue
		}
		maps.Copy(b.schemas, schema)
	}

	return b
}

type GroupVersionKind struct {
	Group   string `json:"group"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
}

func (b *SchemaBuilder) WithScope(rm meta.RESTMapper) *SchemaBuilder {
	for _, schema := range b.schemas {
		//skip resources that do not have the GVK extension:
		//assumption: sub-resources do not have GVKs
		if schema.VendorExtensible.Extensions == nil {
			continue
		}
		var gvksVal any
		var ok bool
		if gvksVal, ok = schema.VendorExtensible.Extensions[common.GVKExtensionKey]; !ok {
			continue
		}
		jsonBytes, err := json.Marshal(gvksVal)
		if err != nil {
			b.err = multierror.Append(b.err, errors.Join(ErrMarshalGVK, err))
			continue
		}
		gvks := make([]*GroupVersionKind, 0, 1)
		if err := json.Unmarshal(jsonBytes, &gvks); err != nil {
			b.err = multierror.Append(b.err, errors.Join(ErrUnmarshalGVK, err))
			continue
		}

		if len(gvks) != 1 {
			b.log.Debug().Int("gvkCount", len(gvks)).Msg("skipping schema with unexpected GVK count")
			continue
		}

		namespaced, err := apiutil.IsGVKNamespaced(runtimeSchema.GroupVersionKind{
			Group:   gvks[0].Group,
			Version: gvks[0].Version,
			Kind:    gvks[0].Kind,
		}, rm)

		if err != nil {
			b.log.Debug().Err(err).
				Str("group", gvks[0].Group).
				Str("version", gvks[0].Version).
				Str("kind", gvks[0].Kind).
				Msg("failed to get namespaced info for GVK")
			continue
		}

		if namespaced {
			if schema.VendorExtensible.Extensions == nil {
				schema.VendorExtensible.Extensions = map[string]any{}
			}
			schema.VendorExtensible.Extensions[common.ScopeExtensionKey] = apiextensionsv1.NamespaceScoped
		} else {
			if schema.VendorExtensible.Extensions == nil {
				schema.VendorExtensible.Extensions = map[string]any{}
			}
			schema.VendorExtensible.Extensions[common.ScopeExtensionKey] = apiextensionsv1.ClusterScoped
		}
	}
	return b
}

func (b *SchemaBuilder) WithCRDCategories(crd *apiextensionsv1.CustomResourceDefinition) *SchemaBuilder {
	if crd == nil {
		return b
	}

	gkv, err := getCRDGroupVersionKind(crd.Spec)
	if err != nil {
		b.err = multierror.Append(b.err, ErrGetCRDGVK)
		return b
	}

	for _, v := range crd.Spec.Versions {
		resourceKey := getOpenAPISchemaKey(metav1.GroupVersionKind{Group: gkv.Group, Version: v.Name, Kind: gkv.Kind})
		resourceSchema, ok := b.schemas[resourceKey]
		if !ok {
			continue
		}

		if len(crd.Spec.Names.Categories) == 0 {
			b.log.Debug().Str("resource", resourceKey).Msg("no categories provided for CRD kind")
			continue
		}
		if resourceSchema.VendorExtensible.Extensions == nil {
			resourceSchema.VendorExtensible.Extensions = map[string]any{}
		}
		resourceSchema.VendorExtensible.Extensions[common.CategoriesExtensionKey] = crd.Spec.Names.Categories
		b.schemas[resourceKey] = resourceSchema
	}
	return b
}

func (b *SchemaBuilder) WithApiResourceCategories(list []*metav1.APIResourceList) *SchemaBuilder {
	if len(list) == 0 {
		return b
	}

	for _, apiResourceList := range list {
		gv, err := runtimeSchema.ParseGroupVersion(apiResourceList.GroupVersion)
		if err != nil {
			b.err = multierror.Append(b.err, errors.Join(ErrParseGroupVersion, err))
			continue
		}
		for _, apiResource := range apiResourceList.APIResources {
			if apiResource.Categories == nil {
				continue
			}
			gvk := metav1.GroupVersionKind{Group: gv.Group, Version: gv.Version, Kind: apiResource.Kind}
			resourceKey := getOpenAPISchemaKey(gvk)
			resourceSchema, ok := b.schemas[resourceKey]
			if !ok {
				continue
			}
			if resourceSchema.VendorExtensible.Extensions == nil {
				resourceSchema.VendorExtensible.Extensions = map[string]any{}
			}
			resourceSchema.VendorExtensible.Extensions[common.CategoriesExtensionKey] = apiResource.Categories
			b.schemas[resourceKey] = resourceSchema
		}
	}
	return b
}

// WithRelationships adds relationship fields to schemas that have *Ref fields
func (b *SchemaBuilder) WithRelationships() *SchemaBuilder {
	// Build kind registry first
	b.buildKindRegistry()

	// Expand relationships in all schemas
	b.log.Info().Int("kindRegistrySize", len(b.kindRegistry)).Msg("Starting relationship expansion")
	for schemaKey, schema := range b.schemas {
		b.log.Debug().Str("schemaKey", schemaKey).Msg("Processing schema for relationships")
		b.expandRelationships(schema)
	}

	return b
}

// buildKindRegistry builds a map of kind names to available resource types
func (b *SchemaBuilder) buildKindRegistry() {
	for schemaKey, schema := range b.schemas {
		// Extract GVK from schema
		if schema.VendorExtensible.Extensions == nil {
			continue
		}

		gvksVal, ok := schema.VendorExtensible.Extensions[common.GVKExtensionKey]
		if !ok {
			continue
		}

		jsonBytes, err := json.Marshal(gvksVal)
		if err != nil {
			b.log.Debug().Err(err).Str("schemaKey", schemaKey).Msg("failed to marshal GVK")
			continue
		}

		var gvks []*GroupVersionKind
		if err := json.Unmarshal(jsonBytes, &gvks); err != nil {
			b.log.Debug().Err(err).Str("schemaKey", schemaKey).Msg("failed to unmarshal GVK")
			continue
		}

		if len(gvks) != 1 {
			continue
		}

		gvk := gvks[0]

		// Add to kind registry
		resourceInfo := ResourceInfo{
			Group:     gvk.Group,
			Version:   gvk.Version,
			Kind:      gvk.Kind,
			SchemaKey: schemaKey,
		}

		// Index by lowercase kind name for consistent lookup
		key := strings.ToLower(gvk.Kind)
		b.kindRegistry[key] = append(b.kindRegistry[key], resourceInfo)
	}

	// Ensure deterministic order for picks: sort each slice by Group, Version, Kind, SchemaKey
	for kindKey, infos := range b.kindRegistry {
		slices.SortFunc(infos, func(a, b ResourceInfo) int {
			if a.Group != b.Group {
				if a.Group < b.Group {
					return -1
				}
				return 1
			}
			if a.Version != b.Version {
				if a.Version < b.Version {
					return -1
				}
				return 1
			}
			if a.Kind != b.Kind {
				if a.Kind < b.Kind {
					return -1
				}
				return 1
			}
			if a.SchemaKey < b.SchemaKey {
				return -1
			}
			if a.SchemaKey > b.SchemaKey {
				return 1
			}
			return 0
		})
		b.kindRegistry[kindKey] = infos
	}

	b.log.Debug().Int("kindCount", len(b.kindRegistry)).Msg("built kind registry for relationships")
}

// expandRelationships detects fields ending with 'Ref' and adds corresponding relationship fields
func (b *SchemaBuilder) expandRelationships(schema *spec.Schema) {
	if schema.Properties == nil {
		return
	}

	for propName := range schema.Properties {
		if !isRefProperty(propName) {
			continue
		}

		baseKind := strings.TrimSuffix(propName, "Ref")
		lookupKey := strings.ToLower(baseKind)

		resourceTypes, exists := b.kindRegistry[lookupKey]
		if !exists || len(resourceTypes) == 0 {
			continue
		}

		fieldName := strings.ToLower(baseKind)
		if _, exists := schema.Properties[fieldName]; exists {
			continue
		}

		target := resourceTypes[0]
		ref := spec.MustCreateRef(fmt.Sprintf("#/definitions/%s.%s.%s", target.Group, target.Version, target.Kind))
		schema.Properties[fieldName] = spec.Schema{SchemaProps: spec.SchemaProps{Ref: ref}}

		b.log.Info().
			Str("sourceField", propName).
			Str("targetField", fieldName).
			Str("targetKind", target.Kind).
			Str("targetGroup", target.Group).
			Msg("Added relationship field")
	}

	// Recursively process nested objects and write back modifications
	for key, prop := range schema.Properties {
		if prop.Type.Contains("object") && prop.Properties != nil {
			b.expandRelationships(&prop)
			schema.Properties[key] = prop
		}
	}
}

func isRefProperty(name string) bool {
	if !strings.HasSuffix(name, "Ref") {
		return false
	}
	if name == "Ref" {
		return false
	}
	return true
}

func (b *SchemaBuilder) Complete() ([]byte, error) {
	v3JSON, err := json.Marshal(&schemaResponse{
		Components: schemasComponentsWrapper{
			Schemas: b.schemas,
		},
	})
	if err != nil {
		return nil, errors.Join(ErrMarshalOpenAPISchema, err)
	}

	v2JSON, err := ConvertJSON(v3JSON)
	if err != nil {
		return nil, errors.Join(ErrConvertOpenAPISchema, err)
	}

	return v2JSON, nil
}

// getOpenAPISchemaKey creates the key that kubernetes uses in its OpenAPI Definitions
func getOpenAPISchemaKey(gvk metav1.GroupVersionKind) string {
	// we need to inverse group to match the runtimeSchema key(io.openmfp.core.v1alpha1.Account)
	parts := strings.Split(gvk.Group, ".")
	slices.Reverse(parts)
	reversedGroup := strings.Join(parts, ".")

	return fmt.Sprintf("%s.%s.%s", reversedGroup, gvk.Version, gvk.Kind)
}

func getCRDGroupVersionKind(spec apiextensionsv1.CustomResourceDefinitionSpec) (*metav1.GroupVersionKind, error) {
	if len(spec.Versions) == 0 {
		return nil, ErrCRDNoVersions
	}

	// Use the first stored version as the preferred one
	return &metav1.GroupVersionKind{
		Group:   spec.Group,
		Version: spec.Versions[0].Name,
		Kind:    spec.Names.Kind,
	}, nil
}
