package apischema_test

import (
	"testing"

	"github.com/openmfp/golang-commons/logger/testlogger"
	apischema "github.com/openmfp/kubernetes-graphql-gateway/listener/pkg/apischema"
	apimocks "github.com/openmfp/kubernetes-graphql-gateway/listener/pkg/apischema/mocks"
	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/openapi"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

// helper constructs a schema with x-kubernetes-group-version-kind
func schemaWithGVK(group, version, kind string) *spec.Schema {
	return &spec.Schema{
		VendorExtensible: spec.VendorExtensible{Extensions: map[string]interface{}{
			"x-kubernetes-group-version-kind": []map[string]string{{
				"group":   group,
				"version": version,
				"kind":    kind,
			}},
		}},
	}
}

func Test_with_relationships_adds_single_target_field(t *testing.T) {
	mock := apimocks.NewMockClient(t)
	mock.EXPECT().Paths().Return(map[string]openapi.GroupVersion{}, nil)
	b := apischema.NewSchemaBuilder(mock, nil, testlogger.New().Logger)

	// definitions contain a target kind Role in group g/v1
	roleKey := "g.v1.Role"
	roleSchema := schemaWithGVK("g", "v1", "Role")

	// source schema that has roleRef
	sourceKey := "g2.v1.Binding"
	sourceSchema := &spec.Schema{SchemaProps: spec.SchemaProps{Properties: map[string]spec.Schema{
		"roleRef": {SchemaProps: spec.SchemaProps{Type: spec.StringOrArray{"object"}}},
	}}}

	b.SetSchemas(map[string]*spec.Schema{
		roleKey:   roleSchema,
		sourceKey: sourceSchema,
	})

	b.WithRelationships()

	// Expect that role field was added referencing the Role definition
	added, ok := b.GetSchemas()[sourceKey].Properties["role"]
	assert.True(t, ok, "expected relationship field 'role' to be added")
	assert.True(t, added.Ref.GetURL() != nil, "expected $ref on relationship field")
	assert.Contains(t, added.Ref.String(), "#/definitions/g.v1.Role")
}

func Test_build_kind_registry_lowercases_keys_and_picks_first(t *testing.T) {
	mock := apimocks.NewMockClient(t)
	mock.EXPECT().Paths().Return(map[string]openapi.GroupVersion{}, nil)
	b := apischema.NewSchemaBuilder(mock, nil, testlogger.New().Logger)

	// Two schemas with same Kind different groups/versions - first should win
	first := schemaWithGVK("a.example", "v1", "Thing")
	second := schemaWithGVK("b.example", "v1", "Thing")
	b.SetSchemas(map[string]*spec.Schema{
		"a.example.v1.Thing": first,
		"b.example.v1.Thing": second,
		"c.other.v1.Other":   schemaWithGVK("c.other", "v1", "Other"),
	})

	b.WithRelationships() // indirectly builds the registry

	// validate lowercase key exists and contains both, but expansion uses first (covered by previous test)
	// we assert the registry was built by triggering another schema that references thingRef
	sRef := &spec.Schema{SchemaProps: spec.SchemaProps{Properties: map[string]spec.Schema{
		"thingRef": {SchemaProps: spec.SchemaProps{Type: spec.StringOrArray{"object"}}},
	}}}
	b.GetSchemas()["x.v1.HasThing"] = sRef

	b.WithRelationships()
	added, ok := b.GetSchemas()["x.v1.HasThing"].Properties["thing"]
	assert.True(t, ok, "expected relationship field 'thing'")
	// ensure it referenced the first group
	assert.Contains(t, added.Ref.String(), "#/definitions/a.example.v1.Thing")
}
