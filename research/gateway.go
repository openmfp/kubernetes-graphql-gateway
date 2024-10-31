package research

import (
	"fmt"
	"strings"

	"github.com/go-openapi/spec"
	"github.com/graphql-go/graphql"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/restmapper"

	"github.com/openmfp/golang-commons/logger"
)

type Gateway struct {
	discoveryClient     *discovery.DiscoveryClient
	definitions         spec.Definitions
	filteredDefinitions spec.Definitions
	log                 *logger.Logger
	resolver            *Resolver
	restMapper          meta.RESTMapper
	// typesCache stores generated GraphQL object types to prevent duplication.
	typesCache      map[string]*graphql.Object
	inputTypesCache map[string]*graphql.InputObject
}

func New(log *logger.Logger, discoveryClient *discovery.DiscoveryClient, definitions, filteredDefinitions spec.Definitions, resolver *Resolver) (*Gateway, error) {
	groupResources, err := restmapper.GetAPIGroupResources(discoveryClient)
	if err != nil {
		log.Err(err).Msg("Error getting GetAPIGroupResources client")
		return nil, err
	}

	return &Gateway{
		discoveryClient:     discoveryClient,
		definitions:         definitions,
		filteredDefinitions: filteredDefinitions,
		log:                 log,
		resolver:            resolver,
		restMapper:          restmapper.NewDiscoveryRESTMapper(groupResources),
		typesCache:          make(map[string]*graphql.Object),
		inputTypesCache:     make(map[string]*graphql.InputObject),
	}, nil
}
func (g *Gateway) GetGraphqlSchema() (graphql.Schema, error) {
	rootQueryFields := graphql.Fields{}
	rootMutationFields := graphql.Fields{}

	for group, groupedResources := range g.getDefinitionsByGroup() {
		queryGroupType := graphql.NewObject(graphql.ObjectConfig{
			Name:   group + "Type",
			Fields: graphql.Fields{},
		})

		mutationGroupType := graphql.NewObject(graphql.ObjectConfig{
			Name:   group + "Mutation",
			Fields: graphql.Fields{},
		})

		for resourceUri, resourceScheme := range groupedResources {
			gvk, err := getGroupVersionKind(resourceUri)
			if err != nil {
				g.log.Error().Err(err).Msg("Error parsing group version kind")
				continue
			}
			singularResourceName := strings.ToLower(gvk.Kind)

			// Generate both fields and inputFields
			fields, inputFields, err := g.generateGraphQLFields(&resourceScheme, singularResourceName, []string{})
			if err != nil {
				g.log.Error().Err(err).Str("resource", singularResourceName).Msg("Error generating fields")
				continue
			}

			if len(fields) == 0 {
				g.log.Error().Str("resource", singularResourceName).Msg("No fields found")
				continue
			}

			resourceType := graphql.NewObject(graphql.ObjectConfig{
				Name:   singularResourceName,
				Fields: fields,
			})

			resourceInputType := graphql.NewInputObject(graphql.InputObjectConfig{
				Name:   singularResourceName + "Input",
				Fields: inputFields,
			})

			capitalizedResourceName, pluralResourceName, err := g.getCapitalizedAndPluralResourceNames(gvk)
			if err != nil {
				g.log.Error().Err(err).Str("kind", gvk.Kind).Msg("Error getting plural resource name")
				continue
			}

			// Query definitions
			queryGroupType.AddFieldConfig(pluralResourceName, &graphql.Field{
				Type:    graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(resourceType))),
				Args:    g.resolver.getListItemsArguments(),
				Resolve: g.resolver.listItems(gvk),
			})

			queryGroupType.AddFieldConfig(singularResourceName, &graphql.Field{
				Type:    graphql.NewNonNull(resourceType),
				Args:    g.resolver.getItemArguments(),
				Resolve: g.resolver.getItem(gvk),
			})

			// Mutation definitions
			mutationGroupType.AddFieldConfig("create"+capitalizedResourceName, &graphql.Field{
				Type:    resourceType,
				Args:    g.resolver.getMutationArguments(resourceInputType),
				Resolve: g.resolver.createItem(gvk),
			})

			mutationGroupType.AddFieldConfig("update"+capitalizedResourceName, &graphql.Field{
				Type:    resourceType,
				Args:    g.resolver.getMutationArguments(resourceInputType),
				Resolve: g.resolver.updateItem(gvk),
			})

			mutationGroupType.AddFieldConfig("delete"+capitalizedResourceName, &graphql.Field{
				Type:    graphql.Boolean,
				Args:    g.resolver.getDeleteArguments(),
				Resolve: g.resolver.deleteItem(gvk),
			})
		}

		// Add group types to root fields
		rootQueryFields[group] = &graphql.Field{
			Type:    queryGroupType,
			Resolve: func(p graphql.ResolveParams) (interface{}, error) { return p.Source, nil },
		}

		rootMutationFields[group] = &graphql.Field{
			Type:    mutationGroupType,
			Resolve: func(p graphql.ResolveParams) (interface{}, error) { return p.Source, nil },
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
	})
}

func (g *Gateway) getCapitalizedAndPluralResourceNames(gvk schema.GroupVersionKind) (string, string, error) {
	mapping, err := g.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return "", "", err
	}

	return strings.Title(strings.ToLower(gvk.Kind)), mapping.Resource.Resource, nil
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
		}
		return graphql.String, graphql.String, nil
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
