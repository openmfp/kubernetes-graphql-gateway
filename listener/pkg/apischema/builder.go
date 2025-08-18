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
	schemas           map[string]*spec.Schema
	err               *multierror.Error
	log               *logger.Logger
	kindRegistry      map[GroupVersionKind]ResourceInfo // Changed: Use GVK as key for precise lookup
	preferredVersions map[string]string                 // map[group/kind]preferredVersion
	maxRelationDepth  int                               // maximum allowed relationship nesting depth (1 = single level)
	relationDepths    map[string]int                    // tracks the minimum depth at which each schema is referenced
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
		schemas:           make(map[string]*spec.Schema),
		kindRegistry:      make(map[GroupVersionKind]ResourceInfo),
		preferredVersions: make(map[string]string),
		maxRelationDepth:  1, // Default to 1-level depth for now
		relationDepths:    make(map[string]int),
		log:               log,
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

// WithMaxRelationDepth sets the maximum allowed relationship nesting depth
// depth=1: A->B (single level)
// depth=2: A->B->C (two levels)
// depth=3: A->B->C->D (three levels)
func (b *SchemaBuilder) WithMaxRelationDepth(depth int) *SchemaBuilder {
	if depth < 1 {
		depth = 1 // Minimum depth is 1
	}
	b.maxRelationDepth = depth
	b.log.Info().Int("maxRelationDepth", depth).Msg("Set maximum relationship nesting depth")
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
			schema.VendorExtensible.AddExtension(common.ScopeExtensionKey, apiextensionsv1.NamespaceScoped)
		} else {
			schema.VendorExtensible.AddExtension(common.ScopeExtensionKey, apiextensionsv1.ClusterScoped)
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
		resourceSchema.VendorExtensible.AddExtension(common.CategoriesExtensionKey, crd.Spec.Names.Categories)
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
			resourceSchema.VendorExtensible.AddExtension(common.CategoriesExtensionKey, apiResource.Categories)
			b.schemas[resourceKey] = resourceSchema
		}
	}
	return b
}

// WithPreferredVersions populates preferred version information from API discovery
func (b *SchemaBuilder) WithPreferredVersions(apiResLists []*metav1.APIResourceList) *SchemaBuilder {
	for _, apiResList := range apiResLists {
		gv, err := runtimeSchema.ParseGroupVersion(apiResList.GroupVersion)
		if err != nil {
			b.log.Debug().Err(err).Str("groupVersion", apiResList.GroupVersion).Msg("failed to parse group version")
			continue
		}

		for _, resource := range apiResList.APIResources {
			// Create a key for group/kind to track preferred version
			key := fmt.Sprintf("%s/%s", gv.Group, resource.Kind)

			// Store this version as preferred for this group/kind
			// ServerPreferredResources returns the preferred version for each group
			b.preferredVersions[key] = gv.Version

			b.log.Debug().
				Str("group", gv.Group).
				Str("kind", resource.Kind).
				Str("preferredVersion", gv.Version).
				Msg("registered preferred version")
		}
	}
	return b
}

// WithRelationships adds relationship fields to schemas that have *Ref fields
func (b *SchemaBuilder) WithRelationships() *SchemaBuilder {
	// Build kind registry first
	b.buildKindRegistry()

	// For depth=1: use simple relation target tracking (working approach)
	// For depth>1: use iterative expansion (scalable approach)
	if b.maxRelationDepth == 1 {
		b.expandWithSimpleDepthControl()
	} else {
		b.expandWithConfigurableDepthControl()
	}

	return b
}

