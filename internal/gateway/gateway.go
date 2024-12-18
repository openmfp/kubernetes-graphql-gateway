package gateway

import (
	"errors"
	"fmt"
	"github.com/go-openapi/spec"
	"github.com/graphql-go/graphql"
	"github.com/openmfp/crd-gql-gateway/internal/resolver"
	"github.com/openmfp/golang-commons/logger"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"regexp"
	"strings"
)

type Provider interface {
	GetSchema() *graphql.Schema
}

type Gateway struct {
	log           *logger.Logger
	resolver      resolver.Provider
	graphqlSchema graphql.Schema

	definitions   spec.Definitions
	subscriptions graphql.Fields
	restMapper    meta.RESTMapper

	// typesCache stores generated GraphQL object types(fields) to prevent redundant repeated generation.
	typesCache map[string]*graphql.Object
	// inputTypesCache stores generated GraphQL input object types(input fields) to prevent redundant repeated generation.
	inputTypesCache map[string]*graphql.InputObject
	// Prevents naming conflict in case of the same Kind name in different groups/versions
	typeNameRegistry map[string]string
}

func New(log *logger.Logger, restMapper meta.RESTMapper, definitions spec.Definitions, resolver resolver.Provider) (*Gateway, error) {
	g := &Gateway{
		log:              log,
		resolver:         resolver,
		definitions:      definitions,
		subscriptions:    graphql.Fields{},
		restMapper:       restMapper,
		typesCache:       make(map[string]*graphql.Object),
		inputTypesCache:  make(map[string]*graphql.InputObject),
		typeNameRegistry: make(map[string]string),
	}

	err := g.generateGraphqlSchema()

	return g, err
}

func (g *Gateway) GetSchema() *graphql.Schema {
	return &g.graphqlSchema
}

func (g *Gateway) generateGraphqlSchema() error {
	rootQueryFields := graphql.Fields{}
	rootMutationFields := graphql.Fields{}
	rootSubscriptionFields := graphql.Fields{}

	for group, groupedResources := range g.getDefinitionsByGroup(g.definitions) {
		queryGroupType := graphql.NewObject(graphql.ObjectConfig{
			Name:   group + "Query",
			Fields: graphql.Fields{},
		})

		mutationGroupType := graphql.NewObject(graphql.ObjectConfig{
			Name:   group + "Mutation",
			Fields: graphql.Fields{},
		})

		for resourceUri, resourceScheme := range groupedResources {
			gvk, err := g.getGroupVersionKind(resourceUri)
			if err != nil {
				g.log.Error().Err(err).Msg("Error parsing group version kind")
				continue
			}

			singular, plural := g.getNames(gvk)

			// Generate both fields and inputFields
			fields, inputFields, err := g.generateGraphQLFields(&resourceScheme, singular, []string{}, make(map[string]bool))
			if err != nil {
				g.log.Error().Err(err).Str("resource", singular).Msg("Error generating fields")
				continue
			}

			if len(fields) == 0 {
				g.log.Debug().Str("resource", singular).Msg("No fields found")
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
				Args:    g.resolver.GetListItemsArguments(),
				Resolve: g.resolver.ListItems(*gvk),
			})

			queryGroupType.AddFieldConfig(singular, &graphql.Field{
				Type:    graphql.NewNonNull(resourceType),
				Args:    g.resolver.GetNameAndNamespaceArguments(),
				Resolve: g.resolver.GetItem(*gvk),
			})

			// Mutation definitions
			mutationGroupType.AddFieldConfig("create"+singular, &graphql.Field{
				Type:    resourceType,
				Args:    g.resolver.GetMutationArguments(resourceInputType),
				Resolve: g.resolver.CreateItem(*gvk),
			})

			mutationGroupType.AddFieldConfig("update"+singular, &graphql.Field{
				Type:    resourceType,
				Args:    g.resolver.GetMutationArguments(resourceInputType),
				Resolve: g.resolver.UpdateItem(*gvk),
			})

			mutationGroupType.AddFieldConfig("delete"+singular, &graphql.Field{
				Type:    graphql.Boolean,
				Args:    g.resolver.GetNameAndNamespaceArguments(),
				Resolve: g.resolver.DeleteItem(*gvk),
			})

			subscriptionSingular := strings.ToLower(fmt.Sprintf("%s_%s", group, singular))
			rootSubscriptionFields[subscriptionSingular] = &graphql.Field{
				Type:        resourceType,
				Args:        g.resolver.GetSubscriptionArguments(true),
				Resolve:     g.resolver.CommonResolver(),
				Subscribe:   g.resolver.SubscribeItem(*gvk),
				Description: fmt.Sprintf("Subscribe to changes of %s", singular),
			}

			subscriptionPlural := strings.ToLower(fmt.Sprintf("%s_%s", group, plural))
			rootSubscriptionFields[subscriptionPlural] = &graphql.Field{
				Type:        graphql.NewList(resourceType),
				Args:        g.resolver.GetSubscriptionArguments(false),
				Resolve:     g.resolver.CommonResolver(),
				Subscribe:   g.resolver.SubscribeItems(*gvk),
				Description: fmt.Sprintf("Subscribe to changes of %s", plural),
			}
		}

		if len(queryGroupType.Fields()) > 0 {
			rootQueryFields[group] = &graphql.Field{
				Type:    queryGroupType,
				Resolve: g.resolver.CommonResolver(),
			}
		}

		if len(mutationGroupType.Fields()) > 0 {
			rootMutationFields[group] = &graphql.Field{
				Type:    mutationGroupType,
				Resolve: g.resolver.CommonResolver(),
			}
		}
	}

	newSchema, err := graphql.NewSchema(graphql.SchemaConfig{
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

	if err != nil {
		g.log.Error().Err(err).Msg("Error creating GraphQL root:alpha")
		return err
	}

	g.graphqlSchema = newSchema

	return nil
}

func (g *Gateway) getNames(gvk *schema.GroupVersionKind) (singular string, plural string) {
	kind := gvk.Kind
	singularName := kind

	// Check if the kind name has already been used for a different group/version
	if existingGroupVersion, exists := g.typeNameRegistry[kind]; exists {
		if existingGroupVersion != gvk.GroupVersion().String() {
			// Conflict detected, append group and version
			groupVersion := strings.ReplaceAll(gvk.GroupVersion().String(), "/", "")
			singularName = kind + groupVersion
		}
	} else {
		// No conflict, register the kind with its group and version
		g.typeNameRegistry[kind] = gvk.GroupVersion().String()
	}

	mapping, err := g.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return singularName, singularName + "s"
	}
	pluralName := cases.Title(language.English).String(mapping.Resource.Resource)

	return singularName, pluralName
}

