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

var stringMapInput = graphql.NewScalar(graphql.ScalarConfig{
	Name:        "StringMapInput",
	Description: "Input type for a map from strings to strings.",
	Serialize: func(value interface{}) interface{} {
		return value
	},
	ParseValue: func(value interface{}) interface{} {
		// Handle array of {key, value} objects from GraphQL variables
		if arr, ok := value.([]interface{}); ok {
			result := make(map[string]string)
			for _, item := range arr {
				if obj, ok := item.(map[string]interface{}); ok {
					if key, keyOk := obj["key"].(string); keyOk {
						if val, valOk := obj["value"].(string); valOk {
							result[key] = val
						}
					}
				}
			}
			return result
		}
		// Also handle direct maps for backwards compatibility
		switch val := value.(type) {
		case map[string]interface{}, map[string]string:
			return val
		default:
			return nil // to tell GraphQL that the value is invalid
		}
	},
	ParseLiteral: func(valueAST ast.Value) any {
		switch value := valueAST.(type) {
		case *ast.ListValue:
			result := make(map[string]string)
			for _, item := range value.Values {
				obj, ok := item.(*ast.ObjectValue)
				if !ok {
					continue
				}

				var key, val string
				var hasKey, hasValue bool

				for _, field := range obj.Fields {
					switch field.Name.Value {
					case "key":
						if keyStr, ok := field.Value.GetValue().(string); ok {
							key = keyStr
							hasKey = true
						}
					case "value":
						if valStr, ok := field.Value.GetValue().(string); ok {
							val = valStr
							hasValue = true
						}
					}
				}

				if hasKey && hasValue {
					result[key] = val
				}
			}
			return result
		default:
			return nil // to tell GraphQL that the value is invalid
		}
	},
})
