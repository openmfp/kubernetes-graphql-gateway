package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	openapi_v2 "github.com/google/gnostic-models/openapiv2"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"log/slog"
	"net/http"
	"reflect"
	"slices"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
	"github.com/graphql-go/handler"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"k8s.io/apimachinery/pkg/runtime"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/client-go/transport"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func gqlTypeForOpenAPIProperties(
	in *openapi_v2.Properties,
	fields graphql.Fields,
	inputFields graphql.InputObjectConfigFieldMap,
	parentFieldName string,
	requiredKeys []string,
) (graphql.Fields, graphql.InputObjectConfigFieldMap) {
	if parentFieldName != "Pod" {
		return fields, inputFields
	}
	for _, property := range in.GetAdditionalProperties() {
		name := strings.ReplaceAll(property.GetName(), "-", "") // not sure if we need this
		currentField := &graphql.Field{
			Name:        name,
			Description: property.GetValue().Description,
		}
		currentInputField := &graphql.InputObjectFieldConfig{
			Description: property.GetValue().Description,
		}

		typeValues := property.GetValue().GetType().GetValue()
		if len(typeValues) < 1 {
			slog.Info("skipping creation of field due to empty type", "field", name)
			continue
		}
		switch typeValues[0] { // the OpenAPI v2 specification (which openapi_v2.Schema is based on) allows for a field to have multiple possible types.
		case "string":
			currentField.Type = graphql.String
			currentInputField.Type = graphql.String
		case "boolean":
			currentField.Type = graphql.Boolean
			currentInputField.Type = graphql.Boolean
		case "integer":
			currentField.Type = graphql.Int
			currentInputField.Type = graphql.Int
		case "array":
			typeName := parentFieldName + cases.Title(language.English).String(name) + "Item"

			if property.GetValue().Properties != nil { // nested array object
				newFields, newInputFields := gqlTypeForOpenAPIProperties(property.GetValue().Properties, graphql.Fields{}, graphql.InputObjectConfigFieldMap{}, typeName, property.GetValue().Required)
				newType := graphql.NewObject(graphql.ObjectConfig{
					Name:   typeName,
					Fields: newFields,
				})
				newInputType := graphql.NewInputObject(graphql.InputObjectConfig{
					Name:   typeName + "Input",
					Fields: newInputFields,
				})
				if len(newFields) == 0 {
					slog.Info("skipping creation of subtype due to emtpy field configuration", "type", typeName)
					continue
				}

				currentField.Type = graphql.NewList(newType)
				currentInputField.Type = graphql.NewList(newInputType)
			} else { // primitive array
				switch property.GetValue().GetType().GetValue()[0] {
				case "string":
					currentField.Type = graphql.String
					currentInputField.Type = graphql.String
				case "boolean":
					currentField.Type = graphql.Boolean
					currentInputField.Type = graphql.Boolean
				case "integer":
					currentField.Type = graphql.Int
					currentInputField.Type = graphql.Int
				}

				currentField.Type = graphql.NewList(currentField.Type)
				currentInputField.Type = graphql.NewList(currentInputField.Type)
			}
		case "object":
			if property.GetValue().GetProperties() == nil {
				continue
			}
			typeName := cases.Title(language.English).String(parentFieldName) + cases.Title(language.English).String(name)
			newFields, newInputFields := gqlTypeForOpenAPIProperties(property.GetValue().GetProperties(), graphql.Fields{}, graphql.InputObjectConfigFieldMap{}, typeName, property.GetValue().GetRequired())
			if len(newFields) == 0 {
				slog.Info("skipping creation of subtype due to emtpy field configuration", "type", typeName)
				continue
			}

			newType := graphql.NewObject(graphql.ObjectConfig{
				Name:   parentFieldName + name,
				Fields: newFields,
			})
			newInputType := graphql.NewInputObject(graphql.InputObjectConfig{
				Name:   typeName + "Input",
				Fields: newInputFields,
			})

			currentField.Type = newType
			currentInputField.Type = newInputType
		default:
			continue
		}

		if slices.Contains(requiredKeys, name) {
			currentField.Type = graphql.NewNonNull(currentField.Type)
			currentInputField.Type = graphql.NewNonNull(currentInputField.Type)
		}

		fields[name] = currentField
		inputFields[name] = currentInputField
	}

	return fields, inputFields
}

