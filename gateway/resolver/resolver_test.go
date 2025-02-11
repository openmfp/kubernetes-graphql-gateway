package resolver

import (
	"context"
	"github.com/graphql-go/graphql"
	"github.com/openmfp/crd-gql-gateway/listener/controller/mocks"
	"github.com/openmfp/golang-commons/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"testing"
)

func getResolver(runtimeClientMock client.WithWatch) (*Service, error) {
	log, err := logger.New(logger.DefaultConfig())

	return New(log, runtimeClientMock), err
}

func TestListItems(t *testing.T) {
	tests := []struct {
		name          string
		args          map[string]interface{}
		mockSetup     func(runtimeClientMock *mocks.MockWithWatch)
		expectedItems []map[string]any
		expectError   bool
	}{
		{
			name: "listItems_OK",
			args: map[string]interface{}{
				namespaceArg:     "test-namespace",
				labelSelectorArg: "key=value",
			},
			mockSetup: func(runtimeClientMock *mocks.MockWithWatch) {
				runtimeClientMock.EXPECT().
					List(
						mock.Anything,
						mock.AnythingOfType("*unstructured.UnstructuredList"),
						client.MatchingLabelsSelector{Selector: labels.SelectorFromSet(labels.Set{"key": "value"})},
						client.InNamespace("test-namespace"),
					).
					Run(func(_ context.Context, l client.ObjectList, _ ...client.ListOption) {
						l.(*unstructured.UnstructuredList).Items = []unstructured.Unstructured{
							{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": "ns-object"}}},
						}
					}).
					Return(nil)
			},
			expectedItems: []map[string]any{
				{"metadata": map[string]interface{}{"name": "ns-object"}},
			},
		},
		{
			name: "listItems_ERROR",
			mockSetup: func(runtimeClientMock *mocks.MockWithWatch) {
				runtimeClientMock.EXPECT().
					List(mock.Anything, mock.AnythingOfType("*unstructured.UnstructuredList"), mock.Anything).
					Return(assert.AnError)
			},
			expectError: true,
		},
		{
			name: "invalidLabelSelector_ERROR",
			args: map[string]interface{}{
				labelSelectorArg: ",,",
			},
			expectedItems: nil,
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtimeClientMock := &mocks.MockWithWatch{}
			if tt.mockSetup != nil {
				tt.mockSetup(runtimeClientMock)
			}

			r, err := getResolver(runtimeClientMock)
			require.NoError(t, err)

			result, err := r.ListItems(schema.GroupVersionKind{
				Group:   "group",
				Version: "version",
				Kind:    "kind",
			})(graphql.ResolveParams{
				Context: context.Background(),
				Args:    tt.args,
			})

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedItems, result)
			}
		})
	}
}

func TestGetItem(t *testing.T) {
	tests := []struct {
		name        string
		args        map[string]interface{}
		mockSetup   func(runtimeClientMock *mocks.MockWithWatch)
		expectedObj map[string]interface{}
		expectError bool
	}{
		{
			name: "getItem_OK",
			args: map[string]interface{}{
				nameArg:      "test-object",
				namespaceArg: "test-namespace",
			},
			mockSetup: func(runtimeClientMock *mocks.MockWithWatch) {
				runtimeClientMock.EXPECT().
					Get(
						mock.Anything,
						client.ObjectKey{Namespace: "test-namespace", Name: "test-object"},
						mock.AnythingOfType("*unstructured.Unstructured"),
					).
					Run(func(_ context.Context, _ client.ObjectKey, obj client.Object, _ ...client.GetOption) {
						unstructuredObj := obj.(*unstructured.Unstructured)
						unstructuredObj.Object = map[string]interface{}{
							"metadata": map[string]interface{}{"name": "test-object"},
						}
					}).
					Return(nil)
			},
			expectedObj: map[string]interface{}{
				"metadata": map[string]interface{}{"name": "test-object"},
			},
		},
		{
			name: "getItem_ERROR",
			args: map[string]interface{}{
				nameArg:      "test-object",
				namespaceArg: "test-namespace",
			},
			mockSetup: func(runtimeClientMock *mocks.MockWithWatch) {
				runtimeClientMock.EXPECT().
					Get(mock.Anything, client.ObjectKey{Namespace: "test-namespace", Name: "test-object"}, mock.Anything).
					Return(assert.AnError)
			},
			expectError: true,
		},
		{
			name: "missingNameArg_ERROR",
			args: map[string]interface{}{
				namespaceArg: "test-namespace",
			},
			expectError: true,
		},
		{
			name: "missingNamespaceArg_ERROR",
			args: map[string]interface{}{
				nameArg: "test-object",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtimeClientMock := &mocks.MockWithWatch{}
			if tt.mockSetup != nil {
				tt.mockSetup(runtimeClientMock)
			}

			r, err := getResolver(runtimeClientMock)
			require.NoError(t, err)

			result, err := r.GetItem(schema.GroupVersionKind{
				Group:   "group",
				Version: "version",
				Kind:    "kind",
			})(graphql.ResolveParams{
				Context: context.Background(),
				Args:    tt.args,
			})

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedObj, result)
			}
		})
	}
}