// expandWithSimpleDepthControl implements the working 1-level depth control
func (b *SchemaBuilder) expandWithSimpleDepthControl() {
	// First pass: identify relation targets
	relationTargets := make(map[string]bool)
	for _, schema := range b.schemas {
		if schema.Properties == nil {
			continue
		}
		for propName := range schema.Properties {
			if !isRefProperty(propName) {
				continue
			}
			baseKind := strings.TrimSuffix(propName, "Ref")
			target := b.findBestResourceForKind(baseKind)
			if target != nil {
				relationTargets[target.SchemaKey] = true
			}
		}
	}

	b.log.Info().
		Int("kindRegistrySize", len(b.kindRegistry)).
		Int("relationTargets", len(relationTargets)).
		Msg("Starting 1-level relationship expansion")

	// Second pass: expand only non-targets
	for schemaKey, schema := range b.schemas {
		if relationTargets[schemaKey] {
			b.log.Debug().Str("schemaKey", schemaKey).Msg("Skipping relation target (1-level depth control)")
			continue
		}
		b.expandRelationshipsSimple(schema, schemaKey)
	}
}

// expandWithConfigurableDepthControl implements scalable depth control for depth > 1
func (b *SchemaBuilder) expandWithConfigurableDepthControl() {
	b.log.Info().
		Int("kindRegistrySize", len(b.kindRegistry)).
		Int("maxRelationDepth", b.maxRelationDepth).
		Msg("Starting configurable relationship expansion")

	// TODO: Implement proper multi-level depth control
	// For now, fall back to simple approach
	b.expandWithSimpleDepthControl()
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

		// Add to kind registry with precise GVK key
		resourceInfo := ResourceInfo{
			Group:     gvk.Group,
			Version:   gvk.Version,
			Kind:      gvk.Kind,
			SchemaKey: schemaKey,
		}

		// Index by full GroupVersionKind for precise lookup (no collisions)
		gvkKey := GroupVersionKind{
			Group:   gvk.Group,
			Version: gvk.Version,
			Kind:    gvk.Kind,
		}
		b.kindRegistry[gvkKey] = resourceInfo
	}

	// No sorting needed - each GVK is now uniquely indexed
	// Check for kinds with multiple resources but no preferred versions
	b.warnAboutMissingPreferredVersions()

	b.log.Debug().Int("gvkCount", len(b.kindRegistry)).Msg("built kind registry for relationships")
}

// TODO: Implement proper multi-level depth calculation when needed
// For now, focusing on the working 1-level depth control

// warnAboutMissingPreferredVersions checks for kinds with multiple resources but no preferred versions
func (b *SchemaBuilder) warnAboutMissingPreferredVersions() {
	// Group resources by kind name to find potential conflicts
	kindGroups := make(map[string][]ResourceInfo)

	for _, resourceInfo := range b.kindRegistry {
		kindKey := strings.ToLower(resourceInfo.Kind)
		kindGroups[kindKey] = append(kindGroups[kindKey], resourceInfo)
	}

	// Check each kind that has multiple resources
	for kindName, resources := range kindGroups {
		if len(resources) <= 1 {
			continue // No conflict possible
		}

		// Check if any of the resources has a preferred version
		hasPreferred := false
		for _, resource := range resources {
			key := fmt.Sprintf("%s/%s", resource.Group, resource.Kind)
			if b.preferredVersions[key] == resource.Version {
				hasPreferred = true
				break
			}
		}

		// Warn if no preferred version found
		if !hasPreferred {
			groups := make([]string, 0, len(resources))
			for _, resource := range resources {
				groups = append(groups, fmt.Sprintf("%s/%s", resource.Group, resource.Version))
			}
			b.log.Warn().
				Str("kind", kindName).
				Strs("availableResources", groups).
				Msg("Multiple resources found for kind with no preferred version - using fallback resolution. Consider setting preferred versions for better API governance.")
		}
	}
}

