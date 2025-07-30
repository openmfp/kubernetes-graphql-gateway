package schema

import "k8s.io/apimachinery/pkg/runtime/schema"

var StringMapScalarForTest = stringMapScalar

func GetGatewayForTest(typeNameRegistry map[string]string) *Gateway {
	return &Gateway{
		typeNameRegistry: typeNameRegistry,
	}
}

func (g *Gateway) GetNamesForTest(gvk *schema.GroupVersionKind) (singular, plural string) {
	return g.getNames(gvk)
}

func (g *Gateway) GenerateTypeNameForTest(typePrefix string, fieldPath []string) string {
	return g.generateTypeName(typePrefix, fieldPath)
}

func SanitizeFieldNameForTest(name string) string {
	return sanitizeFieldName(name)
}

func (g *Gateway) ShouldInferAsObjectMetaForTest(fieldPath []string) bool {
	return g.shouldInferAsObjectMeta(fieldPath)
}

func (g *Gateway) GetObjectMetaTypeForTest() (interface{}, interface{}, error) {
	return g.getObjectMetaType()
}
