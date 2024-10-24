package main

import (
	"encoding/json"
	"fmt"
	"github.com/graphql-go/graphql/language/ast"
	"github.com/graphql-go/handler"
	"github.com/openmfp/crd-gql-gateway/gateway"
	"io/ioutil"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"context"
	"github.com/go-openapi/spec"
	"github.com/graphql-go/graphql"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

func main() {
	config, err := getKubeConfig()
	if err != nil {
		fmt.Printf("Error getting kubeconfig: %v\n", err)
		os.Exit(1)
	}

	httpClient, err := rest.HTTPClientFor(config)
	if err != nil {
		fmt.Printf("Error creating HTTP client: %v\n", err)
		os.Exit(1)
	}

	url := config.Host + "/openapi/v2"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		os.Exit(1)
	}

	// Add authentication headers if needed
	if config.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+config.BearerToken)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		fmt.Printf("Error making request: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Unexpected status code: %d\n", resp.StatusCode)
		os.Exit(1)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Error reading response body: %v\n", err)
		os.Exit(1)
	}

	var swagger spec.Swagger
	err = json.Unmarshal(body, &swagger)
	if err != nil {
		fmt.Printf("Error unmarshalling OpenAPI schema: %v\n", err)
		os.Exit(1)
	}

	err = spec.ExpandSpec(&swagger, nil)
	if err != nil {
		fmt.Printf("Error expanding OpenAPI schema: %v\n", err)
		os.Exit(1)
	}

	filteredResources := map[string]struct{}{
		"io.k8s.api.core.v1.Pod": {},
		// "io.k8s.api.core.v1.Endpoints": {},
		// "io.k8s.api.core.v1.Service":   {},
	}

	filteredDefinitions := make(map[string]spec.Schema)
	for key, val := range swagger.Definitions {
		if _, ok := filteredResources[key]; ok {
			filteredDefinitions[key] = val
		}
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		fmt.Printf("Error creating dynamic client: %v\n", err)
		os.Exit(1)
	}

	resolver := NewResolver(dynamicClient)

	gqlSchema, err := GetGraphqlSchema(filteredDefinitions, resolver)
	if err != nil {
		fmt.Println("Error creating GraphQL schema")
		panic(err)
	}

	fmt.Println("Server is running on http://localhost:3000/graphql")

	http.Handle("/graphql", gateway.Handler(gateway.HandlerConfig{
		Config: &handler.Config{
			Schema:     &gqlSchema,
			Pretty:     true,
			Playground: true,
		},
		UserClaim:   "mail",
		GroupsClaim: "groups",
	}))

	http.ListenAndServe(":3000", nil)
}

var typeCache = make(map[string]graphql.Type)
var inputTypeCache = make(map[string]graphql.Input)

