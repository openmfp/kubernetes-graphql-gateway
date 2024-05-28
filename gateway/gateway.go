package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"slices"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
	"github.com/graphql-go/handler"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	authzv1 "k8s.io/api/authorization/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var stringMapScalar = graphql.NewScalar(graphql.ScalarConfig{
	Name:        "StringMap",
	Description: "A map of strings",
	Serialize: func(value interface{}) interface{} {
		return value
	},
	ParseValue: func(value interface{}) interface{} { return value },
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
			Type: graphql.NewNonNull(graphql.String),
		},
		"namespace": &graphql.Field{
			Type: graphql.NewNonNull(graphql.String),
		},
		"labels": &graphql.Field{
			Type: stringMapScalar,
		},
		"annotations": &graphql.Field{
			Type: stringMapScalar,
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

func gqlTypeForOpenAPIProperties(in map[string]apiextensionsv1.JSONSchemaProps, fields graphql.Fields, inputFields graphql.InputObjectConfigFieldMap, parentFieldName string, requiredKeys []string) (graphql.Fields, graphql.InputObjectConfigFieldMap) {
	for key, info := range in {
		typeKey := strings.ReplaceAll(key, "-", "")
		currentField := &graphql.Field{
			Name:        typeKey,
			Description: info.Description,
		}
		currentInputField := &graphql.InputObjectFieldConfig{
			Description: info.Description,
		}

		switch info.Type {
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
			typeName := parentFieldName + cases.Title(language.English).String(key) + "Item"

			if info.Items.Schema.Properties != nil { // nested array object
				newFields, newInputFields := gqlTypeForOpenAPIProperties(info.Items.Schema.Properties, graphql.Fields{}, graphql.InputObjectConfigFieldMap{}, typeName, info.Items.Schema.Required)
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
				switch info.Items.Schema.Type {
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
			if info.Properties == nil {
				continue
			}
			typeName := parentFieldName + cases.Title(language.English).String(key)
			newFields, newInputFields := gqlTypeForOpenAPIProperties(info.Properties, graphql.Fields{}, graphql.InputObjectConfigFieldMap{}, typeName, info.Required)
			if len(newFields) == 0 {
				slog.Info("skipping creation of subtype due to emtpy field configuration", "type", typeName)
				continue
			}

			newType := graphql.NewObject(graphql.ObjectConfig{
				Name:   parentFieldName + key,
				Fields: newFields,
			})
			newInputType := graphql.NewInputObject(graphql.InputObjectConfig{
				Name:   parentFieldName + key + "Input",
				Fields: newInputFields,
			})

			currentField.Type = newType
			currentInputField.Type = newInputType
		default:
			continue
		}

		if slices.Contains(requiredKeys, key) {
			currentField.Type = graphql.NewNonNull(currentField.Type)
			currentInputField.Type = graphql.NewNonNull(currentInputField.Type)
		}

		fields[typeKey] = currentField
		inputFields[typeKey] = currentInputField
	}

	return fields, inputFields
}

type Config struct {
	Client client.WithWatch

	// optional client.Reader to use for initial crd retrieval
	Reader client.Reader

	pluralToListType   map[string]func() client.ObjectList
	pluralToObjectType map[string]func() client.Object
}

func getListTypesAndCRDsFromScheme(schema *runtime.Scheme, crds []apiextensionsv1.CustomResourceDefinition) (map[string]func() client.ObjectList, map[string]func() client.Object, []apiextensionsv1.CustomResourceDefinition) {
	pluralToList := map[string]func() client.ObjectList{}
	pluralToObject := map[string]func() client.Object{}
	activeCRDs := []apiextensionsv1.CustomResourceDefinition{}

	listInterface := reflect.TypeOf((*client.ObjectList)(nil)).Elem()

	for gvk, knownType := range schema.AllKnownTypes() {

		idx := slices.IndexFunc(crds, func(crd apiextensionsv1.CustomResourceDefinition) bool {
			return strings.Contains(gvk.Kind, crd.Spec.Names.Kind) && crd.Spec.Group == gvk.Group
		})
		if idx == -1 {
			continue
		}

		pluralToObject[crds[idx].Spec.Names.Plural] = func() client.Object {
			return reflect.New(knownType).Interface().(client.Object)
		}

		if !reflect.PointerTo(knownType).Implements(listInterface) {
			continue
		}

		pluralToList[crds[idx].Spec.Names.Plural] = func() client.ObjectList {
			return reflect.New(knownType).Interface().(client.ObjectList)
		}

		activeCRDs = append(activeCRDs, crds[idx])
	}

	return pluralToList, pluralToObject, activeCRDs
}

func crdsByGroup(crds []apiextensionsv1.CustomResourceDefinition) map[string][]apiextensionsv1.CustomResourceDefinition {
	byGroup := map[string][]apiextensionsv1.CustomResourceDefinition{}

	for _, crd := range crds {
		var groupNameBuilder strings.Builder
		for i, part := range strings.Split(crd.Spec.Group, ".") {
			if i == 0 {
				groupNameBuilder.WriteString(part)
				continue
			}
			piece := cases.Title(language.English).String(part)
			groupNameBuilder.WriteString(piece)
		}
		byGroup[groupNameBuilder.String()] = append(byGroup[groupNameBuilder.String()], crd)
	}

	return byGroup
}

func New(ctx context.Context, conf Config) (graphql.Schema, error) {

	if conf.Client == nil {
		return graphql.Schema{}, errors.New("client is required")
	}

	if conf.Reader == nil {
		conf.Reader = conf.Client
	}

	var crdsList apiextensionsv1.CustomResourceDefinitionList
	err := conf.Reader.List(ctx, &crdsList)
	if err != nil {
		return graphql.Schema{}, err
	}

	var crds []apiextensionsv1.CustomResourceDefinition
	conf.pluralToListType, conf.pluralToObjectType, crds = getListTypesAndCRDsFromScheme(conf.Client.Scheme(), crdsList.Items)

	rootQueryFields := graphql.Fields{}
	rootMutationFields := graphql.Fields{}
	subscriptions := graphql.Fields{}

	for group, crds := range crdsByGroup(crds) {

		queryGroupType := graphql.NewObject(graphql.ObjectConfig{
			Name:   group + "Type",
			Fields: graphql.Fields{},
		})

		mutationGroupType := graphql.NewObject(graphql.ObjectConfig{
			Name:   group + "Mutation",
			Fields: graphql.Fields{},
		})

		for _, crd := range crds {

			versionIdx := slices.IndexFunc(crd.Spec.Versions, func(version apiextensionsv1.CustomResourceDefinitionVersion) bool { return version.Storage })
			typeInformation := crd.Spec.Versions[versionIdx]

			fields, inputFields := gqlTypeForOpenAPIProperties(typeInformation.Schema.OpenAPIV3Schema.Properties, graphql.Fields{}, graphql.InputObjectConfigFieldMap{}, crd.Spec.Names.Kind, nil)

			if len(fields) == 0 {
				slog.Info("skip processing of kind due to empty field map", "kind", crd.Spec.Names.Kind)
				continue
			}

			crdType := graphql.NewObject(graphql.ObjectConfig{
				Name:   crd.Spec.Names.Kind,
				Fields: fields,
			})

			crdType.AddFieldConfig("metadata", &graphql.Field{
				Type:        objectMeta,
				Description: "Standard object's metadata.",
			})

			queryGroupType.AddFieldConfig(crd.Spec.Names.Plural, &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(crdType))),
				Args: graphql.FieldConfigArgument{
					"labelselector": &graphql.ArgumentConfig{
						Type:        graphql.String,
						Description: "a label selector to filter the objects by",
					},
					"namespace": &graphql.ArgumentConfig{
						Type:        graphql.String,
						Description: "the namespace in which to search for the objects",
					},
				},
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					ctx, span := otel.Tracer("").Start(p.Context, "Resolve", trace.WithAttributes(attribute.String("kind", crd.Spec.Names.Kind)))
					defer span.End()

					listFunc, ok := conf.pluralToListType[crd.Spec.Names.Plural]
					if !ok {
						return nil, errors.New("no typed client available for the reuqested type")
					}

					list := listFunc()

					var opts []client.ListOption
					if labelSelector, ok := p.Args["labelselector"].(string); ok && labelSelector != "" {
						selector, err := labels.Parse(labelSelector)
						if err != nil {
							slog.Error("unable to parse given label selector", "error", err)
							return nil, err
						}
						opts = append(opts, client.MatchingLabelsSelector{Selector: selector})
					}

					user, ok := p.Context.Value(userContextKey{}).(string)
					if !ok || user == "" {
						return nil, errors.New("no user found in context")
					}

					sar := authzv1.SubjectAccessReview{
						Spec: authzv1.SubjectAccessReviewSpec{
							User: user,
							ResourceAttributes: &authzv1.ResourceAttributes{
								Verb:     "list",
								Group:    crd.Spec.Group,
								Version:  crd.Spec.Versions[versionIdx].Name,
								Resource: crd.Spec.Names.Plural,
							},
						},
					}

					if namespace, ok := p.Args["namespace"].(string); ok && namespace != "" {
						opts = append(opts, client.InNamespace(namespace))
						sar.Spec.ResourceAttributes.Namespace = namespace
					}

					err = conf.Client.Create(ctx, &sar)
					if err != nil {
						return nil, err
					}
					slog.Info("SAR result", "allowed", sar.Status.Allowed, "user", sar.Spec.User, "namespace", sar.Spec.ResourceAttributes.Namespace, "resource", sar.Spec.ResourceAttributes.Resource)

					if !sar.Status.Allowed {
						return nil, errors.New("access denied")
					}

					err = conf.Client.List(ctx, list, opts...)
					if err != nil {
						return nil, err
					}

					items, err := meta.ExtractList(list)
					if err != nil {
						return nil, err
					}

					// the controller-runtime cache returns unordered results so we sort it here
					slices.SortFunc(items, func(a runtime.Object, b runtime.Object) int {
						return strings.Compare(a.(client.Object).GetName(), b.(client.Object).GetName())
					})

					return items, nil
				},
			})

			queryGroupType.AddFieldConfig(crd.Spec.Names.Singular, &graphql.Field{
				Type: graphql.NewNonNull(crdType),
				Args: graphql.FieldConfigArgument{
					"name": &graphql.ArgumentConfig{
						Type:        graphql.NewNonNull(graphql.String),
						Description: "the metadata.name of the object you want to retrieve",
					},
					"namespace": &graphql.ArgumentConfig{
						Type:        graphql.NewNonNull(graphql.String),
						Description: "the metadata.namesapce of the object you want to retrieve",
					},
				},
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					ctx, span := otel.Tracer("").Start(p.Context, "Resolve", trace.WithAttributes(attribute.String("kind", crd.Spec.Names.Kind)))
					defer span.End()

					user, ok := p.Context.Value(userContextKey{}).(string)
					if !ok || user == "" {
						return nil, errors.New("no user found in context")
					}

					name := p.Args["name"].(string)
					namespace := p.Args["namespace"].(string)

					sar := authzv1.SubjectAccessReview{
						Spec: authzv1.SubjectAccessReviewSpec{
							User: user,
							ResourceAttributes: &authzv1.ResourceAttributes{
								Verb:      "get",
								Group:     crd.Spec.Group,
								Version:   crd.Spec.Versions[versionIdx].Name,
								Resource:  crd.Spec.Names.Plural,
								Namespace: namespace,
								Name:      name,
							},
						},
					}

					err = conf.Client.Create(ctx, &sar)
					if err != nil {
						return nil, err
					}
					slog.Info("SAR result", "allowed", sar.Status.Allowed, "user", sar.Spec.User, "namespace", sar.Spec.ResourceAttributes.Namespace, "resource", sar.Spec.ResourceAttributes.Resource)

					if !sar.Status.Allowed {
						return nil, errors.New("access denied")
					}

					objectFunc, ok := conf.pluralToObjectType[crd.Spec.Names.Plural]
					if !ok {
						return nil, errors.New("no typed client available for the reuqested type")
					}

					obj := objectFunc()
					err = conf.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, obj)
					if err != nil {
						return nil, err
					}

					return obj, nil
				},
			})

			capitalizedSingular := cases.Title(language.English).String(crd.Spec.Names.Singular)

			subscriptions[group+capitalizedSingular] = &graphql.Field{
				Type: crdType,
				Args: graphql.FieldConfigArgument{
					"name": &graphql.ArgumentConfig{
						Type:        graphql.NewNonNull(graphql.String),
						Description: "the metadata.name of the object you want to watch",
					},
					"namespace": &graphql.ArgumentConfig{
						Type:        graphql.NewNonNull(graphql.String),
						Description: "the metadata.namesapce of the object you want to watch",
					},
				},
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					return p.Source, nil
				},
				Subscribe: func(p graphql.ResolveParams) (interface{}, error) {
					ctx, span := otel.Tracer("").Start(p.Context, "Subscribe", trace.WithAttributes(attribute.String("kind", crd.Spec.Names.Kind)))
					defer span.End()

					listType, ok := conf.pluralToListType[crd.Spec.Names.Plural]
					if !ok {
						return nil, errors.New("no typed client available for the reuqested type")
					}

					list := listType()

					user, ok := p.Context.Value(userContextKey{}).(string)
					if !ok || user == "" {
						return nil, errors.New("no user found in context")
					}

					sar := authzv1.SubjectAccessReview{
						Spec: authzv1.SubjectAccessReviewSpec{
							User: user,
							ResourceAttributes: &authzv1.ResourceAttributes{
								Verb:      "watch",
								Group:     crd.Spec.Group,
								Version:   crd.Spec.Versions[versionIdx].Name,
								Resource:  crd.Spec.Names.Plural,
								Namespace: p.Args["namespace"].(string),
								Name:      p.Args["name"].(string),
							},
						},
					}

					err = conf.Client.Create(ctx, &sar)
					if err != nil {
						return nil, err
					}
					slog.Info("SAR result", "allowed", sar.Status.Allowed, "user", sar.Spec.User, "namespace", sar.Spec.ResourceAttributes.Namespace, "resource", sar.Spec.ResourceAttributes.Resource)

					if !sar.Status.Allowed {
						return nil, errors.New("access denied")
					}

					listWatch, err := conf.Client.Watch(ctx, list, client.InNamespace(p.Args["namespace"].(string)), client.MatchingFields{
						"metadata.name": p.Args["name"].(string),
					})
					if err != nil {
						return nil, err
					}

					resultChannel := make(chan interface{})
					go func() {
						for ev := range listWatch.ResultChan() {
							select {
							case <-ctx.Done():
								slog.Info("stopping watch due to client cancel")
								listWatch.Stop()
								close(resultChannel)
							default:
								if ev.Type == watch.Bookmark {
									continue
								}

								resultChannel <- ev.Object
							}
						}
					}()

					return resultChannel, nil
				},
			}

			mutationGroupType.AddFieldConfig("delete"+capitalizedSingular, &graphql.Field{
				Type: graphql.Boolean,
				Args: graphql.FieldConfigArgument{
					"name": &graphql.ArgumentConfig{
						Type:        graphql.NewNonNull(graphql.String),
						Description: "the metadata.name of the object you want to delete",
					},
					"namespace": &graphql.ArgumentConfig{
						Type:        graphql.NewNonNull(graphql.String),
						Description: "the metadata.namesapce of the object you want to delete",
					},
				},
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					ctx, span := otel.Tracer("").Start(p.Context, "Delete", trace.WithAttributes(attribute.String("kind", crd.Spec.Names.Kind)))
					defer span.End()

					user, ok := p.Context.Value(userContextKey{}).(string)
					if !ok || user == "" {
						return nil, errors.New("no user found in context")
					}

					sar := authzv1.SubjectAccessReview{
						Spec: authzv1.SubjectAccessReviewSpec{
							User: user,
							ResourceAttributes: &authzv1.ResourceAttributes{
								Verb:      "delete",
								Group:     crd.Spec.Group,
								Version:   crd.Spec.Versions[versionIdx].Name,
								Resource:  crd.Spec.Names.Plural,
								Namespace: p.Args["namespace"].(string),
								Name:      p.Args["name"].(string),
							},
						},
					}

					err = conf.Client.Create(ctx, &sar)
					if err != nil {
						return nil, err
					}
					slog.Info("SAR result", "allowed", sar.Status.Allowed, "user", sar.Spec.User, "namespace", sar.Spec.ResourceAttributes.Namespace, "resource", sar.Spec.ResourceAttributes.Resource)

					if !sar.Status.Allowed {
						return nil, errors.New("access denied")
					}

					us := &unstructured.Unstructured{}
					us.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   crd.Spec.Group,
						Version: crd.Spec.Versions[versionIdx].Name,
						Kind:    crd.Spec.Names.Kind,
					})

					us.SetNamespace(p.Args["namespace"].(string))
					us.SetName(p.Args["name"].(string))

					err = conf.Client.Delete(ctx, us)

					return err == nil, err
				},
			})

			mutationGroupType.AddFieldConfig("create"+capitalizedSingular, &graphql.Field{
				Type: crdType,
				Args: graphql.FieldConfigArgument{
					"spec": &graphql.ArgumentConfig{
						Type: inputFields["spec"].Type,
					},
					"metadata": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(metadataInput),
					},
				},
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					ctx, span := otel.Tracer("").Start(p.Context, "Create", trace.WithAttributes(attribute.String("kind", crd.Spec.Names.Kind)))
					defer span.End()

					user, ok := p.Context.Value(userContextKey{}).(string)
					if !ok || user == "" {
						return nil, errors.New("no user found in context")
					}

					sar := authzv1.SubjectAccessReview{
						Spec: authzv1.SubjectAccessReviewSpec{
							User: user,
							ResourceAttributes: &authzv1.ResourceAttributes{
								Verb:      "create",
								Group:     crd.Spec.Group,
								Version:   crd.Spec.Versions[versionIdx].Name,
								Resource:  crd.Spec.Names.Plural,
								Namespace: p.Args["metadata"].(map[string]interface{})["namespace"].(string),
							},
						},
					}

					err = conf.Client.Create(ctx, &sar)
					if err != nil {
						return nil, err
					}
					slog.Info("SAR result", "allowed", sar.Status.Allowed, "user", sar.Spec.User, "namespace", sar.Spec.ResourceAttributes.Namespace, "resource", sar.Spec.ResourceAttributes.Resource)

					if !sar.Status.Allowed {
						return nil, errors.New("access denied")
					}

					us := &unstructured.Unstructured{}
					us.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   crd.Spec.Group,
						Version: crd.Spec.Versions[versionIdx].Name,
						Kind:    crd.Spec.Names.Kind,
					})

					us.SetNamespace(p.Args["metadata"].(map[string]interface{})["namespace"].(string))
					if name := p.Args["metadata"].(map[string]interface{})["name"]; name != nil {
						us.SetName(name.(string))
					}

					if generateName := p.Args["metadata"].(map[string]interface{})["generateName"]; generateName != nil {
						us.SetGenerateName(generateName.(string))
					}

					if labels := p.Args["metadata"].(map[string]interface{})["labels"]; labels != nil {
						us.SetLabels(labels.(map[string]string))
					}

					if us.GetName() == "" && us.GetGenerateName() == "" {
						return nil, errors.New("either name or generateName must be set")
					}

					unstructured.SetNestedField(us.Object, p.Args["spec"], "spec")

					err = conf.Client.Create(ctx, us)

					return us.Object, err
				},
			})

			mutationGroupType.AddFieldConfig("update"+capitalizedSingular, &graphql.Field{
				Type: crdType,
				Args: graphql.FieldConfigArgument{
					"spec": &graphql.ArgumentConfig{
						Type: inputFields["spec"].Type,
					},
					"metadata": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(metadataInput),
					},
				},
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {

					ctx, span := otel.Tracer("").Start(p.Context, "Update", trace.WithAttributes(attribute.String("kind", crd.Spec.Names.Kind)))
					defer span.End()

					user, ok := p.Context.Value(userContextKey{}).(string)
					if !ok || user == "" {
						return nil, errors.New("no user found in context")
					}

					sar := authzv1.SubjectAccessReview{
						Spec: authzv1.SubjectAccessReviewSpec{
							User: user,
							ResourceAttributes: &authzv1.ResourceAttributes{
								Verb:      "update",
								Group:     crd.Spec.Group,
								Version:   crd.Spec.Versions[versionIdx].Name,
								Resource:  crd.Spec.Names.Plural,
								Namespace: p.Args["metadata"].(map[string]interface{})["namespace"].(string),
								Name:      p.Args["metadata"].(map[string]interface{})["name"].(string),
							},
						},
					}

					err = conf.Client.Create(ctx, &sar)
					if err != nil {
						return nil, err
					}
					slog.Info("SAR result", "allowed", sar.Status.Allowed, "user", sar.Spec.User, "namespace", sar.Spec.ResourceAttributes.Namespace, "resource", sar.Spec.ResourceAttributes.Resource)

					if !sar.Status.Allowed {
						return nil, errors.New("access denied")
					}

					us := &unstructured.Unstructured{}
					us.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   crd.Spec.Group,
						Version: crd.Spec.Versions[versionIdx].Name,
						Kind:    crd.Spec.Names.Kind,
					})

					us.SetNamespace(p.Args["metadata"].(map[string]interface{})["namespace"].(string))
					us.SetName(p.Args["metadata"].(map[string]interface{})["name"].(string))

					err = conf.Client.Get(ctx, client.ObjectKey{Namespace: us.GetNamespace(), Name: us.GetName()}, us)
					if err != nil {
						return nil, err
					}

					unstructured.SetNestedField(us.Object, p.Args["spec"], "spec")

					err = conf.Client.Update(ctx, us)
					if err != nil {
						return nil, err
					}

					return us.Object, nil
				},
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

type userContextKey struct{}

type HandlerConfig struct {
	*handler.Config
	UserClaim string
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

		if r.Header.Get("Accept") == "text/event-stream" {
			opts := handler.NewRequestOptions(r)

			rc := http.NewResponseController(w)
			defer rc.Flush()

			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.Header().Set("Content-Type", "application/json")
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