func getSchemaForResource(apiResource v1.APIResource, document *openapi_v2.Document) (*openapi_v2.Schema, error) {
	// Construct the OpenAPI definition key based on the APIResource
	key := constructDefinitionKey(apiResource)

	// Retrieve the definitions from the OpenAPI document
	definitions := document.GetDefinitions().GetAdditionalProperties()
	for _, definition := range definitions {
		if definition.GetName() == key {
			return definition.GetValue(), nil
		}
	}

	return nil, fmt.Errorf("schema not found for resource %s", apiResource.Kind)
}

// Construct the key for accessing the schema in the OpenAPI document
func constructDefinitionKey(resource v1.APIResource) string {
	// Normalize group (remove dots)
	group := strings.ReplaceAll(resource.Group, ".", "")

	var key, version string
	if group == "" { // Core group
		if resource.Version == "" {
			version = "v1"
		} else {
			version = resource.Version
		}
		key = fmt.Sprintf("io.k8s.api.core.%s.%s", version, resource.Kind)
	} else {
		key = fmt.Sprintf("io.k8s.api.%s.%s.%s", group, resource.Version, resource.Kind)
	}

	return key
}

// Parse the group and version from groupVersion string
func parseGroupVersion(groupVersion string) (string, string) {
	gvParts := strings.Split(groupVersion, "/")
	if len(gvParts) == 2 {
		return gvParts[0], gvParts[1]
	}
	// Core group (no group part in groupVersion)
	return "", gvParts[0]
}

type Config struct {
	Client    client.WithWatch
	Discovery discovery.DiscoveryInterface
	Document  *openapi_v2.Document

	// optional client.Reader to use for initial crd retrieval
	Reader client.Reader

	pluralToListType   map[string]func() client.ObjectList
	pluralToObjectType map[string]func() client.Object
}

func getTypesAndCRDsFromScheme(schema *runtime.Scheme, apiResourceList []*v1.APIResourceList) (
	map[string]func() client.ObjectList,
	map[string]func() client.Object,
	[]v1.APIResource,
) {
	pluralToList := map[string]func() client.ObjectList{}
	pluralToObject := map[string]func() client.Object{}
	var activeResources []v1.APIResource

	listInterface := reflect.TypeOf((*client.ObjectList)(nil)).Elem()
	objectInterface := reflect.TypeOf((*client.Object)(nil)).Elem()

	for _, apiResourceListItem := range apiResourceList {
		for gvk, knownType := range schema.AllKnownTypes() {
			idx := slices.IndexFunc(apiResourceListItem.APIResources, func(crd v1.APIResource) bool {
				return strings.Contains(gvk.Kind, crd.Kind) && crd.Group == gvk.Group
			})
			if idx == -1 {
				continue
			}

			currentResource := apiResourceListItem.APIResources[idx]

			if reflect.PointerTo(knownType).Implements(objectInterface) && !reflect.PointerTo(knownType).Implements(listInterface) {
				pluralToObject[currentResource.Name] = func() client.Object {
					return reflect.New(knownType).Interface().(client.Object)
				}
			}

			if !reflect.PointerTo(knownType).Implements(listInterface) {
				continue
			}

			pluralToList[currentResource.Name] = func() client.ObjectList {
				return reflect.New(knownType).Interface().(client.ObjectList)
			}

			activeResources = append(activeResources, currentResource)
		}
	}

	return pluralToList, pluralToObject, activeResources
}