func GetGraphqlSchema(definitions spec.Definitions, res *resolverProvider) (graphql.Schema, error) {
	rootQueryFields := graphql.Fields{}
	for group, groupedResources := range definitionByGroup(definitions) {
		queryGroupType := graphql.NewObject(graphql.ObjectConfig{
			Name:   group + "Type",
			Fields: graphql.Fields{},
		})

		for resourceName, resourceScheme := range groupedResources {
			fields, _ := GenerateGraphQLInputFields(resourceName, &resourceScheme, definitions)

			if len(fields) == 0 {
				fmt.Println("### err no fields for", resourceName)
				continue
			}

			resourceType := graphql.NewObject(graphql.ObjectConfig{
				Name:   resourceName,
				Fields: fields,
			})

			resourceType.AddFieldConfig("metadata", &graphql.Field{
				Type:        objectMeta,
				Description: "Standard object's metadata.",
			})

			pluralResourceName := getPluralResourceName(resourceName)
			queryGroupType.AddFieldConfig(pluralResourceName, &graphql.Field{
				Type:    graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(resourceType))),
				Args:    res.getListArguments(),
				Resolve: res.listItems(),
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

		groups[group][gvk.Kind] = definition
	}

	return groups
}

// GenerateGraphQLInputFields converts an OpenAPI schema to both graphql.Fields and graphql.InputObjectConfigFieldMap
func GenerateGraphQLInputFields(name string, schema *spec.Schema, definitions spec.Definitions) (graphql.Fields, graphql.InputObjectConfigFieldMap) {
	fields := graphql.Fields{}
	inputFields := graphql.InputObjectConfigFieldMap{}
	for propName, propSchema := range schema.Properties {
		fieldType, inputFieldType := ConvertSchemaToGraphQLType(propName, &propSchema, definitions, []string{name})
		if fieldType != nil {
			fields[propName] = &graphql.Field{
				Type: fieldType,
			}
		}
		if inputFieldType != nil {
			inputFields[propName] = &graphql.InputObjectFieldConfig{
				Type: inputFieldType,
			}
		}
	}
	return fields, inputFields
}

func ConvertSchemaToGraphQLType(name string, schema *spec.Schema, definitions spec.Definitions, parentNames []string) (graphql.Type, graphql.Input) {
	newNameWithParentName := append(parentNames, name)
	typeID := getTypeID(schema, parentNames)
	// Check if the type already exists in the cache
	if outputType, exists := typeCache[typeID]; exists {
		inputType := inputTypeCache[typeID]
		return outputType, inputType
	}

	// Handle $ref
	if schema.Ref.GetURL() != nil {
		ref := schema.Ref.String()
		ref = strings.TrimPrefix(ref, "#/definitions/")
		refSchema, ok := definitions[ref]
		if ok {
			return ConvertSchemaToGraphQLType(ref, &refSchema, definitions, newNameWithParentName)
		}
	}

	var outputType graphql.Type
	var inputType graphql.Input

	switch schema.Type[0] {
	case "string":
		outputType = graphql.String
		inputType = graphql.String
	case "integer":
		outputType = graphql.Int
		inputType = graphql.Int
	case "number":
		outputType = graphql.Float
		inputType = graphql.Float
	case "boolean":
		outputType = graphql.Boolean
		inputType = graphql.Boolean
	case "array":
		itemOutputType, itemInputType := ConvertSchemaToGraphQLType(name, schema.Items.Schema, definitions, newNameWithParentName)
		if itemOutputType != nil {
			outputType = graphql.NewList(itemOutputType)
		}
		if itemInputType != nil {
			inputType = graphql.NewList(itemInputType)
		}
	case "object":
		// Handle additionalProperties for map types
		if schema.AdditionalProperties != nil && schema.AdditionalProperties.Schema != nil {
			valueOutputType, valueInputType := ConvertSchemaToGraphQLType(name, schema.AdditionalProperties.Schema, definitions, newNameWithParentName)
			if valueOutputType != nil {
				outputType = graphql.NewScalar(graphql.ScalarConfig{
					Name: "Map",
				})
			}
			if valueInputType != nil {
				inputType = graphql.NewScalar(graphql.ScalarConfig{
					Name: "Map",
				})
			}
		} else {
			// Handle regular objects
			subFields := graphql.Fields{}
			subInputFields := graphql.InputObjectConfigFieldMap{}
			for propName, propSchema := range schema.Properties {
				fieldType, inputFieldType := ConvertSchemaToGraphQLType(propName, &propSchema, definitions, newNameWithParentName)
				if fieldType != nil {
					subFields[propName] = &graphql.Field{
						Type: fieldType,
					}
				}
				if inputFieldType != nil {
					subInputFields[propName] = &graphql.InputObjectFieldConfig{
						Type: inputFieldType,
					}
				}
			}
			typeName := NormalizeTypeName(newNameWithParentName)
			outputType = graphql.NewObject(graphql.ObjectConfig{
				Name:   typeName,
				Fields: subFields,
			})
			inputType = graphql.NewInputObject(graphql.InputObjectConfig{
				Name:   typeName + "Input",
				Fields: subInputFields,
			})
		}
	}

	// After creating types, store them in cache
	typeCache[typeID] = outputType
	inputTypeCache[typeID] = inputType

	return outputType, inputType
}

func getTypeID(schema *spec.Schema, parentNames []string) string {
	if schema.ID != "" {
		return schema.ID
	}
	return strings.Join(append(parentNames, schema.Title), "_")
}

func NormalizeTypeName(parentNames []string) string {
	name := strings.Join(parentNames, "_")
	name = strings.ReplaceAll(name, ".", "_")
	name = strings.ReplaceAll(name, "-", "_")
	return name
}

// getKubeConfig retrieves Kubernetes configuration
func getKubeConfig() (*rest.Config, error) {
	var kubeconfigPath string
	if envKubeconfig := os.Getenv("KUBECONFIG"); envKubeconfig != "" {
		kubeconfigPath = envKubeconfig
	} else if home := os.Getenv("HOME"); home != "" {
		kubeconfigPath = filepath.Join(home, ".kube", "config")
	} else {
		return nil, fmt.Errorf("cannot find kubeconfig")
	}

	return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
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

// resovler
type resolverProvider struct {
	dynamicClient dynamic.Interface
}

func NewResolver(dynamicClient dynamic.Interface) *resolverProvider {
	return &resolverProvider{
		dynamicClient: dynamicClient,
	}
}

func (r *resolverProvider) listItems() func(p graphql.ResolveParams) (interface{}, error) {
	return func(p graphql.ResolveParams) (interface{}, error) {
		resourceName := p.Info.FieldName
		gvr, err := getGroupVersionResource(resourceName)
		if err != nil {
			return nil, err
		}

		namespace, _ := p.Args["namespace"].(string)
		labelSelector, _ := p.Args["labelselector"].(string)

		listOptions := metav1.ListOptions{
			LabelSelector: labelSelector,
		}

		var list *unstructured.UnstructuredList
		if namespace != "" {
			list, err = r.dynamicClient.Resource(gvr).Namespace(namespace).List(context.Background(), listOptions)
		} else {
			list, err = r.dynamicClient.Resource(gvr).List(context.Background(), listOptions)
		}

		if err != nil {
			return nil, err
		}

		items := list.Items

		// Sort the items by name
		sort.Slice(items, func(i, j int) bool {
			return items[i].GetName() < items[j].GetName()
		})

		return items, nil
	}
}

// Helper function to convert resource name to GroupVersionResource
func getGroupVersionResource(resourceName string) (schema.GroupVersionResource, error) {
	switch resourceName {
	case "pods", "Pod":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}, nil
	case "services", "Service":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}, nil
	// Add more cases for other resource types
	default:
		return schema.GroupVersionResource{}, fmt.Errorf("unknown resource: %s", resourceName)
	}
}

