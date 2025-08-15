package schema

import (
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/go-openapi/spec"
	"github.com/graphql-go/graphql"
)

// RelationEnhancer handles schema enhancement for relation fields
type RelationEnhancer struct {
	gateway *Gateway
}

// NewRelationEnhancer creates a new relation enhancer
func NewRelationEnhancer(gateway *Gateway) *RelationEnhancer {
	return &RelationEnhancer{
		gateway: gateway,
	}
}

// AddRelationFields adds relation fields to schemas that contain *Ref fields
func (re *RelationEnhancer) AddRelationFields(fields graphql.Fields, properties map[string]spec.Schema) {
	for fieldName := range properties {
		if !strings.HasSuffix(fieldName, "Ref") {
			continue
		}

		baseName := strings.TrimSuffix(fieldName, "Ref")
		sanitizedFieldName := sanitizeFieldName(fieldName)

		refField, exists := fields[sanitizedFieldName]
		if !exists {
			continue
		}

		enhancedType := re.enhanceRefTypeWithRelation(refField.Type, baseName)
		if enhancedType == nil {
			continue
		}

		fields[sanitizedFieldName] = &graphql.Field{
			Type: enhancedType,
		}
	}
}

// enhanceRefTypeWithRelation adds a relation field to a *Ref object type
func (re *RelationEnhancer) enhanceRefTypeWithRelation(originalType graphql.Output, baseName string) graphql.Output {
	objType, ok := originalType.(*graphql.Object)
	if !ok {
		return originalType
	}

	cacheKey := objType.Name() + "_" + baseName + "_Enhanced"
	if enhancedType, exists := re.gateway.enhancedTypesCache[cacheKey]; exists {
		return enhancedType
	}

	enhancedFields := re.copyOriginalFields(objType.Fields())
	re.addRelationField(enhancedFields, baseName)

	enhancedType := graphql.NewObject(graphql.ObjectConfig{
		Name:   sanitizeFieldName(cacheKey),
		Fields: enhancedFields,
	})

	re.gateway.enhancedTypesCache[cacheKey] = enhancedType
	return enhancedType
}

// copyOriginalFields converts FieldDefinition to Field for reuse
func (re *RelationEnhancer) copyOriginalFields(originalFieldDefs graphql.FieldDefinitionMap) graphql.Fields {
	enhancedFields := make(graphql.Fields, len(originalFieldDefs))
	for fieldName, fieldDef := range originalFieldDefs {
		enhancedFields[fieldName] = &graphql.Field{
			Type:        fieldDef.Type,
			Description: fieldDef.Description,
			Resolve:     fieldDef.Resolve,
		}
	}
	return enhancedFields
}

// addRelationField adds a single relation field to the enhanced fields
func (re *RelationEnhancer) addRelationField(enhancedFields graphql.Fields, baseName string) {
	targetType := re.findRelationTargetType(baseName)
	if targetType == nil {
		return
	}

	sanitizedBaseName := sanitizeFieldName(baseName)
	enhancedFields[sanitizedBaseName] = &graphql.Field{
		Type:    targetType,
		Resolve: re.gateway.resolver.RelationResolver(baseName),
	}
}

// findRelationTargetType finds the GraphQL type for a relation target
func (re *RelationEnhancer) findRelationTargetType(baseName string) graphql.Output {
	targetKind := cases.Title(language.English).String(baseName)

	for defKey, defSchema := range re.gateway.definitions {
		if re.matchesTargetKind(defSchema, targetKind) {
			if existingType, exists := re.gateway.typesCache[defKey]; exists {
				return existingType
			}

			if fieldType, _, err := re.gateway.convertSwaggerTypeToGraphQL(defSchema, defKey, []string{}, make(map[string]bool)); err == nil {
				return fieldType
			}
		}
	}

	return graphql.String
}

// matchesTargetKind checks if a schema definition matches the target kind
func (re *RelationEnhancer) matchesTargetKind(defSchema spec.Schema, targetKind string) bool {
	gvkExt, ok := defSchema.Extensions["x-kubernetes-group-version-kind"]
	if !ok {
		return false
	}

	gvkSlice, ok := gvkExt.([]any)
	if !ok || len(gvkSlice) == 0 {
		return false
	}

	gvkMap, ok := gvkSlice[0].(map[string]any)
	if !ok {
		return false
	}

	kind, ok := gvkMap["kind"].(string)
	return ok && kind == targetKind
}