// findBestResourceForKind finds the best matching resource for a given kind name
// using preferred version logic and group prioritization
func (b *SchemaBuilder) findBestResourceForKind(kindName string) *ResourceInfo {
	// Collect all resources with matching kind name
	candidates := make([]ResourceInfo, 0)

	for gvk, resourceInfo := range b.kindRegistry {
		if strings.EqualFold(gvk.Kind, kindName) {
			candidates = append(candidates, resourceInfo)
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	if len(candidates) == 1 {
		return &candidates[0]
	}

	// Sort candidates using preferred version logic
	slices.SortFunc(candidates, func(a, bRes ResourceInfo) int {
		// 1. Prioritize resources with preferred versions
		aKey := fmt.Sprintf("%s/%s", a.Group, a.Kind)
		bKey := fmt.Sprintf("%s/%s", bRes.Group, bRes.Kind)

		aPreferred := b.preferredVersions[aKey] == a.Version
		bPreferred := b.preferredVersions[bKey] == bRes.Version

		if aPreferred && !bPreferred {
			return -1 // a is preferred, comes first
		}
		if !aPreferred && bPreferred {
			return 1 // b is preferred, comes first
		}

		// 2. If both or neither are preferred, prioritize by group (core comes first)
		if cmp := b.compareGroups(a.Group, bRes.Group); cmp != 0 {
			return cmp
		}

		// 3. Then by version stability (v1 > v1beta1 > v1alpha1)
		if cmp := b.compareVersionStability(a.Version, bRes.Version); cmp != 0 {
			return cmp
		}

		// 4. Finally by schema key for deterministic ordering
		return strings.Compare(a.SchemaKey, bRes.SchemaKey)
	})

	return &candidates[0]
}

// compareGroups prioritizes core Kubernetes groups over custom groups
func (b *SchemaBuilder) compareGroups(groupA, groupB string) int {
	// Core group (empty string) comes first
	if groupA == "" && groupB != "" {
		return -1
	}
	if groupA != "" && groupB == "" {
		return 1
	}

	// k8s.io groups come before custom groups
	aIsK8s := strings.Contains(groupA, "k8s.io")
	bIsK8s := strings.Contains(groupB, "k8s.io")

	if aIsK8s && !bIsK8s {
		return -1
	}
	if !aIsK8s && bIsK8s {
		return 1
	}

	// Otherwise alphabetical
	return strings.Compare(groupA, groupB)
}

// compareVersionStability prioritizes stable versions over beta/alpha
func (b *SchemaBuilder) compareVersionStability(versionA, versionB string) int {
	aStability := b.getVersionStability(versionA)
	bStability := b.getVersionStability(versionB)

	// Lower number = more stable (stable=0, beta=1, alpha=2)
	if aStability != bStability {
		return aStability - bStability
	}

	// Same stability level, compare alphabetically
	return strings.Compare(versionA, versionB)
}

// getVersionStability returns stability priority (lower = more stable)
func (b *SchemaBuilder) getVersionStability(version string) int {
	if strings.Contains(version, "alpha") {
		return 2 // least stable
	}
	if strings.Contains(version, "beta") {
		return 1 // somewhat stable
	}
	return 0 // most stable (v1, v2, etc.)
}

// expandRelationshipsSimple adds relationship fields for the simple 1-level depth control
func (b *SchemaBuilder) expandRelationshipsSimple(schema *spec.Schema, schemaKey string) {
	if schema.Properties == nil {
		return
	}

	for propName := range schema.Properties {
		if !isRefProperty(propName) {
			continue
		}

		baseKind := strings.TrimSuffix(propName, "Ref")

		// Find the best resource for this kind name using preferred version logic
		target := b.findBestResourceForKind(baseKind)
		if target == nil {
			continue
		}

		fieldName := strings.ToLower(baseKind)
		if _, exists := schema.Properties[fieldName]; exists {
			continue
		}

		// Create proper reference - handle empty group (core) properly
		var refPath string
		if target.Group == "" {
			refPath = fmt.Sprintf("#/definitions/%s.%s", target.Version, target.Kind)
		} else {
			refPath = fmt.Sprintf("#/definitions/%s.%s.%s", target.Group, target.Version, target.Kind)
		}
		ref := spec.MustCreateRef(refPath)
		schema.Properties[fieldName] = spec.Schema{SchemaProps: spec.SchemaProps{Ref: ref}}

		b.log.Info().
			Str("sourceField", propName).
			Str("targetField", fieldName).
			Str("targetKind", target.Kind).
			Str("targetGroup", target.Group).
			Str("refPath", refPath).
			Str("sourceSchema", schemaKey).
			Msg("Added relationship field")
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