func resourceByGroup(activeResources []v1.APIResource) map[string][]v1.APIResource {
	byGroup := map[string][]v1.APIResource{}

	for _, activeResource := range activeResources {
		var groupNameBuilder strings.Builder
		for i, part := range strings.Split(activeResource.Group, ".") {
			if i == 0 {
				groupNameBuilder.WriteString(part)
				continue
			}
			piece := cases.Title(language.English).String(part)
			groupNameBuilder.WriteString(piece)
		}
		byGroup[groupNameBuilder.String()] = append(byGroup[groupNameBuilder.String()], activeResource)
	}

	return byGroup
}

func getRequestedFields(params graphql.ResolveParams) []string {
	fieldSet := make(map[string]struct{})
	var traverseSelections func([]*ast.Field, string)

	traverseSelections = func(fields []*ast.Field, parentPath string) {
		for _, currentField := range fields {
			for _, selection := range currentField.SelectionSet.Selections {
				field, ok := selection.(*ast.Field)
				if !ok || field == nil {
					continue
				}

				fieldPath := field.Name.Value
				if parentPath != "" {
					fieldPath = parentPath + "." + field.Name.Value
				}

				if field.SelectionSet != nil {
					traverseSelections([]*ast.Field{field}, fieldPath)
				} else {
					fieldSet[fieldPath] = struct{}{}
				}
			}
		}
	}

	traverseSelections(params.Info.FieldASTs, "")

	fields := make([]string, 0, len(fieldSet))
	for field := range fieldSet {
		fields = append(fields, field)
	}

	return fields
}

