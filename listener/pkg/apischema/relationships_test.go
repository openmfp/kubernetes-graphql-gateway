package apischema_test

import (
	"testing"

	"github.com/openmfp/golang-commons/logger/testlogger"
	apischema "github.com/openmfp/kubernetes-graphql-gateway/listener/pkg/apischema"
	apimocks "github.com/openmfp/kubernetes-graphql-gateway/listener/pkg/apischema/mocks"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func Test_schema_enforcement_prevents_conflicting_relationship_fields(t *testing.T) {
	mock := apimocks.NewMockClient(t)
	mock.EXPECT().Paths().Return(map[string]openapi.GroupVersion{}, nil)
	b := apischema.NewSchemaBuilder(mock, nil, testlogger.New().Logger)

	// Two schemas with same Kind different groups - should trigger schema enforcement
	first := schemaWithGVK("a.example", "v1", "Thing")
	second := schemaWithGVK("b.example", "v1", "Thing")
	b.SetSchemas(map[string]*spec.Schema{
		"a.example.v1.Thing": first,
		"b.example.v1.Thing": second,
		"c.other.v1.Other":   schemaWithGVK("c.other", "v1", "Other"),
	})

	b.WithRelationships() // indirectly builds the registry

	// Add a schema that references thingRef - should trigger conflict enforcement
	sRef := &spec.Schema{SchemaProps: spec.SchemaProps{Properties: map[string]spec.Schema{
		"thingRef": {SchemaProps: spec.SchemaProps{Type: spec.StringOrArray{"object"}}},
	}}}
	b.GetSchemas()["x.v1.HasThing"] = sRef

	b.WithRelationships()

	// Schema enforcement should PREVENT automatic relationship field generation due to conflicts
	_, hasAutoField := b.GetSchemas()["x.v1.HasThing"].Properties["thing"]
	assert.False(t, hasAutoField, "automatic relationship field should NOT be generated due to conflicts")

	// Schema enforcement should modify the *Ref field to require disambiguation
	thingRefField := b.GetSchemas()["x.v1.HasThing"].Properties["thingRef"]
	assert.Contains(t, thingRefField.Required, "apiGroup", "apiGroup should be required due to conflicts")
	assert.Contains(t, thingRefField.Required, "kind", "kind should be required due to conflicts")
	assert.Contains(t, thingRefField.Description, "Multiple API groups", "description should mention conflicts")
}

func Test_schema_enforcement_with_preferred_versions_still_requires_disambiguation(t *testing.T) {
	mock := apimocks.NewMockClient(t)
	mock.EXPECT().Paths().Return(map[string]openapi.GroupVersion{}, nil)
	b := apischema.NewSchemaBuilder(mock, nil, testlogger.New().Logger)

	// Multiple schemas with same Kind - conflicts exist even with preferred versions
	childA := schemaWithGVK("a.example", "v1", "Child")
	childB := schemaWithGVK("b.example", "v1", "Child")
	childZ := schemaWithGVK("z.last", "v1", "Child") // would be last alphabetically

	b.SetSchemas(map[string]*spec.Schema{
		"a.example.v1.Child": childA,
		"b.example.v1.Child": childB,
		"z.last.v1.Child":    childZ,
	})

	// Set z.last as preferred (even though it would be last alphabetically)
	b.WithPreferredVersions([]*metav1.APIResourceList{
		{
			GroupVersion: "z.last/v1",
			APIResources: []metav1.APIResource{
				{Kind: "Child"},
			},
		},
	})

	b.WithRelationships()

	// Add a parent schema that references childRef
	parentSchema := &spec.Schema{SchemaProps: spec.SchemaProps{Properties: map[string]spec.Schema{
		"childRef": {SchemaProps: spec.SchemaProps{Type: spec.StringOrArray{"object"}}},
	}}}
	b.GetSchemas()["x.v1.Parent"] = parentSchema

	b.WithRelationships()

	// Even with preferred versions, strict mode should prevent automatic field generation with conflicts
	_, hasAutoField := b.GetSchemas()["x.v1.Parent"].Properties["child"]
	assert.False(t, hasAutoField, "automatic relationship field should NOT be generated even with preferred versions when conflicts exist")

	// Schema enforcement should modify the *Ref field to require disambiguation
	childRefField := b.GetSchemas()["x.v1.Parent"].Properties["childRef"]
	assert.Contains(t, childRefField.Required, "apiGroup", "apiGroup should be required due to conflicts")
	assert.Contains(t, childRefField.Required, "kind", "kind should be required due to conflicts")
}

func Test_depth_control_prevents_deep_nesting(t *testing.T) {
	mock := apimocks.NewMockClient(t)
	mock.EXPECT().Paths().Return(map[string]openapi.GroupVersion{}, nil)
	b := apischema.NewSchemaBuilder(mock, nil, testlogger.New().Logger)

	// Create a chain: Root -> Pod -> Service
	// Only Root should get relationship fields, Pod and Service should be marked as targets
	rootSchema := schemaWithGVK("example.com", "v1", "Root")
	rootSchema.Properties = map[string]spec.Schema{
		"podRef": {SchemaProps: spec.SchemaProps{Type: spec.StringOrArray{"object"}}},
	}

	podSchema := schemaWithGVK("", "v1", "Pod") // Core group
	podSchema.Properties = map[string]spec.Schema{
		"serviceRef": {SchemaProps: spec.SchemaProps{Type: spec.StringOrArray{"object"}}},
	}

	serviceSchema := schemaWithGVK("", "v1", "Service") // Core group

	b.SetSchemas(map[string]*spec.Schema{
		"example.com.v1.Root": rootSchema,
		".v1.Pod":             podSchema,
		".v1.Service":         serviceSchema,
	})

	// Verify default depth is 1
	b.WithRelationships()

	schemas := b.GetSchemas()

	// Root should get 'pod' relationship field (depth 0 -> 1)
	_, hasPodField := schemas["example.com.v1.Root"].Properties["pod"]
	assert.True(t, hasPodField, "Root should get pod relationship field")

	// Pod should NOT get 'service' relationship field (would be depth 1 -> 2, exceeds limit)
	_, hasServiceField := schemas[".v1.Pod"].Properties["service"]
	assert.False(t, hasServiceField, "Pod should NOT get service relationship field due to depth limit")

	// Service should not have any relationship fields added
	originalServiceProps := len(serviceSchema.Properties)
	currentServiceProps := len(schemas[".v1.Service"].Properties)
	assert.Equal(t, originalServiceProps, currentServiceProps, "Service should not have relationship fields added")
}

func Test_configurable_depth_control_api(t *testing.T) {
	mock := apimocks.NewMockClient(t)
	mock.EXPECT().Paths().Return(map[string]openapi.GroupVersion{}, nil)
	b := apischema.NewSchemaBuilder(mock, nil, testlogger.New().Logger)

	// Test that WithMaxRelationDepth API works
	b.WithMaxRelationDepth(2)

	// Create simple schemas to verify the builder accepts the API
	rootSchema := schemaWithGVK("example.com", "v1", "Root")
	b.SetSchemas(map[string]*spec.Schema{
		"example.com.v1.Root": rootSchema,
	})

	// Should not panic or error
	b.WithRelationships()

	// For now, even with depth=2, it falls back to depth=1 behavior (as per TODO)
	// This test verifies the API works and doesn't break anything
}

func Test_single_level_prevents_circular_relationships(t *testing.T) {
	mock := apimocks.NewMockClient(t)
	mock.EXPECT().Paths().Return(map[string]openapi.GroupVersion{}, nil)
	b := apischema.NewSchemaBuilder(mock, nil, testlogger.New().Logger)

	// Create circular reference: A -> B, B -> A
	aSchema := schemaWithGVK("example.com", "v1", "A")
	aSchema.Properties = map[string]spec.Schema{
		"bRef": {SchemaProps: spec.SchemaProps{Type: spec.StringOrArray{"object"}}},
	}

	bSchema := schemaWithGVK("example.com", "v1", "B")
	bSchema.Properties = map[string]spec.Schema{
		"aRef": {SchemaProps: spec.SchemaProps{Type: spec.StringOrArray{"object"}}},
	}

	b.SetSchemas(map[string]*spec.Schema{
		"example.com.v1.A": aSchema,
		"example.com.v1.B": bSchema,
	})

	b.WithRelationships()

	schemas := b.GetSchemas()

	// With 1-level depth control, both A and B are marked as relation targets
	// So neither should get automatic relationship fields
	_, hasAField := schemas["example.com.v1.A"].Properties["b"]
	_, hasBField := schemas["example.com.v1.B"].Properties["a"]

	// At least one should not have the field to prevent infinite circular expansion
	circularPrevented := !hasAField || !hasBField
	assert.True(t, circularPrevented, "Circular relationships should be prevented by depth control")
}

func Test_depth_control_with_multiple_chains(t *testing.T) {
	mock := apimocks.NewMockClient(t)
	mock.EXPECT().Paths().Return(map[string]openapi.GroupVersion{}, nil)
	b := apischema.NewSchemaBuilder(mock, nil, testlogger.New().Logger)

	// Multiple chains: Chain1 (Root1 -> Pod), Chain2 (Root2 -> Service)
	root1Schema := schemaWithGVK("example.com", "v1", "Root1")
	root1Schema.Properties = map[string]spec.Schema{
		"podRef": {SchemaProps: spec.SchemaProps{Type: spec.StringOrArray{"object"}}},
	}

	root2Schema := schemaWithGVK("example.com", "v1", "Root2")
	root2Schema.Properties = map[string]spec.Schema{
		"serviceRef": {SchemaProps: spec.SchemaProps{Type: spec.StringOrArray{"object"}}},
	}

	podSchema := schemaWithGVK("", "v1", "Pod")
	serviceSchema := schemaWithGVK("", "v1", "Service")

	b.SetSchemas(map[string]*spec.Schema{
		"example.com.v1.Root1": root1Schema,
		"example.com.v1.Root2": root2Schema,
		".v1.Pod":              podSchema,
		".v1.Service":          serviceSchema,
	})

	b.WithRelationships()

	schemas := b.GetSchemas()

	// Both roots should be able to reference their targets (no conflicts between chains)
	_, hasPodField := schemas["example.com.v1.Root1"].Properties["pod"]
	_, hasServiceField := schemas["example.com.v1.Root2"].Properties["service"]

	assert.True(t, hasPodField, "Root1 should get pod relationship field")
	assert.True(t, hasServiceField, "Root2 should get service relationship field")

	// Targets (Pod, Service) should not get any additional relationship fields
	assert.Empty(t, schemas[".v1.Pod"].Properties, "Pod should not have relationship fields (is a target)")
	assert.Empty(t, schemas[".v1.Service"].Properties, "Service should not have relationship fields (is a target)")
}

func Test_same_kind_different_groups_with_explicit_disambiguation(t *testing.T) {
	mock := apimocks.NewMockClient(t)
	mock.EXPECT().Paths().Return(map[string]openapi.GroupVersion{}, nil)
	b := apischema.NewSchemaBuilder(mock, nil, testlogger.New().Logger)

	// Create two different groups providing the same "Database" kind
	mysqlDB := schemaWithGVK("mysql.example.com", "v1", "Database")
	postgresDB := schemaWithGVK("postgres.example.com", "v1", "Database")

	// Parent schema that wants to reference one of the databases
	appSchema := schemaWithGVK("apps.example.com", "v1", "Application")
	appSchema.Properties = map[string]spec.Schema{
		"databaseRef": {SchemaProps: spec.SchemaProps{Type: spec.StringOrArray{"object"}}},
	}

	b.SetSchemas(map[string]*spec.Schema{
		"mysql.example.com.v1.Database":    mysqlDB,
		"postgres.example.com.v1.Database": postgresDB,
		"apps.example.com.v1.Application":  appSchema,
	})

	b.WithRelationships()

	schemas := b.GetSchemas()

	// Verify schema enforcement was applied
	_, hasAutoField := schemas["apps.example.com.v1.Application"].Properties["database"]
	assert.False(t, hasAutoField, "automatic relationship field should NOT be generated due to kind conflicts")

	// Verify the databaseRef field was modified to require disambiguation
	dbRefField := schemas["apps.example.com.v1.Application"].Properties["databaseRef"]
	assert.NotEmpty(t, dbRefField.Required, "databaseRef should have required fields")
	assert.Contains(t, dbRefField.Required, "apiGroup", "apiGroup should be required to choose between mysql.example.com and postgres.example.com")
	assert.Contains(t, dbRefField.Required, "kind", "kind should be required for disambiguation")

	// Verify the description explains the conflict
	assert.Contains(t, dbRefField.Description, "mysql.example.com", "description should mention mysql group")
	assert.Contains(t, dbRefField.Description, "postgres.example.com", "description should mention postgres group")
	assert.Contains(t, dbRefField.Description, "Multiple API groups", "description should explain the conflict")
}

func Test_same_kind_different_groups_kubernetes_core_vs_custom(t *testing.T) {
	mock := apimocks.NewMockClient(t)
	mock.EXPECT().Paths().Return(map[string]openapi.GroupVersion{}, nil)
	b := apischema.NewSchemaBuilder(mock, nil, testlogger.New().Logger)

	// Simulate core Kubernetes Service vs custom Service
	coreService := schemaWithGVK("", "v1", "Service") // Core group (empty)
	customService := schemaWithGVK("custom.example.com", "v1", "Service")

	// Parent that references "Service" - which one?
	parentSchema := schemaWithGVK("example.com", "v1", "Parent")
	parentSchema.Properties = map[string]spec.Schema{
		"serviceRef": {SchemaProps: spec.SchemaProps{Type: spec.StringOrArray{"object"}}},
	}

	b.SetSchemas(map[string]*spec.Schema{
		".v1.Service":                   coreService,
		"custom.example.com.v1.Service": customService,
		"example.com.v1.Parent":         parentSchema,
	})

	b.WithRelationships()

	schemas := b.GetSchemas()

	// Even with core vs custom, should still require disambiguation
	_, hasAutoField := schemas["example.com.v1.Parent"].Properties["service"]
	assert.False(t, hasAutoField, "automatic relationship field should NOT be generated even for core vs custom conflicts")

	// Schema should enforce disambiguation
	serviceRefField := schemas["example.com.v1.Parent"].Properties["serviceRef"]
	assert.Contains(t, serviceRefField.Required, "apiGroup", "apiGroup should be required to distinguish core vs custom Service")
	assert.Contains(t, serviceRefField.Required, "kind", "kind should be required")

	// Description should mention both core and custom groups
	description := serviceRefField.Description
	assert.Contains(t, description, "core/v1", "description should mention core group")
	assert.Contains(t, description, "custom.example.com/v1", "description should mention custom group")
}

func Test_same_kind_different_groups_with_preferred_version_still_conflicts(t *testing.T) {
	mock := apimocks.NewMockClient(t)
	mock.EXPECT().Paths().Return(map[string]openapi.GroupVersion{}, nil)
	b := apischema.NewSchemaBuilder(mock, nil, testlogger.New().Logger)

	// Multiple "Storage" providers with preferred version set
	s3Storage := schemaWithGVK("aws.example.com", "v1", "Storage")
	gcsStorage := schemaWithGVK("gcp.example.com", "v1", "Storage")
	azureStorage := schemaWithGVK("azure.example.com", "v1", "Storage")

	b.SetSchemas(map[string]*spec.Schema{
		"aws.example.com.v1.Storage":   s3Storage,
		"gcp.example.com.v1.Storage":   gcsStorage,
		"azure.example.com.v1.Storage": azureStorage,
	})

	// Set preferred version for one of them
	b.WithPreferredVersions([]*metav1.APIResourceList{
		{
			GroupVersion: "aws.example.com/v1",
			APIResources: []metav1.APIResource{{Kind: "Storage"}},
		},
	})

	// Parent that wants to reference storage
	appSchema := schemaWithGVK("apps.example.com", "v1", "BackupApp")
	appSchema.Properties = map[string]spec.Schema{
		"storageRef": {SchemaProps: spec.SchemaProps{Type: spec.StringOrArray{"object"}}},
	}
	b.GetSchemas()["apps.example.com.v1.BackupApp"] = appSchema

	b.WithRelationships()

	schemas := b.GetSchemas()

	// Even with preferred version, strict mode should still prevent automatic generation
	_, hasAutoField := schemas["apps.example.com.v1.BackupApp"].Properties["storage"]
	assert.False(t, hasAutoField, "automatic relationship field should NOT be generated even with preferred version when multiple groups provide the same kind")

	// Should still require explicit disambiguation
	storageRefField := schemas["apps.example.com.v1.BackupApp"].Properties["storageRef"]
	assert.Contains(t, storageRefField.Required, "apiGroup", "apiGroup should be required even with preferred version to avoid ambiguity")
}
