package apischema

import (
	"encoding/json"
	"fmt"
	"github.com/hashicorp/go-multierror"
	"github.com/openmfp/crd-gql-gateway/common"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/client-go/openapi"
	"k8s.io/kube-openapi/pkg/validation/spec"
	"maps"
	"slices"
	"strings"
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

func (b *SchemaBuilder) WithCategories(crd *apiextensionsv1.CustomResourceDefinition) *SchemaBuilder {
	category := crd.Spec.Names.Categories
	if len(category) == 0 {
		return b
	}

	for key, schema := range b.schemas {
		if matchesCRD(crd, key) {
			schema.VendorExtensible.AddExtension(common.XKubernetesCategories, category)
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

func matchesCRD(crd *apiextensionsv1.CustomResourceDefinition, schemaKey string) bool {
	gvk, err := getCRDGroupVersionKind(crd.Spec)
	if err != nil {
		return false
	}

	// we need to inverse group to match the schema key(io.openmfp.core.v1alpha1.Account)
	parts := strings.Split(gvk.Group, ".")
	slices.Reverse(parts)
	reversedGroup := strings.Join(parts, ".")

	expectedKey := fmt.Sprintf("%s.%s.%s", reversedGroup, gvk.Version, gvk.Kind)

	return schemaKey == expectedKey
}