func New(ctx context.Context, conf Config) (graphql.Schema, error) {
	if conf.Client == nil {
		return graphql.Schema{}, errors.New("client is required")
	}

	if conf.Reader == nil {
		conf.Reader = conf.Client
	}

	discoveredResources, err := conf.Discovery.ServerPreferredResources()
	if err != nil {
		return graphql.Schema{}, err
	}

	var activeResources []v1.APIResource
	conf.pluralToListType, conf.pluralToObjectType, activeResources = getTypesAndCRDsFromScheme(conf.Client.Scheme(), discoveredResources)

	rootQueryFields := graphql.Fields{}
	rootMutationFields := graphql.Fields{}
	subscriptions := graphql.Fields{}

	for group, groupedResourceList := range resourceByGroup(activeResources) {
		queryGroupType := graphql.NewObject(graphql.ObjectConfig{
			Name:   group + "Type",
			Fields: graphql.Fields{},
		})

		mutationGroupType := graphql.NewObject(graphql.ObjectConfig{
			Name:   group + "Mutation",
			Fields: graphql.Fields{},
		})

		versionToQueryType := map[string]*graphql.Object{}
		versionToMutationType := map[string]*graphql.Object{}
		resolver := NewResolver(&conf)

		for _, typeInformation := range groupedResourceList {
			if _, ok := versionToQueryType[typeInformation.Name]; ok {
				continue
			}

			versionToQueryType[typeInformation.Name] = graphql.NewObject(graphql.ObjectConfig{
				Name:   typeInformation.Name + "Type",
				Fields: graphql.Fields{},
			})

			versionToMutationType[typeInformation.Name] = graphql.NewObject(graphql.ObjectConfig{
				Name:   typeInformation.Name + "Mutation",
				Fields: graphql.Fields{},
			})

			schema, err := getSchemaForResource(typeInformation, conf.Document)
			if err != nil {
				slog.Error("failed to get schema for resource", "resource", typeInformation.Kind, "error", err)
				continue
			}

			// fields, inputFields := gqlTypeForOpenAPIProperties(
			fields, _ := gqlTypeForOpenAPIProperties(
				schema.GetProperties(), graphql.Fields{}, graphql.InputObjectConfigFieldMap{},
				cases.Title(language.English).String(typeInformation.SingularName), nil)

			if len(fields) == 0 {
				slog.Info("skip processing of kind due to empty field map", "kind", typeInformation.Kind)
				continue
			}

			crdType := graphql.NewObject(graphql.ObjectConfig{
				Name:   typeInformation.Kind,
				Fields: fields,
			})

			crdType.AddFieldConfig("metadata", &graphql.Field{
				Type:        objectMeta,
				Description: "Standard object's metadata.",
			})

			versionedQueryType := versionToQueryType[typeInformation.Name]
			versionedMutationType := versionToMutationType[typeInformation.Name]

			versionedQueryType.AddFieldConfig(typeInformation.Name, &graphql.Field{
				Type:    graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(crdType))),
				Args:    resolver.getListArguments(),
				Resolve: resolver.listItems(typeInformation),
			})

			// versionedQueryType.AddFieldConfig(typeInformation.Name, &graphql.Field{
			// 	Type:    graphql.NewNonNull(crdType),
			// 	Args:    resolver.getItemArguments(),
			// 	Resolve: resolver.getItem(typeInformation),
			// })

			// if typeInformation.Storage {
			// 	queryGroupType.AddFieldConfig(crd.Spec.Names.Plural, &graphql.Field{
			// 		Type:    graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(crdType))),
			// 		Args:    resolver.getListArguments(),
			// 		Resolve: resolver.listItems(crd, typeInformation),
			// 	})
			//
			// 	queryGroupType.AddFieldConfig(crd.Spec.Names.Singular, &graphql.Field{
			// 		Type:    graphql.NewNonNull(crdType),
			// 		Args:    resolver.getItemArguments(),
			// 		Resolve: resolver.getItem(crd, typeInformation),
			// 	})
			// }

			// capitalizedSingular := cases.Title(language.English).String(typeInformation.SingularName)
			// capitalizedPlural := cases.Title(language.English).String(typeInformation.Name)
			//
			// versionedMutationType.AddFieldConfig("delete"+capitalizedSingular, &graphql.Field{
			// 	Type:    graphql.Boolean,
			// 	Args:    resolver.getItemArguments(),
			// 	Resolve: resolver.deleteItem(typeInformation),
			// })
			//
			// versionedMutationType.AddFieldConfig("create"+capitalizedSingular, &graphql.Field{
			// 	Type:    crdType,
			// 	Args:    resolver.getChangeArguments(inputFields["spec"].Type),
			// 	Resolve: resolver.createItem(typeInformation),
			// })
			//
			// versionedMutationType.AddFieldConfig("update"+capitalizedSingular, &graphql.Field{
			// 	Type:    crdType,
			// 	Args:    resolver.getChangeArguments(inputFields["spec"].Type),
			// 	Resolve: resolver.updateItem(typeInformation),
			// })

			// if typeInformation.Storage {
			// 	mutationGroupType.AddFieldConfig("delete"+capitalizedSingular, &graphql.Field{
			// 		Type:    graphql.Boolean,
			// 		Args:    resolver.getItemArguments(),
			// 		Resolve: resolver.deleteItem(crd, typeInformation),
			// 	})
			//
			// 	mutationGroupType.AddFieldConfig("create"+capitalizedSingular, &graphql.Field{
			// 		Type:    crdType,
			// 		Args:    resolver.getChangeArguments(inputFields["spec"].Type),
			// 		Resolve: resolver.createItem(crd, typeInformation),
			// 	})
			//
			// 	mutationGroupType.AddFieldConfig("update"+capitalizedSingular, &graphql.Field{
			// 		Type:    crdType,
			// 		Args:    resolver.getChangeArguments(inputFields["spec"].Type),
			// 		Resolve: resolver.updateItem(crd, typeInformation),
			// 	})
			//
			// 	mutationGroupType.AddFieldConfig("patch"+capitalizedSingular, &graphql.Field{
			// 		Type:    crdType,
			// 		Args:    resolver.getPatchArguments(),
			// 		Resolve: resolver.patchItem(crd, typeInformation),
			// 	})
			// }

			// subscriptions[group+typeInformation.Name+capitalizedSingular] = &graphql.Field{
			// 	Type: crdType,
			// 	Args: graphql.FieldConfigArgument{
			// 		"name": &graphql.ArgumentConfig{
			// 			Type:        graphql.NewNonNull(graphql.String),
			// 			Description: "the metadata.name of the object you want to watch",
			// 		},
			// 		"namespace": &graphql.ArgumentConfig{
			// 			Type:        graphql.NewNonNull(graphql.String),
			// 			Description: "the metadata.namesapce of the objects you want to watch",
			// 		},
			// 		"emitOnlyFieldChanges": &graphql.ArgumentConfig{
			// 			Type:         graphql.Boolean,
			// 			DefaultValue: false,
			// 			Description:  "only emit events if the fields that are requested have changed",
			// 		},
			// 	},
			// 	Resolve: func(p graphql.ResolveParams) (interface{}, error) {
			// 		return p.Source, nil
			// 	},
			// 	Subscribe: resolver.subscribeItem(typeInformation),
			// }
			//
			// subscriptions[group+typeInformation.Name+capitalizedPlural] = &graphql.Field{
			// 	Type: graphql.NewList(crdType),
			// 	Args: graphql.FieldConfigArgument{
			// 		"namespace": &graphql.ArgumentConfig{
			// 			Type:        graphql.NewNonNull(graphql.String),
			// 			Description: "the metadata.namesapce of the objects you want to watch",
			// 		},
			// 		"emitOnlyFieldChanges": &graphql.ArgumentConfig{
			// 			Type:         graphql.Boolean,
			// 			DefaultValue: false,
			// 			Description:  "only emit events if the fields that are requested have changed",
			// 		},
			// 	},
			// 	Resolve: func(p graphql.ResolveParams) (interface{}, error) {
			// 		return p.Source, nil
			// 	},
			// 	Subscribe: resolver.subscribeItems(typeInformation),
			// }

			// if typeInformation.Storage {
			// 	subscriptions[group+capitalizedSingular] = &graphql.Field{
			// 		Type: crdType,
			// 		Args: graphql.FieldConfigArgument{
			// 			"name": &graphql.ArgumentConfig{
			// 				Type:        graphql.NewNonNull(graphql.String),
			// 				Description: "the metadata.name of the object you want to watch",
			// 			},
			// 			"namespace": &graphql.ArgumentConfig{
			// 				Type:        graphql.NewNonNull(graphql.String),
			// 				Description: "the metadata.namesapce of the objects you want to watch",
			// 			},
			// 			"emitOnlyFieldChanges": &graphql.ArgumentConfig{
			// 				Type:         graphql.Boolean,
			// 				DefaultValue: false,
			// 				Description:  "only emit events if the fields that are requested have changed",
			// 			},
			// 		},
			// 		Resolve: func(p graphql.ResolveParams) (interface{}, error) {
			// 			return p.Source, nil
			// 		},
			// 		Subscribe: resolver.subscribeItem(crd, typeInformation),
			// 	}
			// 	subscriptions[group+capitalizedPlural] = &graphql.Field{
			// 		Type: graphql.NewList(crdType),
			// 		Args: graphql.FieldConfigArgument{
			// 			"namespace": &graphql.ArgumentConfig{
			// 				Type:        graphql.NewNonNull(graphql.String),
			// 				Description: "the metadata.namesapce of the objects you want to watch",
			// 			},
			// 			"emitOnlyFieldChanges": &graphql.ArgumentConfig{
			// 				Type:         graphql.Boolean,
			// 				DefaultValue: false,
			// 				Description:  "only emit events if the fields that are requested have changed",
			// 			},
			// 		},
			// 		Resolve: func(p graphql.ResolveParams) (interface{}, error) {
			// 			return p.Source, nil
			// 		},
			// 		Subscribe: resolver.subscribeItems(crd, typeInformation),
			// 	}
			// }

			queryGroupType.AddFieldConfig(typeInformation.Name, &graphql.Field{
				Type:    graphql.NewNonNull(versionedQueryType),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) { return p.Source, nil },
			})

			mutationGroupType.AddFieldConfig(typeInformation.Name, &graphql.Field{
				Type:    graphql.NewNonNull(versionedMutationType),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) { return p.Source, nil },
			})
		}

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
		Subscription: graphql.NewObject(graphql.ObjectConfig{
			Name:   "Subscription",
			Fields: subscriptions,
		}),
	})
}