func TestCreateItem(t *testing.T) {
	tests := []struct {
		name        string
		args        map[string]interface{}
		mockSetup   func(runtimeClientMock *mocks.MockWithWatch)
		expectedObj map[string]interface{}
		expectError bool
	}{
		{
			name: "create_item_OK",
			args: map[string]interface{}{
				nameArg:      "test-object",
				namespaceArg: "test-namespace",
				"object": map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "test-object",
					},
				},
			},
			mockSetup: func(runtimeClientMock *mocks.MockWithWatch) {
				runtimeClientMock.EXPECT().
					Create(mock.Anything, mock.AnythingOfType("*unstructured.Unstructured")).
					Return(nil)
			},
			expectedObj: map[string]interface{}{
				"apiVersion": "group/version",
				"kind":       "kind",
				"metadata": map[string]interface{}{
					"name":      "test-object",
					"namespace": "test-namespace",
				},
			},
		},
		{
			name: "create_item_ERROR",
			args: map[string]interface{}{
				nameArg:      "test-object",
				namespaceArg: "test-namespace",
				"object": map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "test-object",
					},
				},
			},
			mockSetup: func(runtimeClientMock *mocks.MockWithWatch) {
				runtimeClientMock.EXPECT().
					Create(mock.Anything, mock.AnythingOfType("*unstructured.Unstructured")).
					Return(assert.AnError)
			},
			expectError: true,
		},
		{
			name: "missing_metadata_name_ERROR",
			args: map[string]interface{}{
				namespaceArg: "test-namespace",
				"object":     map[string]interface{}{},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtimeClientMock := &mocks.MockWithWatch{}
			if tt.mockSetup != nil {
				tt.mockSetup(runtimeClientMock)
			}

			r, err := getResolver(runtimeClientMock)
			require.NoError(t, err)

			result, err := r.CreateItem(schema.GroupVersionKind{
				Group:   "group",
				Version: "version",
				Kind:    "kind",
			})(graphql.ResolveParams{
				Context: context.Background(),
				Args:    tt.args,
			})

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedObj, result)
			}
		})
	}
}

