package research

import (
	"fmt"
	"os"
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
	discoveryClient *discovery.DiscoveryClient
	definitions     spec.Definitions
	log             *logger.Logger
	resolver        *Resolver
	restMapper      meta.RESTMapper
}

func New(log *logger.Logger, discoveryClient *discovery.DiscoveryClient, definitions spec.Definitions, resolver *Resolver) *Gateway {
	groupResources, err := restmapper.GetAPIGroupResources(discoveryClient)
	if err != nil {
		log.Err(err).Msg("Error getting GetAPIGroupResources client")
		os.Exit(1)
	}

	return &Gateway{
		discoveryClient: discoveryClient,
		definitions:     definitions,
		log:             log,
		resolver:        resolver,
		restMapper:      restmapper.NewDiscoveryRESTMapper(groupResources),
	}
}

//
// func (g *Gateway) GetGraphqlSchema() (graphql.Schema, error) {
// 	rootQueryFields := graphql.Fields{}
// 	for group, groupedResources := range g.getDefinitionsByGroup() {
// 		queryGroupType := graphql.NewObject(graphql.ObjectConfig{
// 			Name:   group + "Type",
// 			Fields: graphql.Fields{},
// 		})
//
// 		for resourceUri, resourceScheme := range groupedResources {
// 			gvk, err := getGroupVersionKind(resourceUri)
// 			if err != nil {
// 				g.log.Error().Err(err).Msg("Error parsing group version kind")
// 				continue
// 			}
// 			resourceName := gvk.Kind
//
// 			fields, err := g.generateGraphQLFields(&resourceScheme, resourceName, []string{})
// 			if err != nil {
// 				g.log.Error().Err(err).Str("resource", resourceName).Msg("Error generating fields")
// 				continue
// 			}
//
// 			if len(fields) == 0 {
// 				g.log.Error().Str("resource", resourceName).Msg("No fields found")
// 				continue
// 			}
//
// 			resourceType := graphql.NewObject(graphql.ObjectConfig{
// 				Name:   resourceName,
// 				Fields: fields,
// 			})
//
// 			// resourceType.AddFieldConfig("metadata", &graphql.Field{
// 			// 	Type:        objectMeta,
// 			// 	Description: "Standard object's metadata.",
// 			// 	Resolve: func(p graphql.ResolveParams) (interface{}, error) {
// 			// 		var objMap map[string]interface{}
// 			// 		switch source := p.Source.(type) {
// 			// 		case *unstructured.Unstructured:
// 			// 			objMap = source.Object
// 			// 		case unstructured.Unstructured:
// 			// 			objMap = source.Object
// 			// 		default:
// 			// 			return nil, nil
// 			// 		}
// 			// 		metadata, found, err := unstructured.NestedMap(objMap, "metadata")
// 			// 		if err != nil || !found {
// 			// 			return nil, nil
// 			// 		}
// 			// 		return metadata, nil
// 			// 	},
// 			// })
//
// 			pluralResourceName, err := g.getPluralResourceName(gvk)
// 			if err != nil {
// 				g.log.Error().Err(err).Str("kind", gvk.Kind).Msg("Error getting plural resource name")
// 				continue
// 			}
//
// 			queryGroupType.AddFieldConfig(pluralResourceName, &graphql.Field{
// 				Type:    graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(resourceType))),
// 				Args:    g.resolver.getListArguments(),
// 				Resolve: g.resolver.listItems(gvk),
// 			})
// 		}
//
// 		rootQueryFields[group] = &graphql.Field{
// 			Type:    queryGroupType,
// 			Resolve: func(p graphql.ResolveParams) (interface{}, error) { return p.Source, nil },
// 		}
// 	}
//
// 	return graphql.NewSchema(graphql.SchemaConfig{
// 		Query: graphql.NewObject(graphql.ObjectConfig{
// 			Name:   "Query",
// 			Fields: rootQueryFields,
// 		}),
// 	})
// }

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

			// Pass resourceName as typePrefix
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

// replace it with better option
func (g *Gateway) getPluralResourceName(gvk schema.GroupVersionKind) (string, error) {
	mapping, err := g.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return "", err
	}

	return mapping.Resource.Resource, nil
}

func (g *Gateway) getDefinitionsByGroup() map[string]spec.Definitions {
	groups := map[string]spec.Definitions{}
	for key, definition := range g.definitions {
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
			Resolve: unstructuredFieldResolver(currentFieldPath),
		}
	}

	return fields, nil
}
func (g *Gateway) mapSwaggerTypeToGraphQL(fieldSpec spec.Schema, typePrefix string, fieldPath []string) (graphql.Output, error) {
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
			if existingType, exists := generatedTypes[typeName]; exists {
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
			generatedTypes[typeName] = newType
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