type (
	userContextKey   struct{}
	groupsContextKey struct{}
)

type HandlerConfig struct {
	*handler.Config
	UserClaim   string
	GroupsClaim string
}

func Handler(conf HandlerConfig) http.Handler {
	h := handler.New(conf.Config)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		authHeader := r.Header.Get("Authorization")
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == "" && (conf.Playground || conf.GraphiQL) {
			h.ServeHTTP(w, r)
			return
		}

		claims := jwt.MapClaims{}
		_, _, err := jwt.NewParser().ParseUnverified(token, claims)
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		userIdentifier, ok := claims[conf.UserClaim].(string)
		if !ok || userIdentifier == "" {
			http.Error(w, "invalid user claim", http.StatusUnauthorized)
			return
		}

		ctx := AddUserToContext(r.Context(), userIdentifier)

		if conf.GroupsClaim != "" {
			groups, ok := claims[conf.GroupsClaim].([]any)

			var parsedGroups []string
			for _, group := range groups {
				if group, ok := group.(string); ok {
					parsedGroups = append(parsedGroups, group)
				}
			}

			if ok && len(groups) >= 0 {
				ctx = AddGroupsToContext(ctx, parsedGroups)
			}
		}

		if r.Header.Get("Accept") == "text/event-stream" {
			opts := handler.NewRequestOptions(r)

			rc := http.NewResponseController(w)
			defer rc.Flush()

			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, ":\n\n")
			rc.Flush()

			subscriptionChannel := graphql.Subscribe(graphql.Params{
				Context:        ctx,
				Schema:         *conf.Schema,
				RequestString:  opts.Query,
				VariableValues: opts.Variables,
			})

			for result := range subscriptionChannel {
				b, _ := json.Marshal(result)
				fmt.Fprintf(w, "event: next\ndata: %s\n\n", b)
				rc.Flush()
			}

			fmt.Fprint(w, "event: complete\n\n")
			return
		}

		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

