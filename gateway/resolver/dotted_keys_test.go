package resolver_test

import (
	"testing"

	"github.com/openmfp/kubernetes-graphql-gateway/gateway/resolver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKubernetesToGraphQL(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected any
	}{
		{
			name: "complete_kubernetes_object",
			input: map[string]any{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata": map[string]any{
					"name":      "my-app",
					"namespace": "default",
					"labels": map[string]any{
						"app.kubernetes.io/name":    "my-app",
						"app.kubernetes.io/version": "1.0.0",
					},
					"annotations": map[string]any{
						"deployment.kubernetes.io/revision": "1",
					},
				},
				"spec": map[string]any{
					"replicas": 3,
					"nodeSelector": map[string]any{
						"kubernetes.io/arch":               "amd64",
						"node.kubernetes.io/instance-type": "m5.large",
					},
					"selector": map[string]any{
						"matchLabels": map[string]any{
							"app.kubernetes.io/name": "my-app",
						},
					},
				},
			},
			expected: map[string]any{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata": map[string]any{
					"name":      "my-app",
					"namespace": "default",
					"labels": []map[string]any{
						{"key": "app.kubernetes.io/name", "value": "my-app"},
						{"key": "app.kubernetes.io/version", "value": "1.0.0"},
					},
					"annotations": []map[string]any{
						{"key": "deployment.kubernetes.io/revision", "value": "1"},
					},
				},
				"spec": map[string]any{
					"replicas": 3,
					"nodeSelector": []map[string]any{
						{"key": "kubernetes.io/arch", "value": "amd64"},
						{"key": "node.kubernetes.io/instance-type", "value": "m5.large"},
					},
					"selector": map[string]any{
						"matchLabels": []map[string]any{
							{"key": "app.kubernetes.io/name", "value": "my-app"},
						},
					},
				},
			},
		},
		{
			name: "object_without_metadata_or_spec",
			input: map[string]any{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"data": map[string]any{
					"config.yaml": "key: value",
				},
			},
			expected: map[string]any{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"data": map[string]any{
					"config.yaml": "key: value",
				},
			},
		},
		{
			name:     "nil_input",
			input:    nil,
			expected: nil,
		},
		{
			name:     "invalid_type",
			input:    "not-a-map",
			expected: "not-a-map",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolver.KubernetesToGraphQL(tt.input)

			if tt.expected == nil {
				assert.Nil(t, result)
				return
			}

			// For complex nested objects, we need custom comparison logic
			if expectedMap, ok := tt.expected.(map[string]any); ok {
				resultMap, ok := result.(map[string]any)
				require.True(t, ok, "Expected result to be a map")

				// Compare basic fields
				for key, expectedVal := range expectedMap {
					resultVal := resultMap[key]

					switch key {
					case "metadata":
						compareMetadata(t, expectedVal, resultVal)
					case "spec":
						compareSpec(t, expectedVal, resultVal)
					default:
						assert.Equal(t, expectedVal, resultVal)
					}
				}
			} else {
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestGraphQLToKubernetes(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected any
	}{
		{
			name: "graphql_input_with_label_arrays",
			input: map[string]any{
				"metadata": map[string]any{
					"name": "my-app",
					"labels": []any{
						map[string]any{"key": "app.kubernetes.io/name", "value": "my-app"},
						map[string]any{"key": "environment", "value": "production"},
					},
				},
				"spec": map[string]any{
					"nodeSelector": []any{
						map[string]any{"key": "kubernetes.io/arch", "value": "amd64"},
					},
					"selector": map[string]any{
						"matchLabels": []any{
							map[string]any{"key": "app.kubernetes.io/name", "value": "my-app"},
						},
					},
				},
			},
			expected: map[string]any{
				"metadata": map[string]any{
					"name": "my-app",
					"labels": map[string]string{
						"app.kubernetes.io/name": "my-app",
						"environment":            "production",
					},
				},
				"spec": map[string]any{
					"nodeSelector": map[string]string{
						"kubernetes.io/arch": "amd64",
					},
					"selector": map[string]any{
						"matchLabels": map[string]string{
							"app.kubernetes.io/name": "my-app",
						},
					},
				},
			},
		},
		{
			name:     "nil_input",
			input:    nil,
			expected: nil,
		},
		{
			name:     "invalid_type",
			input:    "not-a-map",
			expected: "not-a-map",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolver.GraphqlToKubernetes(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Helper functions for complex comparisons
func compareMetadata(t *testing.T, expected, result any) {
	expectedMeta := expected.(map[string]any)
	resultMeta, ok := result.(map[string]any)
	require.True(t, ok, "Expected metadata to be a map")

	for key, expectedVal := range expectedMeta {
		resultVal := resultMeta[key]

		if key == "labels" || key == "annotations" {
			if expectedVal == nil {
				assert.Nil(t, resultVal)
			} else {
				expectedArray := expectedVal.([]map[string]any)
				resultArray, ok := resultVal.([]map[string]any)
				require.True(t, ok, "Expected %s to be an array", key)
				assert.ElementsMatch(t, expectedArray, resultArray)
			}
		} else {
			assert.Equal(t, expectedVal, resultVal)
		}
	}
}

func compareSpec(t *testing.T, expected, result any) {
	expectedSpec := expected.(map[string]any)
	resultSpec, ok := result.(map[string]any)
	require.True(t, ok, "Expected spec to be a map")

	for key, expectedVal := range expectedSpec {
		resultVal := resultSpec[key]

		switch key {
		case "nodeSelector":
			if expectedVal == nil {
				assert.Nil(t, resultVal)
			} else {
				expectedArray := expectedVal.([]map[string]any)
				resultArray, ok := resultVal.([]map[string]any)
				require.True(t, ok, "Expected nodeSelector to be an array")
				assert.ElementsMatch(t, expectedArray, resultArray)
			}
		case "selector":
			expectedSelector := expectedVal.(map[string]any)
			resultSelector, ok := resultVal.(map[string]any)
			require.True(t, ok, "Expected selector to be a map")

			if expectedMatchLabels, ok := expectedSelector["matchLabels"]; ok {
				resultMatchLabels := resultSelector["matchLabels"]
				expectedArray := expectedMatchLabels.([]map[string]any)
				resultArray, ok := resultMatchLabels.([]map[string]any)
				require.True(t, ok, "Expected matchLabels to be an array")
				assert.ElementsMatch(t, expectedArray, resultArray)
			}
		default:
			assert.Equal(t, expectedVal, resultVal)
		}
	}
}
