package native

import (
	"fmt"
	"github.com/go-openapi/spec"
	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
	"github.com/openmfp/golang-commons/logger"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"strings"
)

var stringMapScalar = graphql.NewScalar(graphql.ScalarConfig{
	Name:        "StringMap",
	Description: "A map from strings to strings.",
	Serialize: func(value interface{}) interface{} {
		return value
	},
	ParseValue: func(value interface{}) interface{} {
		return value
	},
	ParseLiteral: func(valueAST ast.Value) interface{} {
		result := map[string]string{}
		switch value := valueAST.(type) {
		case *ast.ObjectValue:
			for _, field := range value.Fields {
				if strValue, ok := field.Value.GetValue().(string); ok {
					result[field.Name.Value] = strValue
				}
			}
		}
		return result
	},
})

type Gateway struct {
	log      *logger.Logger
	resolver *Resolver

	definitions         spec.Definitions
	filteredDefinitions spec.Definitions
	subscriptions       graphql.Fields
	restMapper          meta.RESTMapper

	typesCache      map[string]*graphql.Object // typesCache stores generated GraphQL object types to prevent duplication.
	inputTypesCache map[string]*graphql.InputObject
}

func New(log *logger.Logger, restMapper meta.RESTMapper, definitions, filteredDefinitions spec.Definitions, resolver *Resolver) (*Gateway, error) {
	return &Gateway{
		log:                 log,
		resolver:            resolver,
		definitions:         definitions,
		filteredDefinitions: filteredDefinitions,
		subscriptions:       graphql.Fields{},
		restMapper:          restMapper,
		typesCache:          make(map[string]*graphql.Object),
		inputTypesCache:     make(map[string]*graphql.InputObject),
	}, nil
}

