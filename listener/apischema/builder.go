package apischema

import (
	"encoding/json"
	"errors"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"maps"
	"slices"
	"strings"

	"github.com/hashicorp/go-multierror"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/client-go/openapi"
	"k8s.io/kube-openapi/pkg/validation/spec"

	"github.com/openmfp/crd-gql-gateway/common"
)

// SchemaBuilder helps construct GraphQL field config arguments
type SchemaBuilder struct {
	schemas map[string]*spec.Schema
	err     *multierror.Error
}

// NewSchemaBuilder initializes a new builder
func NewSchemaBuilder(oc openapi.Client, preferredApiGroups []string) *SchemaBuilder {
	b := &SchemaBuilder{
		schemas: make(map[string]*spec.Schema),
	}

	apiv3Paths, err := oc.Paths()
	if err != nil {
		b.err = multierror.Append(b.err, fmt.Errorf("failed to get OpenAPI paths: %w", err))
		return b
	}

	for path, gv := range apiv3Paths {
		schema, err := getSchemaForPath(preferredApiGroups, path, gv)
		if err != nil {
			//TODO: debug log?
			continue
		}
		maps.Copy(b.schemas, schema)
	}

	return b
}

func (b *SchemaBuilder) WithCRDCategories(crd *apiextensionsv1.CustomResourceDefinition) *SchemaBuilder {
	categories := crd.Spec.Names.Categories
	if len(categories) == 0 {
		return b
	}

	gvk, err := getCRDGroupVersionKind(crd.Spec)
	if err != nil {
		b.err = multierror.Append(b.err, fmt.Errorf("failed to get CRD GVK: %w", err))
		return b
	}

	schema, ok := b.schemas[getOpenAPISchemaKey(*gvk)]
	if !ok {
		return b
	}

	schema.VendorExtensible.AddExtension(common.XKubernetesCategories, categories)
	return b
}

func (b *SchemaBuilder) WithApiResourceCategories(list []*metav1.APIResourceList) *SchemaBuilder {
	for _, apiResourceList := range list {
		for _, apiResource := range apiResourceList.APIResources {
			if apiResource.Categories == nil {
				continue
			}

			gvk, err := getApiResourceGVK(apiResource.Kind, apiResourceList.GroupVersion)
			if err != nil {
				b.err = multierror.Append(b.err, fmt.Errorf("failed to get API resource GVK: %w", err))
				continue
			}

			schema, ok := b.schemas[getOpenAPISchemaKey(*gvk)]
			if !ok {
				continue
			}

			schema.VendorExtensible.AddExtension(common.XKubernetesCategories, apiResource.Categories)
		}
	}

	return b
}

// Complete returns the constructed arguments
func (b *SchemaBuilder) Complete() ([]byte, error) {
	v3JSON, err := json.Marshal(&schemaResponse{
		Components: schemasComponentsWrapper{
			Schemas: b.schemas,
		},
	})
	if err != nil {
		b.err = multierror.Append(b.err, fmt.Errorf("failed to marshal openAPI v3 schema: %w", err))
		return nil, b.err
	}
	v2JSON, err := ConvertJSON(v3JSON)
	if err != nil {
		b.err = multierror.Append(b.err, fmt.Errorf("failed to convert openAPI v3 schema to v2: %w", err))
		return nil, b.err
	}

	return v2JSON, nil
}

func getOpenAPISchemaKey(gvk metav1.GroupVersionKind) string {
	// we need to inverse group to match the schema key(io.openmfp.core.v1alpha1.Account)
	parts := strings.Split(gvk.Group, ".")
	slices.Reverse(parts)
	reversedGroup := strings.Join(parts, ".")

	return fmt.Sprintf("%s.%s.%s", reversedGroup, gvk.Version, gvk.Kind)
}

func getApiResourceGVK(kind, groupVersion string) (*metav1.GroupVersionKind, error) {
	groupVersionSlice := strings.Split(groupVersion, "/")
	if len(groupVersionSlice) != 2 {
		return nil, errors.New("invalid groupVersion")
	}

	return &metav1.GroupVersionKind{
		Group:   groupVersionSlice[0],
		Version: groupVersionSlice[1],
		Kind:    kind,
	}, nil
}
