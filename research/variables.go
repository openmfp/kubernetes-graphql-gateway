package research

import (
	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
)

var generatedTypes = make(map[string]*graphql.Object)

//
// var objectMeta = graphql.NewObject(graphql.ObjectConfig{
// 	Name: "Metadata",
// 	Fields: graphql.Fields{
// 		"name": &graphql.Field{
// 			Type:        graphql.NewNonNull(graphql.String),
// 			Description: "the metadata.name of the object",
// 		},
// 		"namespace": &graphql.Field{
// 			Type:        graphql.NewNonNull(graphql.String),
// 			Description: "the metadata.namespace of the object",
// 		},
// 		"labels": &graphql.Field{
// 			Type:        stringMapScalar,
// 			Description: "the metadata.labels of the object",
// 		},
// 		"annotations": &graphql.Field{
// 			Type:        stringMapScalar,
// 			Description: "the metadata.annotations of the object",
// 		},
// 	},
// })

var objectMeta = graphql.NewObject(graphql.ObjectConfig{
	Name: "Metadata",
	Fields: graphql.Fields{
		"name": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "the metadata.name of the object",
			Resolve:     unstructuredFieldResolver("name"),
		},
		"namespace": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "the metadata.namespace of the object",
			Resolve:     unstructuredFieldResolver("namespace"),
		},
		"labels": &graphql.Field{
			Type:        stringMapScalar,
			Description: "the metadata.labels of the object",
			Resolve:     unstructuredFieldResolver("labels"),
		},
		"annotations": &graphql.Field{
			Type:        stringMapScalar,
			Description: "the metadata.annotations of the object",
			Resolve:     unstructuredFieldResolver("annotations"),
		},
	},
})

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