func (g *Gateway) GetGraphqlSchema() (graphql.Schema, error) {
	rootQueryFields := graphql.Fields{}
	rootMutationFields := graphql.Fields{}
	rootSubscriptionFields := graphql.Fields{}

	for group, groupedResources := range g.getDefinitionsByGroup() {
		queryGroupType := graphql.NewObject(graphql.ObjectConfig{
			Name:   group + "Query",
			Fields: graphql.Fields{},
		})

		mutationGroupType := graphql.NewObject(graphql.ObjectConfig{
			Name:   group + "Mutation",
			Fields: graphql.Fields{},
		})

		subscriptionGroupType := graphql.NewObject(graphql.ObjectConfig{
			Name:   group + "Subscription",
			Fields: graphql.Fields{},
		})

		for resourceUri, resourceScheme := range groupedResources {
			gvk, err := getGroupVersionKind(resourceUri)
			if err != nil {
				g.log.Error().Err(err).Msg("Error parsing group version kind")
				continue
			}

			singular, plural := g.getNames(gvk)

			// Generate both fields and inputFields
			fields, inputFields, err := g.generateGraphQLFields(&resourceScheme, singular, []string{})
			if err != nil {
				g.log.Error().Err(err).Str("resource", singular).Msg("Error generating fields")
				continue
			}

			if len(fields) == 0 {
				g.log.Error().Str("resource", singular).Msg("No fields found")
				continue
			}

			resourceType := graphql.NewObject(graphql.ObjectConfig{
				Name:   singular,
				Fields: fields,
			})

			resourceInputType := graphql.NewInputObject(graphql.InputObjectConfig{
				Name:   singular + "Input",
				Fields: inputFields,
			})

			queryGroupType.AddFieldConfig(plural, &graphql.Field{
				Type:    graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(resourceType))),
				Args:    g.resolver.getListItemsArguments(),
				Resolve: g.resolver.listItems(gvk),
			})

			queryGroupType.AddFieldConfig(strings.ToLower(singular), &graphql.Field{
				Type:    graphql.NewNonNull(resourceType),
				Args:    g.resolver.getNameAndNamespaceArguments(),
				Resolve: g.resolver.getItem(gvk),
			})

			// Mutation definitions
			mutationGroupType.AddFieldConfig("create"+singular, &graphql.Field{
				Type:    resourceType,
				Args:    g.resolver.getMutationArguments(resourceInputType),
				Resolve: g.resolver.createItem(gvk),
			})

			mutationGroupType.AddFieldConfig("update"+singular, &graphql.Field{
				Type:    resourceType,
				Args:    g.resolver.getMutationArguments(resourceInputType),
				Resolve: g.resolver.updateItem(gvk),
			})

			mutationGroupType.AddFieldConfig("delete"+singular, &graphql.Field{
				Type:    graphql.Boolean,
				Args:    g.resolver.getNameAndNamespaceArguments(),
				Resolve: g.resolver.deleteItem(gvk),
			})

			subscriptionSingular := "subscribeTo" + singular
			subscriptionGroupType.AddFieldConfig(subscriptionSingular, &graphql.Field{
				Type:        resourceType,
				Args:        g.resolver.getNameAndNamespaceArguments(),
				Resolve:     g.resolver.commonResolver(),
				Subscribe:   g.resolver.subscribeItem(gvk),
				Description: fmt.Sprintf("Subscribe to changes of %s", singular),
			})

			subscriptionPlural := "subscribeTo" + plural
			subscriptionGroupType.AddFieldConfig(subscriptionPlural, &graphql.Field{
				Type:        graphql.NewList(resourceType),
				Args:        g.resolver.getListItemsArguments(),
				Resolve:     g.resolver.commonResolver(),
				Subscribe:   g.resolver.subscribeItems(gvk),
				Description: fmt.Sprintf("Subscribe to changes of %s", plural),
			})
		}

		// Add group types to root fields
		rootQueryFields[group] = &graphql.Field{
			Type:    queryGroupType,
			Resolve: g.resolver.commonResolver(),
		}

		rootMutationFields[group] = &graphql.Field{
			Type:    mutationGroupType,
			Resolve: g.resolver.commonResolver(),
		}

		rootSubscriptionFields[group] = &graphql.Field{
			Type:    subscriptionGroupType,
			Resolve: g.resolver.commonResolver(),
		}
	}

	return graphql.NewSchema(graphql.SchemaConfig{
		Query: graphql.NewObject(graphql.ObjectConfig{
			Name:   "Query",
			Fields: rootQueryFields,
		}),
		Mutation: graphql.NewObject(graphql.ObjectConfig{
			Name:   "Mutation",
			Fields: rootMutationFields,
		}),
		Subscription: graphql.NewObject(graphql.ObjectConfig{
			Name:   "Subscription",
			Fields: rootSubscriptionFields,
		}),
	})
}

func (g *Gateway) getNames(gvk schema.GroupVersionKind) (singular string, plural string) {
	mapping, err := g.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return gvk.Kind, gvk.Kind + "s"
	}

	return gvk.Kind, mapping.Resource.Resource
}