func TestUpdateItem(t *testing.T) {
	tests := []struct {
		name        string
		args        map[string]interface{}
		mockSetup   func(runtimeClientMock *mocks.MockWithWatch)
		expectedObj map[string]interface{}
		expectError bool
	}{
		{
			name: "update_item_OK",
			args: map[string]interface{}{
				nameArg:      "test-object",
				namespaceArg: "test-namespace",
				"object": map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "test-object",
					},
				},
			},
			mockSetup: func(runtimeClientMock *mocks.MockWithWatch) {
				runtimeClientMock.EXPECT().
					Get(mock.Anything, client.ObjectKey{Namespace: "test-namespace", Name: "test-object"}, mock.AnythingOfType("*unstructured.Unstructured")).
					Run(func(_ context.Context, _ client.ObjectKey, obj client.Object, _ ...client.GetOption) {
						unstructuredObj := obj.(*unstructured.Unstructured)
						unstructuredObj.Object = map[string]interface{}{
							"metadata": map[string]interface{}{
								"name": "test-object",
							},
						}
					}).
					Return(nil)

				runtimeClientMock.EXPECT().
					Patch(mock.Anything, mock.AnythingOfType("*unstructured.Unstructured"), mock.Anything).
					Return(nil)
			},
			expectedObj: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "test-object",
				},
			},
		},
		{
			name: "missing_metadata_name_ERROR",
			args: map[string]interface{}{
				namespaceArg: "test-namespace",
				"object":     map[string]interface{}{},
			},
			expectError: true,
		},
		{
			name: "get_existing_object_ERROR",
			args: map[string]interface{}{
				nameArg:      "test-object",
				namespaceArg: "test-namespace",
				"object": map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "test-object",
					},
				},
			},
			mockSetup: func(runtimeClientMock *mocks.MockWithWatch) {
				runtimeClientMock.EXPECT().
					Get(mock.Anything, client.ObjectKey{Namespace: "test-namespace", Name: "test-object"}, mock.Anything).
					Return(assert.AnError)
			},
			expectError: true,
		},
		{
			name: "patch_object_ERROR",
			args: map[string]interface{}{
				nameArg:      "test-object",
				namespaceArg: "test-namespace",
				"object": map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "test-object",
					},
				},
			},
			mockSetup: func(runtimeClientMock *mocks.MockWithWatch) {
				runtimeClientMock.EXPECT().
					Get(mock.Anything, client.ObjectKey{Namespace: "test-namespace", Name: "test-object"}, mock.Anything).
					Return(nil)

				runtimeClientMock.EXPECT().
					Patch(mock.Anything, mock.AnythingOfType("*unstructured.Unstructured"), mock.Anything).
					Return(assert.AnError)
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtimeClientMock := &mocks.MockWithWatch{}
			if tt.mockSetup != nil {
				tt.mockSetup(runtimeClientMock)
			}

			r, err := getResolver(runtimeClientMock)
			require.NoError(t, err)

			result, err := r.UpdateItem(schema.GroupVersionKind{
				Group:   "group",
				Version: "version",
				Kind:    "kind",
			})(graphql.ResolveParams{
				Context: context.Background(),
				Args:    tt.args,
			})

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedObj, result)
			}
		})
	}
}

func TestDeleteItem(t *testing.T) {
	tests := []struct {
		name        string
		args        map[string]interface{}
		mockSetup   func(runtimeClientMock *mocks.MockWithWatch)
		expectError bool
	}{
		{
			name: "delete_item_OK",
			args: map[string]interface{}{
				nameArg:      "test-object",
				namespaceArg: "test-namespace",
			},
			mockSetup: func(runtimeClientMock *mocks.MockWithWatch) {
				runtimeClientMock.EXPECT().
					Delete(mock.Anything, mock.AnythingOfType("*unstructured.Unstructured")).
					Return(nil)
			},
		},
		{
			name: "missing_name_argument_ERROR",
			args: map[string]interface{}{
				namespaceArg: "test-namespace",
			},
			expectError: true,
		},
		{
			name: "missing_namespace_argument_ERROR",
			args: map[string]interface{}{
				nameArg: "test-object",
			},
			expectError: true,
		},
		{
			name: "delete_object_ERROR",
			args: map[string]interface{}{
				nameArg:      "test-object",
				namespaceArg: "test-namespace",
			},
			mockSetup: func(runtimeClientMock *mocks.MockWithWatch) {
				runtimeClientMock.EXPECT().
					Delete(mock.Anything, mock.AnythingOfType("*unstructured.Unstructured")).
					Return(assert.AnError)
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtimeClientMock := &mocks.MockWithWatch{}
			if tt.mockSetup != nil {
				tt.mockSetup(runtimeClientMock)
			}

			r, err := getResolver(runtimeClientMock)
			require.NoError(t, err)

			result, err := r.DeleteItem(schema.GroupVersionKind{
				Group:   "group",
				Version: "version",
				Kind:    "kind",
			})(graphql.ResolveParams{
				Context: context.Background(),
				Args:    tt.args,
			})

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, true, result)
			}
		})
	}
}

