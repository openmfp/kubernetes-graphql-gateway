package schema_test

import (
	"testing"

	"github.com/openmfp/kubernetes-graphql-gateway/gateway/schema"
)

func TestShouldInferAsObjectMeta(t *testing.T) {
	g := schema.GetGatewayForTest(map[string]string{})

	tests := []struct {
		name      string
		fieldPath []string
		expected  bool
	}{
		{
			name:      "one_level_deep_metadata",
			fieldPath: []string{"metadata"},
			expected:  true,
		},
		{
			name:      "two_level_deep_metadata",
			fieldPath: []string{"spec", "metadata"},
			expected:  true,
		},
		{
			name:      "three_level_deep_metadata",
			fieldPath: []string{"spec", "apiExport", "metadata"},
			expected:  true,
		},

		{
			name:      "non_metadata_field",
			fieldPath: []string{"spec", "containers"},
			expected:  false,
		},
		{
			name:      "empty_field_path",
			fieldPath: []string{},
			expected:  false,
		},
		{
			name:      "case_sensitive_metadata",
			fieldPath: []string{"Metadata"},
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := g.ShouldInferAsObjectMetaForTest(tt.fieldPath)
			if got != tt.expected {
				t.Errorf("ShouldInferAsObjectMetaForTest(%v) = %v, want %v", tt.fieldPath, got, tt.expected)
			}
		})
	}
}

func TestGetObjectMetaType_Fallback(t *testing.T) {
	// Test that getObjectMetaType doesn't panic and returns something
	g := schema.GetGatewayForTest(map[string]string{})

	outputType, inputType, err := g.GetObjectMetaTypeForTest()

	if err != nil {
		t.Errorf("GetObjectMetaTypeForTest() unexpected error: %v", err)
	}

	if outputType == nil || inputType == nil {
		t.Errorf("GetObjectMetaTypeForTest() should return non-nil types as fallback, got outputType=%v, inputType=%v", outputType, inputType)
	}
}
