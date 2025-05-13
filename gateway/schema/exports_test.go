package schema

import "k8s.io/apimachinery/pkg/runtime/schema"

var StringMapScalarForTest = stringMapScalar

func GetPluralNameForTest(singular string) string {
	return getPluralName(singular)
}

func GetGatewayForTest(typeNameRegistry map[string]string) *Gateway {
	return &Gateway{
		typeNameRegistry: typeNameRegistry,
	}
}

func (g *Gateway) GetNamesForTest(gvk *schema.GroupVersionKind) (singular, plural string) {
	return g.getNames(gvk)
}
