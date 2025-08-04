package schema

import (
	"encoding/json"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
)

var stringMapScalar = graphql.NewScalar(graphql.ScalarConfig{
	Name:        "StringMap",
	Description: "A map from strings to strings.",
	Serialize: func(value interface{}) interface{} {
		return value
	},
	ParseValue: func(value interface{}) interface{} {
		switch val := value.(type) {
		case map[string]interface{}, map[string]string:
			return val
		default:
			return nil // to tell GraphQL that the value is invalid
		}
	},
	ParseLiteral: func(valueAST ast.Value) interface{} {
		switch value := valueAST.(type) {
		case *ast.ObjectValue:
			result := map[string]string{}
			for _, field := range value.Fields {
				if strValue, ok := field.Value.GetValue().(string); ok {
					result[field.Name.Value] = strValue
				}
			}
			return result
		default:
			return nil // to tell GraphQL that the value is invalid
		}
	},
})

var jsonStringScalar = graphql.NewScalar(graphql.ScalarConfig{
	Name:        "JSONString",
	Description: "A JSON-serialized string representation of any object.",
	Serialize: func(value interface{}) interface{} {
		// Convert the value to JSON string
		jsonBytes, err := json.Marshal(value)
		if err != nil {
			// Fallback to empty JSON object if marshaling fails
			return "{}"
		}
		return string(jsonBytes)
	},
	ParseValue: func(value interface{}) interface{} {
		if str, ok := value.(string); ok {
			var result interface{}
			err := json.Unmarshal([]byte(str), &result)
			if err != nil {
				return nil // Invalid JSON
			}
			return result
		}
		return nil
	},
	ParseLiteral: func(valueAST ast.Value) interface{} {
		if value, ok := valueAST.(*ast.StringValue); ok {
			var result interface{}
			err := json.Unmarshal([]byte(value.Value), &result)
			if err != nil {
				return nil // Invalid JSON
			}
			return result
		}
		return nil
	},
})

// Label represents a single key-value label pair
type Label struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// LabelType defines the GraphQL object type for a single label
var LabelType = graphql.NewObject(graphql.ObjectConfig{
	Name:        "Label",
	Description: "A key-value label pair that supports keys with dots and special characters",
	Fields: graphql.Fields{
		"key": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "The label key (can contain dots and special characters)",
		},
		"value": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "The label value",
		},
	},
})

// LabelInputType defines the GraphQL input type for a single label
var LabelInputType = graphql.NewInputObject(graphql.InputObjectConfig{
	Name:        "LabelInput",
	Description: "Input type for a key-value label pair",
	Fields: graphql.InputObjectConfigFieldMap{
		"key": &graphql.InputObjectFieldConfig{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "The label key (can contain dots and special characters)",
		},
		"value": &graphql.InputObjectFieldConfig{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "The label value",
		},
	},
})