func AddUserToContext(ctx context.Context, user string) context.Context {
	return context.WithValue(ctx, userContextKey{}, user)
}

func AddGroupsToContext(ctx context.Context, groups []string) context.Context {
	return context.WithValue(ctx, groupsContextKey{}, groups)
}

func GetUserFromContext(ctx context.Context) (string, bool) {
	user, ok := ctx.Value(userContextKey{}).(string)
	return user, ok
}

func GetGroupsFromContext(ctx context.Context) ([]string, bool) {
	groups, ok := ctx.Value(groupsContextKey{}).([]string)
	return groups, ok
}

type impersonation struct {
	delegate http.RoundTripper
}

func (i *impersonation) RoundTrip(req *http.Request) (*http.Response, error) {

	// use the user header as marker for the rest.
	if len(req.Header.Get(transport.ImpersonateUserHeader)) != 0 {
		return i.delegate.RoundTrip(req)
	}

	if strings.Contains(req.URL.Path, "authorization.k8s.io") { // skip impersonation for subjectaccessreviews
		return i.delegate.RoundTrip(req)
	}

	user, ok := GetUserFromContext(req.Context())
	if !ok || user == "" {
		return i.delegate.RoundTrip(req)
	}

	slog.Debug("impersonating request", "user", user)

	req = utilnet.CloneRequest(req)
	req.Header.Set(transport.ImpersonateUserHeader, user)

	groups, ok := GetGroupsFromContext(req.Context())
	if ok && len(groups) > 0 {
		for _, group := range groups {
			req.Header.Set(transport.ImpersonateGroupHeader, group)
		}
	}

	return i.delegate.RoundTrip(req)
}

func NewImpersonationTransport(rt http.RoundTripper) http.RoundTripper {
	return &impersonation{delegate: rt}
}