func (r *resolverProvider) getListArguments() graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		"labelselector": &graphql.ArgumentConfig{
			Type:        graphql.String,
			Description: "a label selector to filter the objects by",
		},
		"namespace": &graphql.ArgumentConfig{
			Type:        graphql.String,
			Description: "the namespace in which to search for the objects",
		},
	}
}

func (r *resolverProvider) getItemArguments() graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		"name": &graphql.ArgumentConfig{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "the metadata.name of the object",
		},
		"namespace": &graphql.ArgumentConfig{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "the metadata.namespace of the object",
		},
	}
}

func (r *resolverProvider) getChangeArguments(input graphql.Input) graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		"metadata": &graphql.ArgumentConfig{
			Type:        graphql.NewNonNull(metadataInput),
			Description: "the metadata of the object",
		},
		"spec": &graphql.ArgumentConfig{
			Type:        graphql.NewNonNull(input),
			Description: "the spec of the object",
		},
	}
}

func (r *resolverProvider) getPatchArguments() graphql.FieldConfigArgument {
	return graphql.FieldConfigArgument{
		"type": &graphql.ArgumentConfig{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "The JSON patch type, it can be json-patch, merge-patch, strategic-merge-patch",
		},
		"payload": &graphql.ArgumentConfig{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "The JSON patch to apply to the object",
		},
		"metadata": &graphql.ArgumentConfig{
			Type:        graphql.NewNonNull(metadataInput),
			Description: "Metadata including name and namespace of the object you want to patch",
		},
	}
}
