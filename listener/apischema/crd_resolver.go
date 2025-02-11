package apischema

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

type CRDResolver struct {
	*discovery.DiscoveryClient
	//TODO: add logging later
	//logger logr.Logger
}

func (cr *CRDResolver) ResolveApiSchema(crd *apiextensionsv1.CustomResourceDefinition) ([]byte, error) {
	gvk, err := getCRDGroupVersionKind(crd.Spec)
	if err != nil {
		return nil, fmt.Errorf("failed to get CRD GVK: %w", err)
	}
	preferredApiGroups := []string{}
	apiResLists, err := cr.ServerPreferredResources()
	if err != nil {
		return nil, fmt.Errorf("failed to get server preferred resources: %w", err)
	}
	isCRDFound := false
	for _, apiResources := range apiResLists {
		gv := apiResources.GroupVersion
		if gv == fmt.Sprintf("%s/%s", gvk.Group, gvk.Version) {
			for _, res := range apiResources.APIResources {
				if res.Kind == gvk.Kind {
					isCRDFound = true
					break
				}
			}
		}
		preferredApiGroups = append(preferredApiGroups, gv)
	}

	if !isCRDFound {
		return nil, errors.New("failed to find CRD GVK in API preferred resources")
	}

	apiv3Paths, err := cr.OpenAPIV3().Paths()
	if err != nil {
		return nil, fmt.Errorf("failed to get OpenAPI paths: %w", err)
	}

	schemas := make(map[string]*spec.Schema)
	for key, path := range apiv3Paths {
		if !strings.Contains(key, separator) {
			continue
		}
		pathApiGroupArray := strings.Split(key, separator)
		pathApiGroup := strings.Join(pathApiGroupArray[1:], separator)
		// filer out apiGroups that aren't in the preferred list
		if !slices.Contains(preferredApiGroups, pathApiGroup) {
			continue
		}

		b, err := path.Schema(discovery.AcceptV1)
		if err != nil {
			//TODO: debug log?
			continue
		}

		resp := &schemaResponse{}
		err = json.Unmarshal(b, resp)
		if err != nil {
			//TODO: debug log?
			continue
		}
		maps.Copy(schemas, resp.Components.Schemas)
	}
	v3JSON, err := json.Marshal(&schemaResponse{
		Components: schemasComponentsWrapper{
			Schemas: schemas,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal openAPI v3 schema: %w", err)
	}
	v2JSON, err := ConvertJSON(v3JSON)
	if err != nil {
		return nil, fmt.Errorf("failed to convert openAPI v3 schema to v2: %w", err)
	}

	return v2JSON, nil
}

func (cr *CRDResolver) Resolve() ([]byte, error) {
	preferredApiGroups := []string{}
	apiResList, err := cr.ServerPreferredResources()
	if err != nil {
		return nil, fmt.Errorf("failed to get server preferred resources: %w", err)
	}
	for _, apiRes := range apiResList {
		preferredApiGroups = append(preferredApiGroups, apiRes.GroupVersion)
	}

	apiv3Paths, err := cr.OpenAPIV3().Paths()
	if err != nil {
		return nil, fmt.Errorf("failed to get OpenAPI paths: %w", err)
	}

	schemas := make(map[string]*spec.Schema)
	for path, gv := range apiv3Paths {
		schema, err := getSchemaForPath(preferredApiGroups, path, gv)
		if err != nil {
			//TODO: debug log?
			continue
		}
		maps.Copy(schemas, schema)
	}
	v3JSON, err := json.Marshal(&schemaResponse{
		Components: schemasComponentsWrapper{
			Schemas: schemas,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal openAPI v3 schema: %w", err)
	}
	v2JSON, err := ConvertJSON(v3JSON)
	if err != nil {
		return nil, fmt.Errorf("failed to convert openAPI v3 schema to v2: %w", err)
	}

	return v2JSON, nil
}

func getCRDGroupVersionKind(spec apiextensionsv1.CustomResourceDefinitionSpec) (*metav1.GroupVersionKind, error) {
	for _, v := range spec.Versions {
		if v.Storage {
			return &metav1.GroupVersionKind{
				Group:   spec.Group,
				Version: v.Name,
				Kind:    spec.Names.Kind,
			}, nil
		}
	}
	return nil, errors.New("failed to find storage version")
}