func (g *Gateway) getDefinitionsByGroup(filteredDefinitions spec.Definitions) map[string]spec.Definitions {
	groups := map[string]spec.Definitions{}
	for key, definition := range filteredDefinitions {
		gvk, err := g.getGroupVersionKind(key)
		if err != nil {
			g.log.Debug().Err(err).Str("resourceKey", key).Msg("Failed to get group version kind")
			continue
		}

		if _, ok := groups[gvk.Group]; !ok {
			groups[gvk.Group] = spec.Definitions{}
		}

		groups[gvk.Group][key] = definition
	}

	return groups
}

func (g *Gateway) generateGraphQLFields(resourceScheme *spec.Schema, typePrefix string, fieldPath []string, processingTypes map[string]bool) (graphql.Fields, graphql.InputObjectConfigFieldMap, error) {
	fields := graphql.Fields{}
	inputFields := graphql.InputObjectConfigFieldMap{}

	for fieldName, fieldSpec := range resourceScheme.Properties {
		sanitizedFieldName := sanitizeFieldName(fieldName)
		currentFieldPath := append(fieldPath, fieldName)

		fieldType, inputFieldType, err := g.convertSwaggerTypeToGraphQL(fieldSpec, typePrefix, currentFieldPath, processingTypes)
		if err != nil {
			return nil, nil, err
		}

		fields[sanitizedFieldName] = &graphql.Field{
			Type:    fieldType,
			Resolve: g.resolver.UnstructuredFieldResolver(fieldName),
		}

		inputFields[sanitizedFieldName] = &graphql.InputObjectFieldConfig{
			Type: inputFieldType,
		}
	}

	return fields, inputFields, nil
}

func (g *Gateway) convertSwaggerTypeToGraphQL(schema spec.Schema, typePrefix string, fieldPath []string, processingTypes map[string]bool) (graphql.Output, graphql.Input, error) {
	if len(schema.Type) == 0 {
		// Handle $ref types
		if schema.Ref.GetURL() != nil {
			refKey := schema.Ref.String()

			// Remove the leading '#/definitions/' from the ref string
			refKey = strings.TrimPrefix(refKey, "#/definitions/")

			// Check if type is already being processed
			if processingTypes[refKey] {
				// Return existing type to prevent infinite recursion
				if existingType, exists := g.typesCache[refKey]; exists {
					existingInputType := g.inputTypesCache[refKey]
					return existingType, existingInputType, nil
				}
				// Return placeholder types to prevent recursion
				return graphql.String, graphql.String, nil
			}

			if refDef, ok := g.definitions[refKey]; ok {
				// Mark as processing
				processingTypes[refKey] = true
				defer delete(processingTypes, refKey)

				fieldType, inputFieldType, err := g.convertSwaggerTypeToGraphQL(refDef, refKey, fieldPath, processingTypes)
				if err != nil {
					return nil, nil, err
				}

				// Store the types
				if objType, ok := fieldType.(*graphql.Object); ok {
					g.typesCache[refKey] = objType
				}
				if inputObjType, ok := inputFieldType.(*graphql.InputObject); ok {
					g.inputTypesCache[refKey] = inputObjType
				}

				return fieldType, inputFieldType, nil
			} else {
				// Definition not found, return string
				return graphql.String, graphql.String, nil
			}
		}
		return graphql.String, graphql.String, nil
	}

	switch schema.Type[0] {
	case "string":
		return graphql.String, graphql.String, nil
	case "integer":
		return graphql.Int, graphql.Int, nil
	case "number":
		return graphql.Float, graphql.Float, nil
	case "boolean":
		return graphql.Boolean, graphql.Boolean, nil
	case "array":
		if schema.Items != nil && schema.Items.Schema != nil {
			itemType, inputItemType, err := g.convertSwaggerTypeToGraphQL(*schema.Items.Schema, typePrefix, fieldPath, processingTypes)
			if err != nil {
				return nil, nil, err
			}
			return graphql.NewList(itemType), graphql.NewList(inputItemType), nil
		}
		return graphql.NewList(graphql.String), graphql.NewList(graphql.String), nil
	case "object":
		return g.handleObjectFieldSpecType(schema, typePrefix, fieldPath, processingTypes)
	default:
		// Handle unexpected types or additional properties
		return graphql.String, graphql.String, nil
	}
}