func TestGetListItemsArguments(t *testing.T) {
	r := &Service{}
	args := r.GetListItemsArguments()

	require.NotNil(t, args)
	require.Contains(t, args, labelSelectorArg)
	require.Contains(t, args, namespaceArg)

	require.Equal(t, graphql.String, args[labelSelectorArg].Type)
	require.Equal(t, "A label selector to filter the objects by", args[labelSelectorArg].Description)

	require.Equal(t, graphql.String, args[namespaceArg].Type)
	require.Equal(t, "The namespace in which to search for the objects", args[namespaceArg].Description)
}

func TestGetMutationArguments(t *testing.T) {
	r := &Service{}
	resourceInputType := graphql.NewInputObject(graphql.InputObjectConfig{
		Name: "TestResourceInput",
		Fields: graphql.InputObjectConfigFieldMap{
			"testField": &graphql.InputObjectFieldConfig{Type: graphql.String},
		},
	})

	args := r.GetMutationArguments(resourceInputType)

	require.NotNil(t, args)
	require.Contains(t, args, namespaceArg)
	require.Contains(t, args, "object")

	require.Equal(t, graphql.NewNonNull(graphql.String), args[namespaceArg].Type)
	require.Equal(t, "The namespace of the object", args[namespaceArg].Description)

	require.Equal(t, graphql.NewNonNull(resourceInputType), args["object"].Type)
	require.Equal(t, "The object to create or update", args["object"].Description)
}

func TestGetNameAndNamespaceArguments(t *testing.T) {
	r := &Service{}
	arguments := r.GetNameAndNamespaceArguments()

	name, ok := arguments[nameArg]
	assert.True(t, ok, "nameArg should be present")
	assert.NotNil(t, nameArg, "nameArg should not be nil")
	assert.Equal(t, graphql.NewNonNull(graphql.String), name.Type, "nameArg type should be graphql.String")
	assert.Equal(t, "The name of the object", name.Description, "nameArg description should match")

	namespace, ok := arguments[namespaceArg]
	assert.True(t, ok, "namespaceArg should be present")
	assert.NotNil(t, namespaceArg, "namespaceArg should not be nil")
	assert.Equal(t, graphql.NewNonNull(graphql.String), namespace.Type, "namespaceArg type should be graphql.String")
	assert.Equal(t, "The namespace of the object", namespace.Description, "namespaceArg description should match")
}

func TestSanitizeGroupName(t *testing.T) {
	r := &Service{
		groupNames: make(map[string]string),
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty_string", "", "core"},
		{"valid_group_name", "validName", "validName"},
		{"hyphen_to_underscore", "group-name", "group_name"},
		{"special_char_to_underscore", "group@name", "group_name"},
		{"invalid_start_with_prepend", "!invalidStart", "_invalidStart"},
		{"leading_underscore", "_leadingUnderscore", "_leadingUnderscore"},
		{"start_with_number", "123startWithNumber", "_123startWithNumber"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.SanitizeGroupName(tt.input)
			assert.Equal(t, tt.expected, result)
			assert.Equal(t, tt.input, r.groupNames[result], "The original group name should be stored correctly")
		})
	}
}

func TestGetOriginalGroupName(t *testing.T) {
	r := &Service{
		groupNames: map[string]string{
			"group1": "originalGroup1",
			"group2": "originalGroup2",
		},
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"existing_group", "group1", "originalGroup1"},
		{"non_existing_group", "group3", "group3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.GetOriginalGroupName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