func (g *Gateway) getDefinitionsByGroup() map[string]spec.Definitions {
	groups := map[string]spec.Definitions{}
	for key, definition := range g.filteredDefinitions {
		gvk, err := getGroupVersionKind(key)
		if err != nil {
			g.log.Error().Err(err).Str("resourceKey", key).Msg("Error parsing group version kind")
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

func (g *Gateway) generateGraphQLFields(resourceScheme *spec.Schema, typePrefix string, fieldPath []string) (graphql.Fields, graphql.InputObjectConfigFieldMap, error) {
	fields := graphql.Fields{}
	inputFields := graphql.InputObjectConfigFieldMap{}

	for fieldName, fieldSpec := range resourceScheme.Properties {
		currentFieldPath := append(fieldPath, fieldName)

		fieldType, inputFieldType, err := g.mapSwaggerTypeToGraphQL(fieldSpec, typePrefix, currentFieldPath)
		if err != nil {
			return nil, nil, err
		}

		fields[fieldName] = &graphql.Field{
			Type:    fieldType,
			Resolve: g.resolver.unstructuredFieldResolver(fieldName),
		}

		inputFields[fieldName] = &graphql.InputObjectFieldConfig{
			Type: inputFieldType,
		}
	}

	return fields, inputFields, nil
}

func (g *Gateway) mapSwaggerTypeToGraphQL(fieldSpec spec.Schema, typePrefix string, fieldPath []string) (graphql.Output, graphql.Input, error) {
	if len(fieldSpec.Type) == 0 {
		// Handle $ref types
		if fieldSpec.Ref.GetURL() != nil {
			refTypeName := getTypeNameFromRef(fieldSpec.Ref.String())
			if refDef, ok := g.definitions[refTypeName]; ok {
				return g.mapSwaggerTypeToGraphQL(refDef, typePrefix, fieldPath)
			}
		}
		return graphql.String, graphql.String, nil
	}

	switch fieldSpec.Type[0] {
	case "string":
		return graphql.String, graphql.String, nil
	case "integer":
		return graphql.Int, graphql.Int, nil
	case "number":
		return graphql.Float, graphql.Float, nil
	case "boolean":
		return graphql.Boolean, graphql.Boolean, nil
	case "array":
		if fieldSpec.Items != nil && fieldSpec.Items.Schema != nil {
			itemType, inputItemType, err := g.mapSwaggerTypeToGraphQL(*fieldSpec.Items.Schema, typePrefix, fieldPath)
			if err != nil {
				return nil, nil, err
			}
			return graphql.NewList(itemType), graphql.NewList(inputItemType), nil
		}
		return graphql.NewList(graphql.String), graphql.NewList(graphql.String), nil
	case "object":
		return g.handleObjectFieldSpecType(fieldSpec, typePrefix, fieldPath)
	default:
		// Handle $ref to definitions
		if fieldSpec.Ref.GetURL() != nil {
			refTypeName := getTypeNameFromRef(fieldSpec.Ref.String())
			if refDef, ok := g.definitions[refTypeName]; ok {
				return g.mapSwaggerTypeToGraphQL(refDef, typePrefix, fieldPath)
			}
		}
		return graphql.String, graphql.String, nil
	}
}

func (g *Gateway) handleObjectFieldSpecType(fieldSpec spec.Schema, typePrefix string, fieldPath []string) (graphql.Output, graphql.Input, error) {
	if len(fieldSpec.Properties) > 0 {
		typeName := typePrefix + "_" + strings.Join(fieldPath, "_")

		// Check if type already generated
		if existingType, exists := g.typesCache[typeName]; exists {
			existingInputType := g.inputTypesCache[typeName]
			return existingType, existingInputType, nil
		}

		nestedFields, nestedInputFields, err := g.generateGraphQLFields(&fieldSpec, typePrefix, fieldPath)
		if err != nil {
			return nil, nil, err
		}

		newType := graphql.NewObject(graphql.ObjectConfig{
			Name:   typeName,
			Fields: nestedFields,
		})

		newInputType := graphql.NewInputObject(graphql.InputObjectConfig{
			Name:   typeName + "Input",
			Fields: nestedInputFields,
		})

		// Store the generated types
		g.typesCache[typeName] = newType
		g.inputTypesCache[typeName] = newInputType

		return newType, newInputType, nil
	} else if fieldSpec.AdditionalProperties != nil && fieldSpec.AdditionalProperties.Schema != nil {
		// Handle map types
		if len(fieldSpec.AdditionalProperties.Schema.Type) == 1 && fieldSpec.AdditionalProperties.Schema.Type[0] == "string" {
			// This is a map[string]string
			return stringMapScalar, stringMapScalar, nil
		}
	}

	// It's an empty object
	return graphql.String, graphql.String, nil
}

func getTypeNameFromRef(ref string) string {
	parts := strings.Split(ref, "/")
	return parts[len(parts)-1]
}

func getGroupVersionKind(resourceKey string) (schema.GroupVersionKind, error) {
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
