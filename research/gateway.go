package research

import (
	"fmt"
	"github.com/graphql-go/graphql/language/ast"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"strings"

	"github.com/go-openapi/spec"
	"github.com/graphql-go/graphql"
)

var stringMapScalar = graphql.NewScalar(graphql.ScalarConfig{
	Name:        "StringMap",
	Description: "A map of strings, Commonly used for metadata.labels and metadata.annotations.",
	Serialize:   func(value interface{}) interface{} { return value },
	ParseValue:  func(value interface{}) interface{} { return value },
	ParseLiteral: func(valueAST ast.Value) interface{} {
		out := map[string]string{}

		switch value := valueAST.(type) {
		case *ast.ObjectValue:
			for _, field := range value.Fields {
				out[field.Name.Value] = field.Value.GetValue().(string)
			}
		}
		return out
	},
})

var objectMeta = graphql.NewObject(graphql.ObjectConfig{
	Name: "Metadata",
	Fields: graphql.Fields{
		"name": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "the metadata.name of the object",
		},
		"namespace": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "the metadata.namespace of the object",
		},
		"labels": &graphql.Field{
			Type:        stringMapScalar,
			Description: "the metadata.labels of the object",
		},
		"annotations": &graphql.Field{
			Type:        stringMapScalar,
			Description: "the metadata.annotations of the object",
		},
	},
})

var metadataInput = graphql.NewInputObject(graphql.InputObjectConfig{
	Name: "MetadataInput",
	Fields: graphql.InputObjectConfigFieldMap{
		"name": &graphql.InputObjectFieldConfig{
			Type:        graphql.String,
			Description: "the metadata.name of the object you want to create",
		},
		"generateName": &graphql.InputObjectFieldConfig{
			Type:        graphql.String,
			Description: "the metadata.generateName of the object you want to create",
		},
		"namespace": &graphql.InputObjectFieldConfig{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "the metadata.namespace of the object you want to create",
		},
		"labels": &graphql.InputObjectFieldConfig{
			Type:        stringMapScalar,
			Description: "the metadata.labels of the object you want to create",
		},
	},
})

type MetadatInput struct {
	Name         string            `mapstructure:"name,omitempty"`
	GenerateName string            `mapstructure:"generateName,omitempty"`
	Namespace    string            `mapstructure:"namespace,omitempty"`
	Labels       map[string]string `mapstructure:"labels,omitempty"`
}

var typeCache = make(map[string]graphql.Type)
var inputTypeCache = make(map[string]graphql.Input)

func GetGraphqlSchema(definitions spec.Definitions, res *ResolverProvider) (graphql.Schema, error) {
	rootQueryFields := graphql.Fields{}
	for group, groupedResources := range definitionByGroup(definitions) {
		queryGroupType := graphql.NewObject(graphql.ObjectConfig{
			Name:   group + "Type",
			Fields: graphql.Fields{},
		})

		for resourceUri, resourceScheme := range groupedResources {
			gvk, err := parseGroupVersionKind(resourceUri)
			if err != nil {
				fmt.Printf("Error parsing group version kind: %v\n", err)
				continue
			}
			resourceName := gvk.Kind

			fields, err := GenerateGraphQLFields(&resourceScheme, definitions, []string{})
			if err != nil {
				fmt.Printf("Error generating fields for %s: %v\n", resourceName, err)
				continue
			}

			if len(fields) == 0 {
				fmt.Println("### err no fields for", resourceName)
				continue
			}

			resourceType := graphql.NewObject(graphql.ObjectConfig{
				Name:   resourceName,
				Fields: fields,
			})

			resourceType.AddFieldConfig("metadata", &graphql.Field{
				Type: objectMetaType(),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					var objMap map[string]interface{}
					switch source := p.Source.(type) {
					case *unstructured.Unstructured:
						objMap = source.Object
					case unstructured.Unstructured:
						objMap = source.Object
					default:
						return nil, nil
					}
					metadata, found, err := unstructured.NestedMap(objMap, "metadata")
					if err != nil || !found {
						return nil, nil
					}
					return metadata, nil
				},
			})

			pluralResourceName := getPluralResourceName(resourceName)

			queryGroupType.AddFieldConfig(pluralResourceName, &graphql.Field{
				Type:    graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(resourceType))),
				Args:    res.getListArguments(),
				Resolve: res.listItems(gvk),
			})
		}

		rootQueryFields[group] = &graphql.Field{
			Type:    queryGroupType,
			Resolve: func(p graphql.ResolveParams) (interface{}, error) { return p.Source, nil },
		}
	}

	return graphql.NewSchema(graphql.SchemaConfig{
		Query: graphql.NewObject(graphql.ObjectConfig{
			Name:   "Query",
			Fields: rootQueryFields,
		}),
	})
}
func objectMetaType() *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: "ObjectMeta",
		Fields: graphql.Fields{
			"name": &graphql.Field{
				Type:    graphql.String,
				Resolve: unstructuredFieldResolver([]string{"name"}),
			},
			"namespace": &graphql.Field{
				Type:    graphql.String,
				Resolve: unstructuredFieldResolver([]string{"namespace"}),
			},
			"labels": &graphql.Field{
				Type: graphql.NewScalar(graphql.ScalarConfig{
					Name: "LabelsMap",
					Serialize: func(value interface{}) interface{} {
						return value
					},
				}),
				Resolve: unstructuredFieldResolver([]string{"labels"}),
			},
			// Add other metadata fields as needed
		},
	})
}

