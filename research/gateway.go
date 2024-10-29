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
	typesCache map[string]*graphql.Object
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
	}, nil
}

func (g *Gateway) GetGraphqlSchema() (graphql.Schema, error) {
	rootQueryFields := graphql.Fields{}
	for group, groupedResources := range g.getDefinitionsByGroup() {
		queryGroupType := graphql.NewObject(graphql.ObjectConfig{
			Name:   group + "Type",
			Fields: graphql.Fields{},
		})

		for resourceUri, resourceScheme := range groupedResources {
			gvk, err := getGroupVersionKind(resourceUri)
			if err != nil {
				g.log.Error().Err(err).Msg("Error parsing group version kind")
				continue
			}
			resourceName := gvk.Kind

			// Pass resourceName as typePrefix to guarantee unique type names
			fields, err := g.generateGraphQLFields(&resourceScheme, resourceName, []string{})
			if err != nil {
				g.log.Error().Err(err).Str("resource", resourceName).Msg("Error generating fields")
				continue
			}

			if len(fields) == 0 {
				g.log.Error().Str("resource", resourceName).Msg("No fields found")
				continue
			}

			resourceType := graphql.NewObject(graphql.ObjectConfig{
				Name:   resourceName,
				Fields: fields,
			})

			pluralResourceName, err := g.getPluralResourceName(gvk)
			if err != nil {
				g.log.Error().Err(err).Str("kind", gvk.Kind).Msg("Error getting plural resource name")
				continue
			}

			queryGroupType.AddFieldConfig(pluralResourceName, &graphql.Field{
				Type:    graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(resourceType))),
				Args:    g.resolver.getListArguments(),
				Resolve: g.resolver.listItems(gvk),
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

func (g *Gateway) getPluralResourceName(gvk schema.GroupVersionKind) (string, error) {
	mapping, err := g.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return "", err
	}

	return mapping.Resource.Resource, nil
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

func (g *Gateway) generateGraphQLFields(resourceScheme *spec.Schema, typePrefix string, fieldPath []string) (graphql.Fields, error) {
	fields := graphql.Fields{}

	for fieldName, fieldSpec := range resourceScheme.Properties {
		currentFieldPath := append(fieldPath, fieldName)

		fieldType, err := g.mapSwaggerTypeToGraphQL(fieldSpec, typePrefix, currentFieldPath)
		if err != nil {
			return nil, err
		}

		fields[fieldName] = &graphql.Field{
			Type:    fieldType,
			Resolve: g.resolver.unstructuredFieldResolver(fieldName),
		}
	}

	return fields, nil
}
func (g *Gateway) mapSwaggerTypeToGraphQL(fieldSpec spec.Schema, typePrefix string, fieldPath []string) (graphql.Output, error) {
	if len(fieldSpec.Type) == 0 {
		// Handle $ref types
		if fieldSpec.Ref.GetURL() != nil {
			refTypeName := getTypeNameFromRef(fieldSpec.Ref.String())
			if refDef, ok := g.definitions[refTypeName]; ok {
				return g.mapSwaggerTypeToGraphQL(refDef, typePrefix, fieldPath)
			}
		}
		return graphql.String, nil
	}

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
			itemType, err := g.mapSwaggerTypeToGraphQL(*fieldSpec.Items.Schema, typePrefix, fieldPath)
			if err != nil {
				return nil, err
			}
			return graphql.NewList(itemType), nil
		}
		return graphql.NewList(graphql.String), nil
	case "object":
		if len(fieldSpec.Properties) > 0 {
			typeName := typePrefix + "_" + strings.Join(fieldPath, "_")

			// Check if type already generated
			if existingType, exists := g.typesCache[typeName]; exists {
				return existingType, nil
			}

			nestedFields, err := g.generateGraphQLFields(&fieldSpec, typePrefix, fieldPath)
			if err != nil {
				return nil, err
			}
			newType := graphql.NewObject(graphql.ObjectConfig{
				Name:   typeName,
				Fields: nestedFields,
			})
			// Store the generated type
			g.typesCache[typeName] = newType
			return newType, nil
		}
		return graphql.String, nil
	default:
		// Handle $ref to definitions
		if fieldSpec.Ref.GetURL() != nil {
			refTypeName := getTypeNameFromRef(fieldSpec.Ref.String())
			if refDef, ok := g.definitions[refTypeName]; ok {
				return g.mapSwaggerTypeToGraphQL(refDef, typePrefix, fieldPath)
			}
		}
		return graphql.String, nil
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
