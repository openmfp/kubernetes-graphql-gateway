package apischema

import (
	"errors"

	"github.com/getkin/kin-openapi/openapi2"
	openapi_v2 "github.com/google/gnostic-models/openapiv2"
	"sigs.k8s.io/yaml"
)

func convertSchemaRef(ref *openapi2.SchemaRef) (*openapi_v2.Schema, error) {
	if ref == nil {
		return nil, nil
	}

	// Start with an empty Schema
	schema := &openapi_v2.Schema{}

	// Handle Reference
	if ref.Ref != "" {
		schema.XRef = ref.Ref
		return schema, nil
	}

	if ref.Value != nil {

		// Map basic fields
		schema.Title = ref.Value.Title
		schema.Description = ref.Value.Description
		schema.Type = &openapi_v2.TypeItem{
			Value: ref.Value.Type.Slice(),
		}
		//schema.Type.Value = ref.Value.Type.Slice()
		schema.Format = ref.Value.Format

		// Map properties recursively
		if ref.Value.Properties != nil {
			namedSchemas := make([]*openapi_v2.NamedSchema, 0)
			for propName, propRef := range ref.Value.Properties {
				convertedProp, err := convertSchemaRef(propRef)
				if err != nil {
					return nil, err
				}
				namedSchemas = append(namedSchemas, &openapi_v2.NamedSchema{
					Name:  propName,
					Value: convertedProp,
				})
			}
			schema.Properties = &openapi_v2.Properties{
				AdditionalProperties: namedSchemas,
			}
		}

		// Map items (for arrays)
		if ref.Value.Items != nil {
			itemsSchema, err := convertSchemaRef(ref.Value.Items)
			if err != nil {
				return nil, err
			}
			schema.Items = &openapi_v2.ItemsItem{
				Schema: []*openapi_v2.Schema{itemsSchema},
			}
		}

		// Map Extentions
		if ref.Value.Extensions != nil {
			vendorExt := make([]*openapi_v2.NamedAny, 0)
			for extName, extValue := range ref.Value.Extensions {
				yamlValue, err := yaml.Marshal(extValue)
				if err != nil {
					return nil, err
				}
				vendorExt = append(vendorExt, &openapi_v2.NamedAny{
					Name: extName,
					Value: &openapi_v2.Any{
						Yaml: string(yamlValue),
					},
				})
			}
			schema.VendorExtension = vendorExt
		}

	}

	return schema, nil
}

func ConvertToGnosticDocument(kinSwagger *openapi2.T) (*openapi_v2.Document, error) {
	if kinSwagger == nil {
		return nil, errors.New("nil input not allowed")
	}
	gnosticDoc := &openapi_v2.Document{
		Swagger: kinSwagger.Swagger,
		Info: &openapi_v2.Info{
			Title:       kinSwagger.Info.Title,
			Description: kinSwagger.Info.Description,
			Version:     kinSwagger.Info.Version,
		},
	}
	namedSchemas := make([]*openapi_v2.NamedSchema, 0)
	for name, schema := range kinSwagger.Definitions {
		schemaValue, err := convertSchemaRef(schema)
		if err != nil {
			return nil, err
		}
		namedSchemas = append(namedSchemas, &openapi_v2.NamedSchema{
			Name:  name,
			Value: schemaValue,
		})
	}
	gnosticDoc.Definitions = &openapi_v2.Definitions{
		AdditionalProperties: namedSchemas,
	}
	return gnosticDoc, nil
}