// replace it with better option
func getPluralResourceName(singularName string) string {
	switch singularName {
	case "Pod":
		return "pods"
	case "Service":
		return "services"
	// Add more cases as needed
	default:
		return singularName + "s" // Simple pluralization, might not work for all resources
	}
}

func definitionByGroup(definitions spec.Definitions) map[string]spec.Definitions {
	groups := map[string]spec.Definitions{}
	for key, definition := range definitions {
		gvk, err := parseGroupVersionKind(key)
		if err != nil {
			fmt.Printf("Error parsing group version kind: %v\n", err)
			continue
		}

		group := gvk.Group
		if group == "" {
			group = "core"
		}

		if _, ok := groups[group]; !ok {
			groups[group] = spec.Definitions{}
		}

		groups[group][key] = definition
	}

	return groups
}

// GenerateGraphQLInputFields converts an OpenAPI schema to both graphql.Fields and graphql.InputObjectConfigFieldMap
func GenerateGraphQLFields(resourceScheme *spec.Schema, definitions spec.Definitions, fieldPath []string) (graphql.Fields, error) {
	fields := graphql.Fields{}

	for fieldName, fieldSpec := range resourceScheme.Properties {
		currentFieldPath := append(fieldPath, fieldName)

		fieldType, err := mapSwaggerTypeToGraphQL(fieldSpec, definitions, currentFieldPath)
		if err != nil {
			return nil, err
		}

		fields[fieldName] = &graphql.Field{
			Type:    fieldType,
			Resolve: unstructuredFieldResolver(currentFieldPath),
		}
	}

	return fields, nil
}
func mapSwaggerTypeToGraphQL(fieldSpec spec.Schema, definitions spec.Definitions, fieldPath []string) (graphql.Output, error) {
	switch fieldSpec.Type[0] {
	case "string":
		return graphql.String, nil
	case "integer":
		return graphql.Int, nil
	case "number":
		return graphql.Float, nil
	case "boolean":
		return graphql.Boolean, nil
	case "array":
		if fieldSpec.Items != nil && fieldSpec.Items.Schema != nil {
			itemType, err := mapSwaggerTypeToGraphQL(*fieldSpec.Items.Schema, definitions, fieldPath)
			if err != nil {
				return nil, err
			}
			return graphql.NewList(graphql.NewNonNull(itemType)), nil
		}
		return graphql.NewList(graphql.String), nil
	case "object":
		if len(fieldSpec.Properties) > 0 {
			nestedFields, err := GenerateGraphQLFields(&fieldSpec, definitions, fieldPath)
			if err != nil {
				return nil, err
			}
			typeName := "Object_" + strings.Join(fieldPath, "_")
			return graphql.NewObject(graphql.ObjectConfig{
				Name:   typeName,
				Fields: nestedFields,
			}), nil
		}
		return graphql.String, nil
	default:
		// Handle $ref to definitions
		if fieldSpec.Ref.GetURL() != nil {
			refTypeName := getTypeNameFromRef(fieldSpec.Ref.String())
			if refDef, ok := definitions[refTypeName]; ok {
				return mapSwaggerTypeToGraphQL(refDef, definitions, fieldPath)
			}
		}
		return graphql.String, nil
	}
}

func getTypeNameFromRef(ref string) string {
	parts := strings.Split(ref, "/")
	return parts[len(parts)-1]
}

func parseGroupVersionKind(resourceKey string) (schema.GroupVersionKind, error) {
	parts := strings.Split(resourceKey, ".")
	if len(parts) < 6 {
		return schema.GroupVersionKind{}, fmt.Errorf("invalid resource key format: %s", resourceKey)
	}

	// The expected structure is: io.k8s.api.<group>.<version>.<kind>
	group := parts[3]
	version := parts[4]
	kind := parts[5]

	// Handle nested kinds (e.g., "ReplicaSetSpec")
	if len(parts) > 6 {
		kind = strings.Join(parts[5:], ".")
	}

	// Special case: the 'core' group has an empty group name in Kubernetes API
	if group == "core" {
		group = ""
	}

	return schema.GroupVersionKind{
		Group:   group,
		Version: version,
		Kind:    kind,
	}, nil
}