func (g *Gateway) handleObjectFieldSpecType(fieldSpec spec.Schema, typePrefix string, fieldPath []string, processingTypes map[string]bool) (graphql.Output, graphql.Input, error) {
	if len(fieldSpec.Properties) > 0 {
		typeName := g.generateTypeName(typePrefix, fieldPath)

		// Check if type already generated
		if existingType, exists := g.typesCache[typeName]; exists {
			return existingType, g.inputTypesCache[typeName], nil
		}

		// Store placeholder to prevent recursion
		g.typesCache[typeName] = nil
		g.inputTypesCache[typeName] = nil

		nestedFields, nestedInputFields, err := g.generateGraphQLFields(&fieldSpec, typeName, fieldPath, processingTypes)
		if err != nil {
			return nil, nil, err
		}

		newType := graphql.NewObject(graphql.ObjectConfig{
			Name:   sanitizeFieldName(typeName),
			Fields: nestedFields,
		})

		newInputType := graphql.NewInputObject(graphql.InputObjectConfig{
			Name:   sanitizeFieldName(typeName) + "Input",
			Fields: nestedInputFields,
		})

		// Store the generated types
		g.typesCache[typeName] = newType
		g.inputTypesCache[typeName] = newInputType

		return newType, newInputType, nil
	} else if fieldSpec.AdditionalProperties != nil && fieldSpec.AdditionalProperties.Schema != nil {
		// Hagndle map types
		if len(fieldSpec.AdditionalProperties.Schema.Type) == 1 && fieldSpec.AdditionalProperties.Schema.Type[0] == "string" {
			// This is a map[string]string
			return stringMapScalar, stringMapScalar, nil
		}
	}

	// It's an empty object
	return graphql.String, graphql.String, nil
}

func (g *Gateway) generateTypeName(typePrefix string, fieldPath []string) string {
	name := typePrefix + strings.Join(fieldPath, "")
	return name
}

// io.openmfp.core.v1alpha1.Account

// getGroupVersionKind retrieves the GroupVersionKind for a given resourceKey and its OpenAPI schema.
// It first checks for the 'x-kubernetes-group-version-kind' extension and uses it if available.
// If not, it falls back to parsing the resourceKey.
func (g *Gateway) getGroupVersionKind(resourceKey string) (*schema.GroupVersionKind, error) {
	// First, check if 'x-kubernetes-group-version-kind' extension is present
	resourceSpec, ok := g.definitions[resourceKey]
	if !ok || resourceSpec.Extensions == nil {
		return nil, errors.New("no resource extensions")
	}
	xkGvk, ok := resourceSpec.Extensions["x-kubernetes-group-version-kind"]
	if !ok {
		return nil, errors.New("x-kubernetes-group-version-kind extension not found")
	}
	// xkGvk should be an array of maps
	if gvkList, ok := xkGvk.([]interface{}); ok && len(gvkList) > 0 {
		// Use the first item in the list
		if gvkMap, ok := gvkList[0].(map[string]interface{}); ok {
			group, _ := gvkMap["group"].(string)
			version, _ := gvkMap["version"].(string)
			kind, _ := gvkMap["kind"].(string)

			// Sanitize the group and kind names
			return &schema.GroupVersionKind{
				Group:   g.resolver.SanitizeGroupName(group),
				Version: version,
				Kind:    kind,
			}, nil
		}
	}

	return nil, errors.New("failed to parse x-kubernetes-group-version-kind extension")
}

func sanitizeFieldName(name string) string {
	// Replace any invalid characters with '_'
	name = regexp.MustCompile(`[^_a-zA-Z0-9]`).ReplaceAllString(name, "_")

	// If the name doesn't start with a letter or underscore, prepend '_'
	if !regexp.MustCompile(`^[_a-zA-Z]`).MatchString(name) {
		name = "_" + name
	}

	return name
}
